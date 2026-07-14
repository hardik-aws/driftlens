package report

import (
	"testing"

	"github.com/hardik-aws/driftlens/internal/dag"
	"github.com/hardik-aws/driftlens/internal/model"
)

func containsDep(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestBuildDependencyView(t *testing.T) {
	g, err := dag.Parse(`digraph {
        "vpc" ;
        "eks-cluster" ;
        "eks-cluster" -> "vpc";
        "eks-cluster/services/api" ;
        "eks-cluster/services/api" -> "eks-cluster";
}`)
	if err != nil {
		t.Fatal(err)
	}
	results := []model.Result{
		{Dir: "infra/vpc", Status: model.StatusClean},
		{Dir: "infra/eks-cluster", Status: model.StatusDrift},
		{Dir: "infra/eks-cluster/services/api", Status: model.StatusError},
	}
	v := BuildDependencyView(g, results, "infra")
	if v == nil {
		t.Fatal("view is nil")
	}
	byName := map[string]DepNode{}
	for _, n := range v.Nodes {
		byName[n.Name] = n
	}
	if byName["vpc"].Status != "clean" {
		t.Errorf("vpc status = %q, want clean", byName["vpc"].Status)
	}
	if byName["eks-cluster"].Status != "drift" {
		t.Errorf("eks-cluster status = %q, want drift", byName["eks-cluster"].Status)
	}
	if byName["eks-cluster/services/api"].Status != "error" {
		t.Errorf("api status = %q, want error", byName["eks-cluster/services/api"].Status)
	}
	if byName["eks-cluster"].Dir != "infra/eks-cluster" {
		t.Errorf("eks-cluster Dir = %q, want infra/eks-cluster", byName["eks-cluster"].Dir)
	}
	if byName["vpc"].Level != 0 || byName["eks-cluster"].Level != 1 {
		t.Errorf("levels wrong: vpc=%d eks=%d", byName["vpc"].Level, byName["eks-cluster"].Level)
	}
	if got := byName["eks-cluster"].DependsOn; len(got) != 1 || got[0] != "vpc" {
		t.Errorf("eks DependsOn = %v, want [vpc]", got)
	}
	if got := byName["vpc"].RequiredBy; !containsDep(got, "eks-cluster") {
		t.Errorf("vpc RequiredBy = %v, want to include eks-cluster", got)
	}
}

func TestBuildDependencyViewNilGraph(t *testing.T) {
	if v := BuildDependencyView(nil, nil, "root"); v != nil {
		t.Errorf("nil graph should yield nil view, got %+v", v)
	}
}

func TestBuildDependencyViewUnknownStatus(t *testing.T) {
	g, _ := dag.Parse(`digraph { "orphan" ; }`)
	v := BuildDependencyView(g, nil, "root")
	if v.Nodes[0].Status != "unknown" {
		t.Errorf("unmatched node status = %q, want unknown", v.Nodes[0].Status)
	}
	if v.Nodes[0].Dir != "" {
		t.Errorf("unmatched node Dir = %q, want empty", v.Nodes[0].Dir)
	}
}
