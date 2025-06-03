package watcher

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/tmunongo/rivet/config"
	"github.com/tmunongo/rivet/executor"
	"github.com/tmunongo/rivet/repository"
)

// Watcher manages the monitoring of multiple repositories.
type Watcher struct {
	AppConfig *config.AppConfig
	Executor  executor.CommandExecutor
	logger    *slog.Logger
	repos     []*repository.Repository
}

// NewWatcher creates a new Watcher instance.
func NewWatcher(appCfg *config.AppConfig, exec executor.CommandExecutor, logger *slog.Logger) *Watcher {
	w := &Watcher{
		AppConfig: appCfg,
		Executor:  exec,
		logger:    logger,
	}

	for _, repoCfg := range appCfg.Repositories {
		// Create a child logger for each repository for contextual logging
		repoLogger := logger.With("repositoryPath", filepath.Join(repoCfg.BasePath, repoCfg.CloneDirName), "branch", repoCfg.Branch)
		w.repos = append(w.repos, repository.NewRepository(repoCfg, exec, repoLogger))
	}
	return w
}

// Run starts the monitoring process for all configured repositories.
// It blocks until the provided context is cancelled and all repository goroutines have finished.
func (w *Watcher) Run(ctx context.Context) {
	w.logger.Info("Watcher started. Monitoring repositories...")
	var wg sync.WaitGroup

	for _, repoInstance := range w.repos {
		wg.Add(1)
		go func(repo *repository.Repository) {
			defer wg.Done()
			// Pass the logger from the repo struct, which is already contextualized
			w.monitorRepository(ctx, repo, repo.Config.CheckIntervalSeconds)
		}(repoInstance)
	}

	// Wait for all monitoring goroutines to complete.
	// This happens when the context is cancelled and each goroutine respects it.
	wg.Wait()
	w.logger.Info("Watcher stopped. All repository monitors shut down.")
}

// monitorRepository handles the periodic checking for a single repository.
func (w *Watcher) monitorRepository(ctx context.Context, repo *repository.Repository, checkIntervalSec int) {
	repoLogger := w.logger
	repoLogger.Info("Starting monitoring for repository", "intervalSeconds", checkIntervalSec)

	// Initial check immediately, but respect context cancellation.
	repoLogger.Info("Performing initial check...")
	if err := repo.Process(ctx); err != nil {
		repoLogger.Error("Error during initial processing", "error", err)
	}
	// Check if context was cancelled during initial process
	if ctx.Err() != nil {
		repoLogger.Info("Monitoring stopped during initial process due to context cancellation.")
		return
	}

	ticker := time.NewTicker(time.Duration(checkIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			repoLogger.Info("Scheduled check triggered.")
			if err := repo.Process(ctx); err != nil {
				repoLogger.Error("Error during scheduled processing", "error", err)
			}
			// Check if context was cancelled during this process
			if ctx.Err() != nil {
				repoLogger.Info("Monitoring stopped during scheduled process due to context cancellation.")
				return
			}
		case <-ctx.Done():
			repoLogger.Info("Monitoring stopping due to context cancellation signal.")
			return
		}
	}
}