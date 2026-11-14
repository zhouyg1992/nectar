package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/memory"
)

type memoryCLIOptions struct {
	kind       string
	scope      string
	status     string
	tags       string
	source     string
	project    string
	chat       string
	channel    string
	configPath string
	socket     string
	limit      int
	jsonOut    bool
	confirm    bool
	statusSet  bool
}

type memoryCLIListResult struct {
	Memories []memory.Memory `json:"memories"`
}

func runMemory(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printMemoryUsage()
		return nil
	}
	command := strings.ToLower(args[0])
	positional, opts, err := parseMemoryCLIOptions(args[1:])
	if errors.Is(err, flag.ErrHelp) {
		printMemoryUsage()
		return nil
	}
	if err != nil {
		return err
	}
	if (command == "list" || command == "ls") && !opts.statusSet {
		opts.status = string(memory.StatusActive)
	}

	switch command {
	case "add", "remember":
		return memoryCLIAdd(positional, opts)
	case "search", "find":
		return memoryCLISearch(positional, opts)
	case "list", "ls":
		return memoryCLIList(positional, opts)
	case "get", "show":
		return memoryCLIGet(positional, opts)
	case "update":
		return memoryCLIUpdate(positional, opts)
	case "approve", "activate":
		return memoryCLISetStatus(positional, opts, memory.StatusActive)
	case "reject", "archive":
		return memoryCLISetStatus(positional, opts, memory.StatusArchived)
	case "pin":
		return memoryCLISetPinned(positional, opts, true)
	case "unpin":
		return memoryCLISetPinned(positional, opts, false)
	case "forget", "delete", "rm":
		return memoryCLIForget(positional, opts)
	default:
		printMemoryUsage()
		return fmt.Errorf("unknown memory command %q", command)
	}
}

func printMemoryUsage() {
	fmt.Print(`Usage:
  nectar memory add [--kind profile|project|fact|lesson|episode]
                    [--scope chat|project|global] [--status candidate|active]
                    [--tags a,b] [--source agent] [--socket PATH] [--json] <text>
  nectar memory search [--scope chat|project|global|all] [--limit N] [--json] <query>
  nectar memory list [--status active|pending|archived|all]
                     [--scope chat|project|global|all] [--json]
  nectar memory get <id> [--json]
  nectar memory update <id> [--json] <new text>
  nectar memory approve|reject|pin|unpin <id>
  nectar memory forget <id> --confirm

Scope defaults to the current chat when NECTAR_CHAT_ID is set, otherwise the
current project. In an agent session, NECTAR_API_SOCKET routes writes through
the Nectar daemon so sandboxed agents do not need write access to ~/.nectar.
Add --json for machine-readable output.
`)
}

func parseMemoryCLIOptions(args []string) ([]string, memoryCLIOptions, error) {
	opts := memoryCLIOptions{
		kind:   string(memory.KindFact),
		scope:  "auto",
		status: string(memory.StatusCandidate),
		source: "cli",
		limit:  5,
	}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-h" || arg == "--help" {
			return nil, opts, flag.ErrHelp
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--json":
			if hasValue {
				return nil, opts, fmt.Errorf("%s does not take a value", name)
			}
			opts.jsonOut = true
		case "--confirm":
			if hasValue {
				return nil, opts, fmt.Errorf("%s does not take a value", name)
			}
			opts.confirm = true
		case "--kind", "--scope", "--status", "--tags", "--source",
			"--project", "--chat", "--channel", "--config", "--socket", "--limit":
			if !hasValue {
				i++
				if i >= len(args) {
					return nil, opts, fmt.Errorf("%s requires a value", name)
				}
				value = args[i]
			}
			switch name {
			case "--kind":
				opts.kind = value
			case "--scope":
				opts.scope = value
			case "--status":
				opts.status = value
				opts.statusSet = true
			case "--tags":
				opts.tags = value
			case "--source":
				opts.source = value
			case "--project":
				opts.project = value
			case "--chat":
				opts.chat = value
			case "--channel":
				opts.channel = value
			case "--config":
				opts.configPath = value
			case "--socket":
				opts.socket = value
			case "--limit":
				limit, err := strconv.Atoi(value)
				if err != nil || limit <= 0 {
					return nil, opts, fmt.Errorf("--limit must be a positive number")
				}
				opts.limit = limit
			}
		default:
			return nil, opts, fmt.Errorf("unknown memory flag %q", name)
		}
	}
	return positional, opts, nil
}

func memoryCLIAdd(positional []string, opts memoryCLIOptions) error {
	content := strings.TrimSpace(strings.Join(positional, " "))
	if content == "" {
		return errors.New("memory add requires text")
	}
	kind, err := parseMemoryKind(opts.kind)
	if err != nil {
		return err
	}
	status, err := parseMemoryStatus(opts.status)
	if err != nil {
		return err
	}
	if status == memory.StatusArchived {
		return errors.New("new memory cannot start archived")
	}
	cfg, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	scope, scopeID, err := memoryCLIAddScope(opts)
	if err != nil {
		return err
	}
	if scope == memory.ScopeGlobal && !memoryCLIIsAdmin(cfg) {
		return errors.New("global memory requires an admin user")
	}
	if memoryCLIIsGroup(opts) && status == memory.StatusActive && !memoryCLIIsAdmin(cfg) {
		status = memory.StatusCandidate
	}
	confidence := 0.4
	if status == memory.StatusActive {
		confidence = 0.9
	}
	workdir := ""
	if scope != memory.ScopeGlobal {
		workdir, _ = memoryCLIProjectDir(opts)
	}
	content, locality := memory.NormalizeContent(workdir, content)
	projectID := ""
	if scope == memory.ScopeProject {
		projectID = strings.TrimPrefix(scopeID, "project:")
	}
	item, err := store.Put(context.Background(), memory.Memory{
		Kind:       kind,
		Scope:      scope,
		ScopeID:    scopeID,
		ProjectID:  projectID,
		Content:    content,
		Locality:   locality,
		Tags:       parseMemoryTags(opts.tags),
		Source:     strings.TrimSpace(opts.source),
		Confidence: confidence,
		Status:     status,
	})
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(item)
	}
	printMemoryCLIItem(item)
	if item.Status == memory.StatusCandidate {
		fmt.Printf("candidate: activate with `nectar memory approve %s`\n", item.ID)
	}
	return nil
}

func memoryCLISearch(positional []string, opts memoryCLIOptions) error {
	query := strings.TrimSpace(strings.Join(positional, " "))
	if query == "" {
		return errors.New("memory search requires a query")
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	scope, scopeIDs, err := memoryCLIViewScope(opts)
	if err != nil {
		return err
	}
	items, err := store.Search(context.Background(), memory.SearchOptions{
		Query:    query,
		Scope:    scope,
		ScopeIDs: scopeIDs,
		Limit:    opts.limit,
	})
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(memoryCLIListResult{Memories: items})
	}
	if len(items) == 0 {
		fmt.Println("No matching memories.")
		return nil
	}
	for _, item := range items {
		printMemoryCLIItem(item)
	}
	return nil
}

func memoryCLIList(positional []string, opts memoryCLIOptions) error {
	if len(positional) > 0 {
		return errors.New("memory list does not take positional arguments")
	}
	status, err := parseMemoryListStatus(opts.status)
	if err != nil {
		return err
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	scope, scopeIDs, err := memoryCLIViewScope(opts)
	if err != nil {
		return err
	}
	items, err := store.List(context.Background(), memory.ListOptions{
		Status:   status,
		Scope:    scope,
		ScopeIDs: scopeIDs,
	})
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(memoryCLIListResult{Memories: items})
	}
	counts := make(map[memory.Status]int)
	allItems, err := store.List(context.Background(), memory.ListOptions{Scope: scope, ScopeIDs: scopeIDs})
	if err == nil {
		for _, item := range allItems {
			counts[item.Status]++
		}
		fmt.Printf("active=%d pending=%d archived=%d\n", counts[memory.StatusActive], counts[memory.StatusCandidate], counts[memory.StatusArchived])
	}
	if len(items) == 0 {
		fmt.Println("No memories.")
		return nil
	}
	for _, item := range items {
		printMemoryCLIItem(item)
	}
	return nil
}

func memoryCLIGet(positional []string, opts memoryCLIOptions) error {
	id, err := memoryCLIID(positional)
	if err != nil {
		return err
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	item, ok, err := store.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("memory %q not found", id)
	}
	if err := memoryCLICheckVisible(item, opts); err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(item)
	}
	printMemoryCLIDetail(item)
	return nil
}

func memoryCLIUpdate(positional []string, opts memoryCLIOptions) error {
	if len(positional) < 2 {
		return errors.New("memory update requires an id and new text")
	}
	id := strings.TrimSpace(positional[0])
	content := strings.TrimSpace(strings.Join(positional[1:], " "))
	if id == "" || content == "" {
		return errors.New("memory update requires an id and new text")
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	existing, err := memoryCLIManageable(store, id, opts)
	if err != nil {
		return err
	}
	workdir := ""
	if existing.Scope != memory.ScopeGlobal {
		workdir, _ = memoryCLIProjectDir(opts)
	}
	content, _ = memory.NormalizeContent(workdir, content)
	item, err := store.UpdateContent(context.Background(), id, content)
	if err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(item)
	}
	printMemoryCLIItem(item)
	return nil
}

func memoryCLISetStatus(positional []string, opts memoryCLIOptions, status memory.Status) error {
	id, err := memoryCLIID(positional)
	if err != nil {
		return err
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	item, err := memoryCLIManageable(store, id, opts)
	if err != nil {
		return err
	}
	if err := store.SetStatus(context.Background(), id, status); err != nil {
		return err
	}
	item.Status = status
	if opts.jsonOut {
		return printJSON(item)
	}
	fmt.Printf("%s %s\n", id, status)
	return nil
}

func memoryCLISetPinned(positional []string, opts memoryCLIOptions, pinned bool) error {
	id, err := memoryCLIID(positional)
	if err != nil {
		return err
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	item, err := memoryCLIManageable(store, id, opts)
	if err != nil {
		return err
	}
	if err := store.SetPinned(context.Background(), id, pinned); err != nil {
		return err
	}
	item.Pinned = pinned
	if opts.jsonOut {
		return printJSON(item)
	}
	action := "unpinned"
	if pinned {
		action = "pinned"
	}
	fmt.Printf("%s %s\n", id, action)
	return nil
}

func memoryCLIForget(positional []string, opts memoryCLIOptions) error {
	id, err := memoryCLIID(positional)
	if err != nil {
		return err
	}
	if !opts.confirm {
		return errors.New("memory forget requires --confirm")
	}
	_, store, err := memoryCLILoad(opts)
	if err != nil {
		return err
	}
	if _, err := memoryCLIManageable(store, id, opts); err != nil {
		return err
	}
	if err := store.Delete(context.Background(), id); err != nil {
		return err
	}
	if opts.jsonOut {
		return printJSON(map[string]any{"id": id, "deleted": true})
	}
	fmt.Printf("%s forgotten\n", id)
	return nil
}

func memoryCLILoad(opts memoryCLIOptions) (config.Config, memory.Store, error) {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return config.Config{}, nil, err
	}
	if socket := memoryCLISocket(opts); socket != "" {
		return cfg, &remoteMemoryStore{socket: socket}, nil
	}
	store, err := memory.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, store, nil
}

func memoryCLIAddScope(opts memoryCLIOptions) (memory.Scope, string, error) {
	scope := strings.ToLower(strings.TrimSpace(opts.scope))
	if scope == "" || scope == "auto" {
		if memoryCLIChatID(opts) != "" {
			scope = string(memory.ScopeChat)
		} else {
			scope = string(memory.ScopeProject)
		}
	}
	switch memory.Scope(scope) {
	case memory.ScopeGlobal:
		return memory.ScopeGlobal, "", nil
	case memory.ScopeChat:
		chatID := memoryCLIChatID(opts)
		if chatID == "" {
			return "", "", errors.New("chat scope requires --chat or NECTAR_CHAT_ID")
		}
		return memory.ScopeChat, memory.ChatScope(memoryCLIChannel(opts), chatID), nil
	case memory.ScopeProject:
		workdir, err := memoryCLIProjectDir(opts)
		if err != nil {
			return "", "", err
		}
		scopeID := memory.ProjectScope(workdir)
		if scopeID == "" {
			return "", "", errors.New("could not resolve a project scope")
		}
		return memory.ScopeProject, scopeID, nil
	default:
		return "", "", fmt.Errorf("unsupported memory scope %q", opts.scope)
	}
}

func memoryCLIViewScope(opts memoryCLIOptions) (memory.Scope, []string, error) {
	scope := strings.ToLower(strings.TrimSpace(opts.scope))
	switch scope {
	case "", "auto":
		return "", memoryCLIContextScopeIDs(opts), nil
	case "all":
		if ids := memoryCLIContextScopeIDs(opts); len(ids) > 0 {
			return "", ids, nil
		}
		return "", nil, nil
	case "global":
		return memory.ScopeGlobal, nil, nil
	case "chat":
		chatID := memoryCLIChatID(opts)
		if chatID == "" {
			return "", nil, errors.New("chat scope requires --chat or NECTAR_CHAT_ID")
		}
		return memory.ScopeChat, []string{memory.ChatScope(memoryCLIChannel(opts), chatID)}, nil
	case "project":
		workdir, err := memoryCLIProjectDir(opts)
		if err != nil {
			return "", nil, err
		}
		scopeIDs := memory.ProjectScopeAliases(workdir)
		if len(scopeIDs) == 0 {
			return "", nil, errors.New("could not resolve a project scope")
		}
		return memory.ScopeProject, scopeIDs, nil
	default:
		return "", nil, fmt.Errorf("unsupported memory scope %q", opts.scope)
	}
}

func memoryCLIManageable(store memory.Store, id string, opts memoryCLIOptions) (memory.Memory, error) {
	item, ok, err := store.Get(context.Background(), id)
	if err != nil {
		return memory.Memory{}, err
	}
	if !ok {
		return memory.Memory{}, fmt.Errorf("memory %q not found", id)
	}
	if err := memoryCLICheckVisible(item, opts); err != nil {
		return memory.Memory{}, err
	}
	if item.Scope == memory.ScopeGlobal {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			return memory.Memory{}, err
		}
		if !memoryCLIIsAdmin(cfg) {
			return memory.Memory{}, errors.New("global memory requires an admin user")
		}
	}
	if memoryCLIIsGroup(opts) && !memoryCLIIsAdminForPath(opts) {
		return memory.Memory{}, errors.New("group memory changes require an admin user")
	}
	return item, nil
}

func memoryCLICheckVisible(item memory.Memory, opts memoryCLIOptions) error {
	switch strings.ToLower(strings.TrimSpace(opts.scope)) {
	case "global":
		if item.Scope != memory.ScopeGlobal {
			return fmt.Errorf("memory %q is not a global memory", item.ID)
		}
		return nil
	case "chat":
		if item.Scope != memory.ScopeChat {
			return fmt.Errorf("memory %q is not a chat memory", item.ID)
		}
	case "project":
		if item.Scope != memory.ScopeProject {
			return fmt.Errorf("memory %q is not a project memory", item.ID)
		}
	}
	if item.Scope == memory.ScopeGlobal {
		return nil
	}
	_, ids, err := memoryCLIViewScope(opts)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if item.ScopeID == id {
			return nil
		}
	}
	return fmt.Errorf("memory %q is outside the current chat/project", item.ID)
}

func memoryCLIIsAdmin(cfg config.Config) bool {
	userID := strings.TrimSpace(os.Getenv("NECTAR_USER_ID"))
	if userID == "" || len(cfg.Bot.AdminUsers) == 0 {
		return true
	}
	for _, admin := range cfg.Bot.AdminUsers {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	return false
}

func memoryCLIIsAdminForPath(opts memoryCLIOptions) bool {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return false
	}
	return memoryCLIIsAdmin(cfg)
}

func memoryCLIIsGroup(opts memoryCLIOptions) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("NECTAR_CHAT_TYPE")), "group")
}

func memoryCLIChatID(opts memoryCLIOptions) string {
	return firstNonEmpty(opts.chat, os.Getenv("NECTAR_CHAT_ID"))
}

func memoryCLISocket(opts memoryCLIOptions) string {
	if socket := strings.TrimSpace(opts.socket); socket != "" {
		return socket
	}
	return strings.TrimSpace(os.Getenv("NECTAR_API_SOCKET"))
}

func memoryCLIContextScopeIDs(opts memoryCLIOptions) []string {
	var ids []string
	if chatID := memoryCLIChatID(opts); chatID != "" {
		ids = append(ids, memory.ChatScope(memoryCLIChannel(opts), chatID))
	}
	if workdir := firstNonEmpty(opts.project, os.Getenv("NECTAR_WORKING_DIR")); workdir != "" {
		ids = append(ids, memory.ProjectScopeAliases(workdir)...)
	}
	return ids
}

func memoryCLIChannel(opts memoryCLIOptions) string {
	return firstNonEmpty(opts.channel, os.Getenv("NECTAR_CHANNEL"))
}

func memoryCLIProjectDir(opts memoryCLIOptions) (string, error) {
	if project := firstNonEmpty(opts.project, os.Getenv("NECTAR_WORKING_DIR")); project != "" {
		return project, nil
	}
	workdir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return workdir, nil
}

func memoryCLIID(positional []string) (string, error) {
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return "", errors.New("exactly one memory id is required")
	}
	return strings.TrimSpace(positional[0]), nil
}

func parseMemoryKind(value string) (memory.Kind, error) {
	switch kind := memory.Kind(strings.ToLower(strings.TrimSpace(value))); kind {
	case memory.KindProfile, memory.KindProject, memory.KindFact, memory.KindLesson, memory.KindEpisode:
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported memory kind %q", value)
	}
}

func parseMemoryStatus(value string) (memory.Status, error) {
	switch status := memory.Status(strings.ToLower(strings.TrimSpace(value))); status {
	case memory.StatusCandidate, memory.StatusActive, memory.StatusArchived:
		return status, nil
	case "pending":
		return memory.StatusCandidate, nil
	default:
		return "", fmt.Errorf("unsupported memory status %q", value)
	}
}

func parseMemoryListStatus(value string) (memory.Status, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "active":
		return memory.StatusActive, nil
	case "pending", "candidate":
		return memory.StatusCandidate, nil
	case "archived":
		return memory.StatusArchived, nil
	case "all":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported memory status %q", value)
	}
}

func parseMemoryTags(value string) []string {
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func printMemoryCLIItem(item memory.Memory) {
	fmt.Printf("%s\t%s\t%s\t%s\t%s\n",
		item.ID, item.Kind, item.Status, item.Scope, truncateMemoryCLIText(item.Content, 160))
}

func printMemoryCLIDetail(item memory.Memory) {
	fmt.Printf("id: %s\nkind: %s\nstatus: %s\nscope: %s\n", item.ID, item.Kind, item.Status, item.Scope)
	if item.ScopeID != "" {
		fmt.Printf("scope_id: %s\n", item.ScopeID)
	}
	if item.ProjectID != "" {
		fmt.Printf("project_id: %s\n", item.ProjectID)
	}
	if item.DeviceID != "" {
		fmt.Printf("device_id: %s\n", item.DeviceID)
	}
	if item.Locality != "" {
		fmt.Printf("locality: %s\n", item.Locality)
	}
	if item.Revision > 0 {
		fmt.Printf("revision: %d\n", item.Revision)
	}
	if item.UseCount > 0 {
		fmt.Printf("uses: %d\n", item.UseCount)
	}
	if item.Source != "" {
		fmt.Printf("source: %s\n", item.Source)
	}
	if item.Confidence > 0 {
		fmt.Printf("confidence: %.2f\n", item.Confidence)
	}
	if item.Pinned {
		fmt.Println("pinned: true")
	}
	fmt.Printf("created: %s\nupdated: %s\n\n%s\n", item.CreatedAt.Format("2006-01-02 15:04:05"), item.UpdatedAt.Format("2006-01-02 15:04:05"), item.Content)
}

func truncateMemoryCLIText(value string, max int) string {
	flat := strings.Join(strings.Fields(value), " ")
	runes := []rune(flat)
	if len(runes) <= max {
		return flat
	}
	return string(runes[:max]) + "..."
}
