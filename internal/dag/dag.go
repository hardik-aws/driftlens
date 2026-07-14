// Package dag parses a terragrunt dependency graph (DOT digraph) into a
// queryable structure with topological levels. An edge "A" -> "B" means A
// depends on B.
package dag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Graph is a resolved dependency graph.
type Graph struct {
	nodes      map[string]bool
	deps       map[string]map[string]bool // node -> set of its dependencies
	dependents map[string]map[string]bool // node -> set of nodes depending on it
}

var (
	edgeRe = regexp.MustCompile(`"([^"]+)"\s*->\s*"([^"]+)"`)
	// nodeRe matches a standalone node declaration: a quoted name terminated by
	// `;`. Unanchored so it works whether declarations sit on their own line
	// (real `terragrunt dag graph` output) or inline (e.g. `digraph { "x" ; }`).
	nodeRe = regexp.MustCompile(`"([^"]+)"\s*;`)
)

// Parse reads a DOT digraph emitted by `terragrunt dag graph`. It records every
// node and every edge; an edge "A" -> "B" means A depends on B. Parse returns an
// error if the graph contains a cycle.
func Parse(dot string) (*Graph, error) {
	g := &Graph{
		nodes:      map[string]bool{},
		deps:       map[string]map[string]bool{},
		dependents: map[string]map[string]bool{},
	}
	for _, line := range strings.Split(dot, "\n") {
		for _, m := range edgeRe.FindAllStringSubmatch(line, -1) {
			g.addEdge(m[1], m[2])
		}
		for _, m := range nodeRe.FindAllStringSubmatch(line, -1) {
			g.addNode(m[1])
		}
	}
	if _, err := g.levels(); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Graph) addNode(n string) {
	g.nodes[n] = true
	if g.deps[n] == nil {
		g.deps[n] = map[string]bool{}
	}
	if g.dependents[n] == nil {
		g.dependents[n] = map[string]bool{}
	}
}

func (g *Graph) addEdge(from, to string) {
	g.addNode(from)
	g.addNode(to)
	g.deps[from][to] = true
	g.dependents[to][from] = true
}

// Nodes returns all node names, sorted.
func (g *Graph) Nodes() []string { return sortedKeys(g.nodes) }

// Deps returns the sorted dependencies of node (what it waits for).
func (g *Graph) Deps(node string) []string { return sortedKeys(g.deps[node]) }

// Dependents returns the sorted nodes that depend on node.
func (g *Graph) Dependents(node string) []string { return sortedKeys(g.dependents[node]) }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Levels returns nodes grouped into topological levels: level 0 has no
// dependencies, level n depends only on nodes in earlier levels. Within a level
// nodes are sorted. Returns an error if the graph has a cycle.
func (g *Graph) Levels() ([][]string, error) { return g.levels() }

func (g *Graph) levels() ([][]string, error) {
	indeg := map[string]int{} // remaining unresolved dependencies
	for n := range g.nodes {
		indeg[n] = len(g.deps[n])
	}
	resolved := map[string]bool{}
	var out [][]string
	for len(resolved) < len(g.nodes) {
		var level []string
		for n := range g.nodes {
			if !resolved[n] && indeg[n] == 0 {
				level = append(level, n)
			}
		}
		if len(level) == 0 {
			return nil, fmt.Errorf("dependency cycle detected")
		}
		sort.Strings(level)
		for _, n := range level {
			resolved[n] = true
			for d := range g.dependents[n] {
				indeg[d]--
			}
		}
		out = append(out, level)
	}
	return out, nil
}
