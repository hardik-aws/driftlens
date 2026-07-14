package dag

import "testing"

const fixture = `digraph {
        "aws-backup" ;
        "aws-datasync" ;
        "aws-datasync" -> "vpc";
        "aws-efs" ;
        "aws-efs" -> "vpc";
        "eks-cluster" ;
        "eks-cluster" -> "vpc";
        "eks-cluster/services/cfs-admin-api" ;
        "eks-cluster/services/cfs-admin-api" -> "eks-cluster";
        "vpc" ;
}`

func containsDag(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestParseNodesAndEdges(t *testing.T) {
	g, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Deps("eks-cluster"); len(got) != 1 || got[0] != "vpc" {
		t.Errorf("Deps(eks-cluster) = %v, want [vpc]", got)
	}
	if got := g.Deps("aws-backup"); len(got) != 0 {
		t.Errorf("Deps(aws-backup) = %v, want [] (isolated node)", got)
	}
	if got := g.Dependents("vpc"); !containsDag(got, "eks-cluster") || !containsDag(got, "aws-efs") {
		t.Errorf("Dependents(vpc) = %v, want to include eks-cluster and aws-efs", got)
	}
	if got := g.Deps("eks-cluster/services/cfs-admin-api"); len(got) != 1 || got[0] != "eks-cluster" {
		t.Errorf("Deps(service) = %v, want [eks-cluster]", got)
	}
}

func TestLevels(t *testing.T) {
	g, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	lv, err := g.Levels()
	if err != nil {
		t.Fatal(err)
	}
	if len(lv) != 3 {
		t.Fatalf("levels = %d, want 3: %v", len(lv), lv)
	}
	if !containsDag(lv[0], "vpc") || !containsDag(lv[0], "aws-backup") {
		t.Errorf("level 0 = %v, want vpc + aws-backup", lv[0])
	}
	if !containsDag(lv[1], "eks-cluster") || !containsDag(lv[1], "aws-efs") {
		t.Errorf("level 1 = %v, want eks-cluster + aws-efs", lv[1])
	}
	if !containsDag(lv[2], "eks-cluster/services/cfs-admin-api") {
		t.Errorf("level 2 = %v, want the service", lv[2])
	}
}

func TestParseCycleReturnsError(t *testing.T) {
	_, err := Parse(`digraph {
        "a" -> "b";
        "b" -> "a";
}`)
	if err == nil {
		t.Fatal("want cycle error, got nil")
	}
}
