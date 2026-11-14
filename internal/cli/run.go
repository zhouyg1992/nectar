package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// Deps carries host process details used by the CLI.
type Deps struct {
	Version string
	RunBot  func(configPath string) error
}

// Run dispatches the top-level Nectar CLI.
func Run(args []string, deps Deps) error {
	if len(args) > 0 {
		switch args[0] {
		case "service":
			return runService(args[1:])
		case "update":
			return runUpdate(args[1:], deps.Version)
		case "schedule":
			return runSchedule(args[1:])
		case "skills":
			return runSkills(args[1:])
		case "memory":
			return runMemory(args[1:])
		case "uninstall":
			return runUninstall(args[1:])
		case "help", "-h", "--help":
			printMainUsage()
			return nil
		}
	}

	fs := flag.NewFlagSet("nectar", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Println("nectar", deps.Version)
		return nil
	}

	return deps.RunBot(*configPath)
}

func printMainUsage() {
	fmt.Print(`Usage:
  nectar [-config PATH]
  nectar -version
  nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart] [--timeout 10m]
  nectar schedule <list|add|preview|pause|resume|run|delete|update>
  nectar skills <install|uninstall>
  nectar memory <add|search|list|get|approve|reject|pin|unpin|forget>
  nectar uninstall [--purge-data] [--keep-binary] [--no-skills] [--dry-run]
  nectar service <install|uninstall|start|stop|restart|status|logs>
`)
}
