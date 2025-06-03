package repository

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tmunongo/rivet/config"
	"github.com/tmunongo/rivet/executor"
)

type Repository struct {
	Config config.RepositoryConfig
	Executor executor.CommandExecutor
	logger *slog.Logger
	workingPath string
	isInitialised bool
}

func NewRepository(cfg config.RepositoryConfig, exec executor.CommandExecutor, logger *slog.Logger) *Repository {
	return &Repository{
		Config: cfg,
		Executor: exec,
		logger: logger,
	}
}

func (r *Repository) getWorkingPath() (string, error) {
	if r.workingPath != "" {
		return r.workingPath, nil
	}
	if r.Config.BasePath == "" || r.Config.CloneDirName == "" {
				return "", fmt.Errorf("basePath or cloneDirName is empty in repository config")
	}

	absBasePath, err := filepath.Abs(r.Config.BasePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for basePath '%s': %w", r.Config.BasePath, err)
	}

	r.workingPath = filepath.Join(absBasePath, r.Config.CloneDirName)
	return r.workingPath, nil
}

func (r *Repository) ensureCloned(ctx context.Context) error {
	if r.isInitialised {
		return nil
	}

	workDir, err := r.getWorkingPath()
	if err != nil {
		return fmt.Errorf("could not determine working path: %w", err)
	}

	r.logger.Info("Ensuring repository is cloned", "targetPath", workDir)

	gitDirPath := filepath.Join(workDir, ".git")
	if _, err := os.Stat(gitDirPath); err == nil {
		// .git directory exists, assume it's cloned
		r.logger.Info("Repository already exists.", "path", workDir)
		r.isInitialised = true
		return nil
	} else if !os.IsNotExist(err) {
		// Some other error checking .git (e.g., permission denied)
		return fmt.Errorf("failed to check for existing .git directory at '%s': %w", gitDirPath, err)
	}

	// .git does not exist, attempt to clone
	r.logger.Info("Repository not found locally, attempting to clone...", "url", r.Config.GitURL, "branch", r.Config.Branch)

	// Ensure base path exists
	if _, err := os.Stat(r.Config.BasePath); os.IsNotExist(err) {
		r.logger.Info("Base path does not exist, creating it.", "basePath", r.Config.BasePath)
		if err := os.MkdirAll(r.Config.BasePath, 0755); err != nil {
			return fmt.Errorf("failed to create base path '%s': %w", r.Config.BasePath, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check base path '%s': %w", r.Config.BasePath, err)
	}

	// Command: git clone -b <branch> <url> <cloneDirName>
	// Executor runs commands *in* a working directory. For clone, the working dir is BasePath.
	args := []string{"clone", "-b", r.Config.Branch, r.Config.GitURL, r.Config.CloneDirName}
	stdout, stderr, exitCode, err := r.Executor.Execute(ctx, r.Config.BasePath, "git", args...)

	if err != nil {
		r.logger.Error("Git clone command execution failed", "error", err, "stdout", stdout, "stderr", stderr, "exitCode", exitCode)
		return fmt.Errorf("git clone execution failed: %w", err)
	}
	if exitCode != 0 {
		r.logger.Error("Git clone command returned non-zero exit code", "exitCode", exitCode, "stdout", stdout, "stderr", stderr)
		return fmt.Errorf("git clone command failed with exit code %d. Stderr: %s", exitCode, stderr)
	}

	r.logger.Info("Git clone successful.", "stdout", stdout)
	r.isInitialised = true
	return nil
}

func (r *Repository) CheckForUpdates(ctx context.Context) (bool, error) {
	if !r.isInitialised {
		return false, fmt.Errorf("repository not initialized, call EnsureCloned first")
	}
	workDir, _ := r.getWorkingPath() // Error already checked in EnsureCloned
	r.logger.Debug("Checking for updates...")

	// 1. Fetch updates from remote
	r.logger.Debug("Running 'git fetch'...", "branch", r.Config.Branch)
	fetchArgs := []string{"fetch", "origin", r.Config.Branch, "--prune"}
	_, stderrFetch, exitCodeFetch, errFetch := r.Executor.Execute(ctx, workDir, "git", fetchArgs...)
	if errFetch != nil || exitCodeFetch != 0 {
		r.logger.Error("Git fetch failed", "error", errFetch, "exitCode", exitCodeFetch, "stderr", stderrFetch)
		return false, fmt.Errorf("git fetch failed (exit %d): %w. Stderr: %s", exitCodeFetch, errFetch, stderrFetch)
	}
	r.logger.Debug("'git fetch' successful.")

	// 2. Get local HEAD commit
	localCommitArgs := []string{"rev-parse", "HEAD"}
	localCommitOut, stderrLocal, exitCodeLocal, errLocal := r.Executor.Execute(ctx, workDir, "git", localCommitArgs...)
	if errLocal != nil || exitCodeLocal != 0 {
		r.logger.Error("Failed to get local HEAD commit", "error", errLocal, "exitCode", exitCodeLocal, "stderr", stderrLocal)
		return false, fmt.Errorf("failed to get local HEAD (exit %d): %w. Stderr: %s", exitCodeLocal, errLocal, stderrLocal)
	}
	localCommit := strings.TrimSpace(localCommitOut)
	r.logger.Debug("Local commit", "sha", localCommit)

	// 3. Get remote HEAD commit for the tracked branch
	remoteRef := fmt.Sprintf("origin/%s", r.Config.Branch)
	remoteCommitArgs := []string{"rev-parse", remoteRef}
	remoteCommitOut, stderrRemote, exitCodeRemote, errRemote := r.Executor.Execute(ctx, workDir, "git", remoteCommitArgs...)
	if errRemote != nil || exitCodeRemote != 0 {
		r.logger.Error("Failed to get remote commit", "remoteRef", remoteRef, "error", errRemote, "exitCode", exitCodeRemote, "stderr", stderrRemote)
		return false, fmt.Errorf("failed to get remote commit for '%s' (exit %d): %w. Stderr: %s", remoteRef, exitCodeRemote, errRemote, stderrRemote)
	}
	remoteCommit := strings.TrimSpace(remoteCommitOut)
	r.logger.Debug("Remote commit", "sha", remoteCommit, "remoteRef", remoteRef)

	if localCommit == remoteCommit {
		r.logger.Info("No updates found. Local and remote are at the same commit.", "commit", localCommit)
		return false, nil
	}

	// 4. Check if local is an ancestor of remote (i.e., behind)
	ancestorArgs := []string{"merge-base", "--is-ancestor", localCommit, remoteCommit}
	_, stderrAncestor, exitCodeAncestor, errAncestor := r.Executor.Execute(ctx, workDir, "git", ancestorArgs...)
	if errAncestor != nil && exitCodeAncestor != 0 && exitCodeAncestor != 1 { // error other than typical non-ancestor exit code 1
		r.logger.Error("Git merge-base command execution failed", "error", errAncestor, "exitCode", exitCodeAncestor, "stderr", stderrAncestor)
		return false, fmt.Errorf("git merge-base execution failed (exit %d): %w. Stderr: %s", exitCodeAncestor, errAncestor, stderrAncestor)
	}

	if exitCodeAncestor == 0 { // localCommit is an ancestor of remoteCommit (and they are different)
		r.logger.Info("Updates found!", "localCommit", localCommit, "remoteCommit", remoteCommit)
		return true, nil
	}
	// exitCodeAncestor == 1 means local is not an ancestor (diverged, or local is ahead).
	// Other exit codes are actual errors handled above.
	r.logger.Info("Local commit is not a simple ancestor of remote. Possible divergence or local is ahead. No auto-pull.", "local", localCommit, "remote", remoteCommit)
	return false, nil
}

func (r *Repository) PullChanges(ctx context.Context) error {
	if !r.isInitialised {
		return fmt.Errorf("repository not initialized")
	}
	workDir, _ := r.getWorkingPath()
	r.logger.Info("Pulling changes...", "branch", r.Config.Branch)

	args := []string{"pull", "origin", r.Config.Branch, "--ff-only"}
	stdout, stderr, exitCode, err := r.Executor.Execute(ctx, workDir, "git", args...)
	if err != nil || exitCode != 0 {
		r.logger.Error("Git pull failed", "error", err, "exitCode", exitCode, "stdout", stdout, "stderr", stderr)
		return fmt.Errorf("git pull failed (exit %d): %w. Stderr: %s", exitCode, err, stderr)
	}
	r.logger.Info("'git pull' successful.", "stdout", stdout)
	return nil
}

// BuildContainers builds the Docker containers using docker compose.
func (r *Repository) BuildContainers(ctx context.Context) error {
	if !r.isInitialised {
		return fmt.Errorf("repository not initialised")
	}
	workDir, _ := r.getWorkingPath()
	
	composeFilePath := r.Config.ComposeFile
	if !filepath.IsAbs(composeFilePath) {
		composeFilePath = filepath.Join(workDir, composeFilePath)
	}

	r.logger.Info("Building containers...", "service", r.Config.ServiceName, "composeFile", composeFilePath)
	
	args := []string{"compose", "-f", composeFilePath, "build", "--pull"} // --pull attempts to pull newer base images
	if r.Config.ServiceName != "" {
		args = append(args, r.Config.ServiceName)
	}

	stdout, stderr, exitCode, err := r.Executor.Execute(ctx, workDir, "docker", args...)
	if err != nil || exitCode != 0 {
		r.logger.Error("Docker-compose build failed", "error", err, "exitCode", exitCode, "stdout", stdout, "stderr", stderr)
		return fmt.Errorf("docker compose build failed (exit %d): %w. Stderr: %s", exitCode, err, stderr)
	}
	r.logger.Info("'docker compose build' successful.", "stdout", stdout)
	return nil
}

func (r *Repository) DeployContainers(ctx context.Context) error {
	if !r.isInitialised {
		return fmt.Errorf("repository not initialised")
	}
	workDir, _ := r.getWorkingPath()

	composeFilePath := r.Config.ComposeFile
	if !filepath.IsAbs(composeFilePath) {
		composeFilePath = filepath.Join(workDir, composeFilePath)
	}
	serviceName := r.Config.ServiceName

	r.logger.Info("Deploying containers...", "service", serviceName, "composeFile", composeFilePath)

	initialScale := 1 // TODO: Make this configurable or detect current scale
	targetScaleUp := initialScale + 1
	finalScale := initialScale

	// Step 1: Scale up
	r.logger.Info("Scaling up service", "service", serviceName, "targetInstances", targetScaleUp)
	upArgs := []string{
		"compose", "-f", composeFilePath, "up", "-d",
		"--no-deps",
		"--scale", fmt.Sprintf("%s=%d", serviceName, targetScaleUp),
		"--no-recreate", // Important: don't stop existing, just add new
		serviceName,     // Specify service for --no-recreate to apply correctly
	}
	stdoutUp, stderrUp, exitCodeUp, errUp := r.Executor.Execute(ctx, workDir, "docker", upArgs...)
	if errUp != nil || exitCodeUp != 0 {
		r.logger.Error("Docker-compose scale up failed", "error", errUp, "exitCode", exitCodeUp, "stdout", stdoutUp, "stderr", stderrUp)
		return fmt.Errorf("docker compose scale up failed (exit %d): %w. Stderr: %s", exitCodeUp, errUp, stderrUp)
	}
	r.logger.Info("Service scaled up successfully.", "stdout", stdoutUp)

	// Step 2: Simplified Health Check (wait)
	healthCheckDelay := 30 * time.Second
	r.logger.Info("Waiting for new container to stabilize (simulated health check)...", "duration", healthCheckDelay)
	select {
	case <-time.After(healthCheckDelay):
		// Continue
	case <-ctx.Done():
		r.logger.Warn("Context cancelled during health check wait")
		return ctx.Err()
	}

	// Step 3: Scale down
	r.logger.Info("Scaling down service", "service", serviceName, "targetInstances", finalScale)
	downArgs := []string{
		"compose", "-f", composeFilePath, "up", "-d",
		"--scale", fmt.Sprintf("%s=%d", serviceName, finalScale),
		"--no-recreate", // Ensure it removes an old one, not the one just started
		serviceName,
	}
	stdoutDown, stderrDown, exitCodeDown, errDown := r.Executor.Execute(ctx, workDir, "docker", downArgs...)
	if errDown != nil || exitCodeDown != 0 {
		r.logger.Error("Docker-compose scale down failed", "error", errDown, "exitCode", exitCodeDown, "stdout", stdoutDown, "stderr", stderrDown)
		// This is critical, service might be in an inconsistent state
		return fmt.Errorf("docker compose scale down failed (exit %d): %w. Stderr: %s", exitCodeDown, errDown, stderrDown)
	}
	r.logger.Info("Service scaled down successfully. Deployment complete.", "stdout", stdoutDown)
	return nil
}

// Process checks for updates and, if found, pulls, builds, and deploys.
// This is the main entry point for periodic checks on a repository.
func (r *Repository) Process(ctx context.Context) error {
	// Ensure cloned should be called first if not already initialized.
	if err := r.ensureCloned(ctx); err != nil {
		r.logger.Error("Failed to ensure repository is cloned/initialized", "error", err)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}
	// Context check after potentially long operation
	if ctx.Err() != nil { r.logger.Info("Context cancelled after ensureCloned"); return ctx.Err() }


	r.logger.Info("Processing repository")
	updatesFound, err := r.CheckForUpdates(ctx)
	if err != nil {
		r.logger.Error("Failed to check for updates", "error", err)
		return fmt.Errorf("update check failed: %w", err)
	}
    if ctx.Err() != nil { r.logger.Info("Context cancelled after CheckForUpdates"); return ctx.Err() }


	if !updatesFound {
		r.logger.Info("No updates found. Nothing to do.")
		return nil
	}

	r.logger.Info("Updates detected. Starting deployment process...")
	if err := r.PullChanges(ctx); err != nil {
		r.logger.Error("Failed to pull changes", "error", err)
		return fmt.Errorf("pull changes failed: %w", err)
	}
    if ctx.Err() != nil { r.logger.Info("Context cancelled after PullChanges"); return ctx.Err() }


	if err := r.BuildContainers(ctx); err != nil {
		r.logger.Error("Failed to build containers", "error", err)
		return fmt.Errorf("build containers failed: %w", err)
	}
    if ctx.Err() != nil { r.logger.Info("Context cancelled after BuildContainers"); return ctx.Err() }


	if err := r.DeployContainers(ctx); err != nil {
		r.logger.Error("Failed to deploy containers", "error", err)
		return fmt.Errorf("deploy containers failed: %w", err)
	}
    if ctx.Err() != nil { r.logger.Info("Context cancelled after DeployContainers"); return ctx.Err() }


	r.logger.Info("Repository processed and deployed successfully.")
	return nil
}