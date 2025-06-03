package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// CommandExecutor defines an interface for running external commands.
type CommandExecutor interface {
	Execute(ctx context.Context, workingDir string, command string, args ...string) (stdout string, stderr string, exitCode int, err error)
}

// OSCommandExecutor is the concrete implementation that uses os/exec.
type OSCommandExecutor struct{}

// NewOSCommandExecutor creates a new instance of OSCommandExecutor.
func NewOSCommandExecutor() *OSCommandExecutor {
	return &OSCommandExecutor{}
}

// Execute runs the given command with arguments in the specified working directory.
// It returns the standard output, standard error, exit code, and any Go error encountered.
func (e *OSCommandExecutor) Execute(ctx context.Context, workingDir string, command string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run() // Run waits for the command to complete.

	stdout := outBuf.String()
	stderr := errBuf.String()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Command started and exited with non-zero status
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = -1 // Could not determine exit code
			}
			// Return the Go error as well, which includes stderr if captured by ExitError
			return stdout, stderr, exitCode, fmt.Errorf("command '%s %s' failed with exit code %d: %w. Stderr: %s", command, strings.Join(args, " "), exitCode, err, stderr)
		}
		// Other errors (e.g., command not found, context cancelled before start)
		exitCode = -1 // Indicate a failure before or during execution not related to command's own exit status
		return stdout, stderr, exitCode, fmt.Errorf("failed to run command '%s %s': %w. Stderr: %s", command, strings.Join(args, " "), err, stderr)
	}

	// Success
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		exitCode = status.ExitStatus()
	}

	return stdout, stderr, exitCode, nil
}