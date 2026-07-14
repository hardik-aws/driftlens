package report

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/hardik-aws/driftlens/internal/dag"
	"github.com/hardik-aws/driftlens/internal/model"
)

// DepNode is one module positioned in the dependency graph with its drift status.
type DepNode struct {
	Name       string // graph node name (root-relative path)
	Dir        string // matched Result.Dir ("" if no result matched)
	Status     string // clean | drift | error | unknown
	Level      int    // topological level for layout
	DependsOn  []string
	RequiredBy []string
}

// DependencyView is the report-ready dependency graph: nodes carrying drift
// status plus the topological level grouping used for layout.
type DependencyView struct {
	Nodes  []DepNode
	Levels [][]string
}

// BuildDependencyView joins a parsed dependency graph with per-module results,
// tagging each graph node with its drift status. root is the scan root, used to
// resolve graph node names (root-relative) against Result.Dir. Returns nil when
// g is nil. A node with no matching result is tagged "unknown".
func BuildDependencyView(g *dag.Graph, results []model.Result, root string) *DependencyView {
	if g == nil {
		return nil
	}
	dirs := make([]string, len(results))
	statusOf := map[string]string{}
	for i, r := range results {
		dirs[i] = r.Dir
		statusOf[r.Dir] = string(r.Status)
	}

	levels, _ := g.Levels() // g came from a successful Parse; cycle already ruled out
	levelOf := map[string]int{}
	for i, lv := range levels {
		for _, n := range lv {
			levelOf[n] = i
		}
	}

	var nodes []DepNode
	for _, n := range g.Nodes() {
		dir, ok := matchDir(dirs, root, n)
		status := "unknown"
		if ok {
			status = statusOf[dir]
		}
		nodes = append(nodes, DepNode{
			Name:       n,
			Dir:        dir,
			Status:     status,
			Level:      levelOf[n],
			DependsOn:  g.Deps(n),
			RequiredBy: g.Dependents(n),
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Level != nodes[j].Level {
			return nodes[i].Level < nodes[j].Level
		}
		return nodes[i].Name < nodes[j].Name
	})
	return &DependencyView{Nodes: nodes, Levels: levels}
}

// matchDir resolves node (root-relative) to a Result.Dir. It tries an exact
// join(root,node) match, then a bare match, then a path-suffix match, so it
// works whether Result.Dir is absolute, root-prefixed, or bare.
func matchDir(dirs []string, root, node string) (string, bool) {
	target := filepath.Clean(filepath.Join(root, node))
	for _, d := range dirs {
		if filepath.Clean(d) == target {
			return d, true
		}
	}
	bare := filepath.Clean(node)
	for _, d := range dirs {
		if filepath.Clean(d) == bare {
			return d, true
		}
	}
	suffix := string(filepath.Separator) + bare
	for _, d := range dirs {
		if strings.HasSuffix(filepath.Clean(d), suffix) {
			return d, true
		}
	}
	return "", false
}
