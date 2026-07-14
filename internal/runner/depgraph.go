package runner

import (
	"context"
	"fmt"

	"github.com/hardik-aws/driftlens/internal/dag"
)

// DependencyGraph resolves the terragrunt dependency DAG at root by shelling
// `terragrunt dag graph` (modern, ≥0.73) or `graph-dependencies` (classic),
// both of which emit a DOT digraph. It returns (nil, nil) for terraform, which
// has no DAG concept. Any command or parse failure returns (nil, err); callers
// degrade by omitting the dependency view rather than failing the run.
func DependencyGraph(ctx context.Context, root, tool string, opts Options) (*dag.Graph, error) {
	if tool != "terragrunt" {
		return nil, nil
	}
	args := []string{"graph-dependencies"}
	if terragruntModernCLI(ctx, root, opts) {
		args = []string{"dag", "graph"}
	}
	stdout, stderr, code, err := opts.Commander.Run(ctx, root, "terragrunt", args...)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("%s", errMsg("dag graph", err, stderr, code))
	}
	return dag.Parse(string(stdout))
}
