package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type memoryFile struct {
	Version int      `json:"version"`
	Items   []Memory `json:"items"`
}

type FileStore struct {
	mu    sync.Mutex
	path  string
	items []Memory
	now   func() time.Time
}

func NewFileStore(dataDir string) (*FileStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data dir is empty")
	}
	dir := filepath.Join(dataDir, "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	return &FileStore{
		path: filepath.Join(dir, "memory.json"),
		now:  time.Now,
	}, nil
}

func (s *FileStore) Put(ctx context.Context, item Memory) (Memory, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := s.acquireWriteLock()
	if err != nil {
		return Memory{}, err
	}
	defer s.releaseWriteLock(lock)
	if err := s.readLocked(); err != nil {
		return Memory{}, err
	}

	now := s.now().UTC()
	item.Content = Redact(strings.TrimSpace(item.Content))
	if item.Content == "" {
		return Memory{}, errors.New("memory content is empty")
	}
	if item.Kind == "" {
		item.Kind = KindFact
	}
	if item.Scope == "" {
		item.Scope = ScopeChat
	}
	if item.ProjectID == "" && item.Scope == ScopeProject {
		item.ProjectID = strings.TrimPrefix(item.ScopeID, "project:")
	}
	if item.Locality == "" {
		item.Locality = localityForContent(item.Content)
	}
	if item.Locality == LocalityDevice && item.DeviceID == "" {
		item.DeviceID = DeviceID()
	}
	if item.Status == "" {
		item.Status = StatusCandidate
	}
	if item.Confidence <= 0 {
		item.Confidence = 0.5
	}
	if item.ID == "" {
		item.ID = newID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	item.Revision = 1
	item.ContentHash = contentHash(item.Content)
	item.Tags = normalizeTags(item.Tags)

	// Repeated /remember calls update one record instead of creating duplicates.
	for i := range s.items {
		existing := &s.items[i]
		if existing.DeletedAt != nil || existing.Scope != item.Scope || existing.ScopeID != item.ScopeID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(existing.Content), item.Content) {
			continue
		}
		existing.Tags = normalizeTags(append(existing.Tags, item.Tags...))
		existing.UpdatedAt = now
		existing.Revision++
		existing.ContentHash = item.ContentHash
		if item.Source != "" {
			existing.Source = item.Source
		}
		existing.Evidence = appendUnique(existing.Evidence, item.Evidence...)
		if item.Confidence > existing.Confidence {
			existing.Confidence = item.Confidence
		}
		if item.Status == StatusActive {
			existing.Status = StatusActive
		}
		if item.Pinned {
			existing.Pinned = true
		}
		if item.Locality != "" {
			existing.Locality = item.Locality
		}
		if err := s.persistLocked(); err != nil {
			return Memory{}, err
		}
		return *existing, nil
	}

	s.items = append(s.items, item)
	if err := s.persistLocked(); err != nil {
		return Memory{}, err
	}
	return item, nil
}

func (s *FileStore) Get(ctx context.Context, id string) (Memory, bool, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readLocked(); err != nil {
		return Memory{}, false, err
	}
	for _, item := range s.items {
		if item.ID == id && item.DeletedAt == nil {
			return item, true, nil
		}
	}
	return Memory{}, false, nil
}

func (s *FileStore) List(ctx context.Context, opts ListOptions) ([]Memory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readLocked(); err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Memory, 0, len(s.items))
	for _, item := range s.items {
		if item.DeletedAt != nil || expired(item, now) {
			continue
		}
		if opts.Status != "" && item.Status != opts.Status {
			continue
		}
		if opts.Scope != "" && item.Scope != opts.Scope {
			continue
		}
		if !inScopes(item, opts.ScopeIDs) {
			continue
		}
		out = append(out, item)
	}
	sortMemories(out)
	return out, nil
}

func (s *FileStore) Search(ctx context.Context, opts SearchOptions) ([]Memory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readLocked(); err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = s.now()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}
	terms := queryTerms(opts.Query)
	type scored struct {
		item  Memory
		score float64
	}
	var candidates []scored
	for _, item := range s.items {
		if item.Status != StatusActive || item.DeletedAt != nil || expired(item, now) {
			continue
		}
		if opts.Scope != "" && item.Scope != opts.Scope {
			continue
		}
		if !inScopes(item, opts.ScopeIDs) {
			continue
		}
		if !memoryRelevant(item, terms) {
			continue
		}
		candidates = append(candidates, scored{item: item, score: scoreMemory(item, terms, now)})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].item.UpdatedAt.After(candidates[j].item.UpdatedAt)
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]Memory, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.item)
	}
	return out, nil
}

func (s *FileStore) SetStatus(ctx context.Context, id string, status Status) error {
	return s.update(ctx, id, func(item *Memory) {
		item.Status = status
	})
}

func (s *FileStore) SetPinned(ctx context.Context, id string, pinned bool) error {
	return s.update(ctx, id, func(item *Memory) {
		item.Pinned = pinned
	})
}

func (s *FileStore) UpdateContent(ctx context.Context, id, content string) (Memory, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := s.acquireWriteLock()
	if err != nil {
		return Memory{}, err
	}
	defer s.releaseWriteLock(lock)
	if err := s.readLocked(); err != nil {
		return Memory{}, err
	}
	content = Redact(strings.TrimSpace(content))
	if content == "" {
		return Memory{}, errors.New("memory content is empty")
	}
	for i := range s.items {
		item := &s.items[i]
		if item.ID != id || item.DeletedAt != nil {
			continue
		}
		item.Content = content
		item.ContentHash = contentHash(content)
		item.Locality = localityForContent(content)
		if item.Locality == LocalityDevice && item.DeviceID == "" {
			item.DeviceID = DeviceID()
		}
		item.Revision++
		item.UpdatedAt = s.now().UTC()
		if err := s.persistLocked(); err != nil {
			return Memory{}, err
		}
		return *item, nil
	}
	return Memory{}, fmt.Errorf("memory %q not found", id)
}

func (s *FileStore) Touch(ctx context.Context, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := s.acquireWriteLock()
	if err != nil {
		return err
	}
	defer s.releaseWriteLock(lock)
	if err := s.readLocked(); err != nil {
		return err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	now := s.now().UTC()
	changed := false
	for i := range s.items {
		item := &s.items[i]
		if item.DeletedAt != nil || !want[item.ID] {
			continue
		}
		item.UseCount++
		item.LastUsedAt = &now
		item.Revision++
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistLocked()
}

func (s *FileStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := s.acquireWriteLock()
	if err != nil {
		return err
	}
	defer s.releaseWriteLock(lock)
	if err := s.readLocked(); err != nil {
		return err
	}
	now := s.now().UTC()
	for i := range s.items {
		item := &s.items[i]
		if item.ID != id || item.DeletedAt != nil {
			continue
		}
		item.DeletedAt = &now
		item.Status = StatusArchived
		item.Revision++
		item.UpdatedAt = now
		return s.persistLocked()
	}
	return fmt.Errorf("memory %q not found", id)
}

func (s *FileStore) Counts(ctx context.Context, scopeIDs []string) (map[Status]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readLocked(); err != nil {
		return nil, err
	}
	now := s.now()
	counts := make(map[Status]int)
	for _, item := range s.items {
		if item.DeletedAt != nil || expired(item, now) || !inScopes(item, scopeIDs) {
			continue
		}
		counts[item.Status]++
	}
	return counts, nil
}

func (s *FileStore) update(ctx context.Context, id string, apply func(*Memory)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := s.acquireWriteLock()
	if err != nil {
		return err
	}
	defer s.releaseWriteLock(lock)
	if err := s.readLocked(); err != nil {
		return err
	}
	for i := range s.items {
		item := &s.items[i]
		if item.ID != id || item.DeletedAt != nil {
			continue
		}
		apply(item)
		item.Revision++
		item.UpdatedAt = s.now().UTC()
		return s.persistLocked()
	}
	return fmt.Errorf("memory %q not found", id)
}

func (s *FileStore) acquireWriteLock() (*os.File, error) {
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open memory lock: %w", err)
	}
	if err := lockFile(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock memory: %w", err)
	}
	return lock, nil
}

func (s *FileStore) releaseWriteLock(lock *os.File) {
	_ = unlockFile(lock)
	_ = lock.Close()
}

// readLocked always reads the file. memory.json is small, and the CLI and the
// bot are separate processes, so caching would hide writes from each other.
func (s *FileStore) readLocked() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.items = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("read memory: %w", err)
	}
	if len(data) == 0 {
		s.items = nil
		return nil
	}
	var file memoryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("decode memory: %w", err)
	}
	if file.Version != 0 && file.Version != 1 {
		return fmt.Errorf("unsupported memory version %d", file.Version)
	}
	s.items = file.Items
	for i := range s.items {
		normalizeLoaded(&s.items[i])
	}
	return nil
}

func (s *FileStore) persistLocked() error {
	file := memoryFile{Version: 1, Items: s.items}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	temp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("create memory temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod memory temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write memory: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close memory temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace memory: %w", err)
	}
	cleanup = false
	return nil
}

func normalizeLoaded(item *Memory) {
	if redacted := Redact(item.Content); redacted != item.Content {
		item.Content = redacted
		item.ContentHash = contentHash(redacted)
		item.Revision++
	}
	if item.Kind == "" {
		item.Kind = KindFact
	}
	if item.Scope == "" {
		item.Scope = ScopeChat
	}
	if item.Status == "" {
		item.Status = StatusCandidate
	}
	if item.Confidence <= 0 {
		item.Confidence = 0.5
	}
	if item.Revision == 0 {
		item.Revision = 1
	}
	if item.ContentHash == "" {
		item.ContentHash = contentHash(item.Content)
	}
	if item.Locality == "" {
		item.Locality = localityForContent(item.Content)
	}
	if item.Locality == LocalityDevice && item.DeviceID == "" {
		item.DeviceID = DeviceID()
	}
	if item.ProjectID == "" && item.Scope == ScopeProject {
		item.ProjectID = strings.TrimPrefix(item.ScopeID, "project:")
	}
}

func inScopes(item Memory, scopeIDs []string) bool {
	if len(scopeIDs) == 0 {
		return true
	}
	if item.Scope == ScopeGlobal {
		return true
	}
	for _, id := range scopeIDs {
		if item.ScopeID == id {
			return true
		}
	}
	return false
}

func sortMemories(items []Memory) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Pinned != items[j].Pinned {
			return items[i].Pinned
		}
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
}

func scoreMemory(item Memory, terms []string, now time.Time) float64 {
	score := 0.0
	if item.Pinned {
		score += 10
	}
	switch item.Kind {
	case KindProfile:
		score += 4
	case KindProject:
		score += 3
	case KindLesson:
		score += 2.5
	case KindFact:
		score += 1.5
	case KindEpisode:
		score += 1
	}
	score += item.Confidence
	age := now.Sub(item.UpdatedAt)
	if age < 0 {
		age = 0
	}
	score += 2 / (1 + age.Hours()/(24*30))
	score += float64(item.UseCount) * 0.05

	haystack := strings.ToLower(item.Content + " " + strings.Join(item.Tags, " "))
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			score += 2
		}
	}
	return score
}

func memoryRelevant(item Memory, terms []string) bool {
	if len(terms) == 0 || item.Pinned || item.Kind == KindProfile {
		return true
	}
	haystack := strings.ToLower(item.Content + " " + strings.Join(item.Tags, " "))
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

func queryTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	seen := make(map[string]bool)
	var terms []string
	add := func(term string) {
		if len([]rune(term)) < 2 || seen[term] || len(terms) >= 120 {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}

	var latin strings.Builder
	var han []rune
	flushLatin := func() {
		add(latin.String())
		latin.Reset()
	}
	flushHan := func() {
		if len(han) == 1 {
			add(string(han))
		}
		for i := 0; i+1 < len(han); i++ {
			add(string(han[i : i+2]))
		}
		han = han[:0]
	}
	for _, r := range query {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			latin.WriteRune(r)
		default:
			flushLatin()
			flushHan()
		}
	}
	flushLatin()
	flushHan()
	return terms
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func appendUnique(base []string, values ...string) []string {
	seen := make(map[string]bool, len(base))
	for _, item := range base {
		seen[item] = true
	}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		base = append(base, item)
	}
	return base
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func localityForContent(content string) Locality {
	if strings.Contains(content, "${") {
		return LocalityTemplated
	}
	if hasAbsolutePath(content) {
		return LocalityDevice
	}
	return LocalityPortable
}

func expired(item Memory, now time.Time) bool {
	return item.ExpiresAt != nil && !item.ExpiresAt.After(now)
}

func newID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

var _ Store = (*FileStore)(nil)
