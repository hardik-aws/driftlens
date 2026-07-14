package runner

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

// ExecCommander runs real terraform/terragrunt binaries via os/exec.
type ExecCommander struct {
	// Env, when non-empty, replaces the child process environment. Empty means
	// inherit the parent (driftlens) environment.
	Env []string
}

// Run executes `name args...` in dir. A non-zero process exit is reported via
// exitCode (err stays nil); err is set only when the process fails to start.
func (c ExecCommander) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(c.Env) > 0 {
		cmd.Env = c.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.Bytes(), stderr.Bytes(), ee.ExitCode(), nil
	}
	return stdout.Bytes(), stderr.Bytes(), -1, err
}
