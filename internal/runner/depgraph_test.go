package runner

import (
	"context"
	"testing"
)

func TestDependencyGraphParsesModern(t *testing.T) {
	fc := &fakeCommander{
		stdout: map[string]string{
			"root/--version": "terragrunt version 0.80.4",
			"root/dag": `digraph {
        "vpc" ;
        "eks-cluster" ;
        "eks-cluster" -> "vpc";
}`,
		},
	}
	g, err := DependencyGraph(context.Background(), "root", "terragrunt", Options{Commander: fc})
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Deps("eks-cluster"); len(got) != 1 || got[0] != "vpc" {
		t.Errorf("Deps(eks-cluster) = %v, want [vpc]", got)
	}
}

func TestDependencyGraphNilForTerraform(t *testing.T) {
	fc := &fakeCommander{}
	g, err := DependencyGraph(context.Background(), "root", "terraform", Options{Commander: fc})
	if g != nil || err != nil {
		t.Fatalf("terraform DependencyGraph = (%v, %v), want (nil, nil)", g, err)
	}
	if len(fc.calls) != 0 {
		t.Errorf("terraform must not shell terragrunt: %v", fc.calls)
	}
}

func TestDependencyGraphErrorOnFailure(t *testing.T) {
	fc := &fakeCommander{
		stdout: map[string]string{"root/--version": "terragrunt version 0.80.4"},
		exit:   map[string]int{"root/dag": 1},
		stderr: map[string]string{"root/dag": "boom"},
	}
	if _, err := DependencyGraph(context.Background(), "root", "terragrunt", Options{Commander: fc}); err == nil {
		t.Fatal("want error on non-zero exit")
	}
}
