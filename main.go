package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tmunongo/rivet/config"
	"github.com/tmunongo/rivet/executor"
	"github.com/tmunongo/rivet/watcher"
)

var version = "dev"

func main() {
	// Setup structured logger
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	// Command-line flags
	defaultConfigPath, err := getDefaultConfigPath()
	if err != nil {
		slog.Error("Failed to determine default config path", "error", err)
		// Fallback, though this might not be ideal if user home dir is critical
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			defaultConfigPath = filepath.Join(home, ".config", "rivet", "rivet.yaml")
		} else {
			defaultConfigPath = "rivet.yaml" // Current directory as last resort
		}
		slog.Warn("Using fallback config path", "path", defaultConfigPath)
	}

	configFile := flag.String("config", defaultConfigPath, "Path to the configuration file.")
	versionFlag := flag.Bool("version", false, "Print Rivet version and exit.")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Rivet version: %s\n", version)
		return
	}

	slog.Info("Starting Rivet ", "version", version, "configFile", *configFile)

	appCfg, err := config.LoadConfig(*configFile)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}
	if len(appCfg.Repositories) == 0 {
		slog.Info("No repositories configured. Exiting.")
		os.Exit(0)
	}
	slog.Info("Configuration loaded successfully", "repositoriesCount", len(appCfg.Repositories))

	// Create command executor
	cmdExecutor := executor.NewOSCommandExecutor()

	// Create and run the watcher
	appWatcher := watcher.NewWatcher(appCfg, cmdExecutor, slog.Default().WithGroup("watcher"))

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("Received signal, initiating shutdown...", "signal", sig.String())
		cancel() // Trigger context cancellation for the watcher
	}()

	// Run the watcher
	slog.Info("Starting watcher...")
	appWatcher.Run(ctx) // This will block until ctx is cancelled and all goroutines finish

	slog.Info("Rivet CI/CD Tool shut down gracefully.")
}

func getDefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "rivet", "rivet.yaml"), nil
}
