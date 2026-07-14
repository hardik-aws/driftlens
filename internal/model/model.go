// Package model defines the shared types used across drift-detect.
package model

import "time"

// Status is the drift outcome for a single module.
type Status string

const (
	StatusClean Status = "clean"
	StatusDrift Status = "drift"
	StatusError Status = "error"
)

// Module is a discovered Terraform/Terragrunt root directory to evaluate.
type Module struct {
	Dir  string // PATH-relative directory
	Tool string // "terraform" | "terragrunt"
}

// ResourceChange describes a single drifted resource from a plan.
type ResourceChange struct {
	Address string   `json:"address"`
	Action  string   `json:"action"`            // create | update | delete | replace | read
	Changed []string `json:"changed,omitempty"` // top-level attributes that changed
	Detail  string   `json:"detail,omitempty"`  // raw human-readable plan diff block
}

// Result is the outcome of running init+plan against a Module.
type Result struct {
	Dir      string           `json:"dir"`
	Tool     string           `json:"tool"`
	Status   Status           `json:"status"`
	Drifted  []ResourceChange `json:"drifted,omitempty"`
	Err      string           `json:"error,omitempty"`
	Duration time.Duration    `json:"duration_ms"`
}

// ExitCode aggregates results into a process exit code.
// Precedence: any error -> 1, else any drift -> 2, else 0.
func ExitCode(results []Result) int {
	code := 0
	for _, r := range results {
		switch r.Status {
		case StatusError:
			return 1
		case StatusDrift:
			code = 2
		}
	}
	return code
}
