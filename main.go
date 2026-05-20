package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"obsidian-remotely-sync-cli/config"
	synce "obsidian-remotely-sync-cli/sync"
)

var (
	version = "dev"
)

const usage = `obsidian-remotely-sync-cli — encrypted Obsidian vault sync via S3

Usage:
  obsidian-remotely-sync-cli [flags] <command>

Commands:
  init      Create a default config file at the config path
  status    Show pending sync actions without making changes
  pull      Download and decrypt remote files to the local vault
  push      Encrypt and upload local vault files to remote
  sync      Bidirectional sync (respects direction + conflict config)

Flags:
`

func main() {
	// Global flags
	configPath := flag.String("config", "", "path to config file (default: ~/.obsidian-remotely-sync-cli/config.yaml)")
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	showVersion := flag.Bool("version", false, "print version and exit")
	dryRun := flag.Bool("dry-run", false, "show what would be done without making changes")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("obsidian-remotely-sync-cli %s\n", version)
		os.Exit(0)
	}

	logger := log.New(os.Stderr, "obsidian-remotely-sync-cli: ", log.LstdFlags)
	if *verbose {
		logger.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}

	args := flag.Args()

	// Default command is "sync" when no subcommand is given
	cmd := "sync"
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
	}

	switch cmd {
	case "init":
		runInit(cfgPath, logger)
	case "status":
		cfg := mustLoadConfig(cfgPath, logger)
		runStatus(cfg, logger, *verbose)
	case "pull":
		cfg := mustLoadConfig(cfgPath, logger)
		cfg.Sync.Direction = "pull"
		runSync(cfg, logger, *verbose, *dryRun)
	case "push":
		cfg := mustLoadConfig(cfgPath, logger)
		cfg.Sync.Direction = "push"
		runSync(cfg, logger, *verbose, *dryRun)
	case "sync":
		cfg := mustLoadConfig(cfgPath, logger)
		runSync(cfg, logger, *verbose, *dryRun)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

// mustLoadConfig loads config or exits on error.
func mustLoadConfig(path string, logger *log.Logger) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "run 'obsidian-remotely-sync-cli init' to create a default config\n")
		os.Exit(1)
	}
	return cfg
}

// newEngine creates a SyncEngine from config, connecting to S3.
func newEngine(ctx context.Context, cfg *config.Config, logger *log.Logger) *synce.SyncEngine {
	s3c, err := connectS3(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to S3: %v\n", err)
		os.Exit(1)
	}

	engine, err := synce.NewEngine(cfg, s3c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing sync engine: %v\n", err)
		os.Exit(1)
	}
	return engine
}

// runInit creates a default config file.
func runInit(cfgPath string, logger *log.Logger) {
	err := config.WriteDefault(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	logger.Printf("config created at %s", cfgPath)
	logger.Printf("edit the config file to set your vault path, S3 credentials, and encryption password")
}

// runStatus shows pending sync actions.
func runStatus(cfg *config.Config, logger *log.Logger, verbose bool) {
	ctx := context.Background()
	engine := newEngine(ctx, cfg, logger)
	defer engine.Close()

	fmt.Printf("Vault:       %s\n", cfg.VaultPath)
	fmt.Printf("Remote:      %s/%s\n", cfg.S3.Bucket, cfg.S3.Prefix)
	fmt.Printf("Direction:   %s\n", cfg.Sync.Direction)
	fmt.Printf("Conflict:    %s\n", cfg.Sync.Conflict)
	fmt.Printf("Encryption:  %s\n", cfg.Encryption.Method)
	fmt.Println()

	diffs, err := engine.Diff(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error computing diff: %v\n", err)
		os.Exit(1)
	}

	if len(diffs) == 0 {
		fmt.Println("Everything up to date.")
		return
	}

	// Show individual file actions
	if verbose {
		for _, d := range diffs {
			fmt.Printf("  [%-14s] %s\n", d.Action, d.Path)
		}
		fmt.Println()
	}

	fmt.Println(synce.DiffSummary(diffs))
}

// runSync performs sync (with optional dry-run).
func runSync(cfg *config.Config, logger *log.Logger, verbose bool, dryRun bool) {
	ctx := context.Background()
	engine := newEngine(ctx, cfg, logger)
	defer engine.Close()

	if dryRun {
		diffs, err := engine.Diff(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error computing diff: %v\n", err)
			os.Exit(1)
		}
		if len(diffs) == 0 {
			fmt.Println("Everything up to date.")
			return
		}
		fmt.Printf("[dry-run] %s\n", synce.DiffSummary(diffs))
		for _, d := range diffs {
			if d.Action != synce.ActionNone {
				fmt.Printf("  [%-14s] %s\n", d.Action, d.Path)
			}
		}
		return
	}

	result, err := engine.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync error: %v\n", err)
		os.Exit(1)
	}

	// Report results
	parts := []string{}
	if result.Pushed > 0 {
		parts = append(parts, fmt.Sprintf("%d pushed", result.Pushed))
	}
	if result.Pulled > 0 {
		parts = append(parts, fmt.Sprintf("%d pulled", result.Pulled))
	}
	if result.DeletedLocal > 0 {
		parts = append(parts, fmt.Sprintf("%d local deleted", result.DeletedLocal))
	}
	if result.DeletedRemote > 0 {
		parts = append(parts, fmt.Sprintf("%d remote deleted", result.DeletedRemote))
	}
	if len(result.Conflicts) > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts resolved", len(result.Conflicts)))
	}

	if len(parts) == 0 {
		fmt.Println("Everything up to date.")
	} else {
		fmt.Printf("Sync complete: %s\n", strings.Join(parts, ", "))
	}

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		}
		os.Exit(1)
	}

	if verbose && len(result.Conflicts) > 0 {
		fmt.Println("Conflicts resolved:")
		for _, c := range result.Conflicts {
			fmt.Printf("  %s (local: %s, remote: %s)\n", c.Path, c.LocalHash[:8], c.RemoteHash[:8])
		}
	}
}
