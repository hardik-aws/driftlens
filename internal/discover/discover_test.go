package discover

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// write creates file at dir/name with empty content, making dirs as needed.
func write(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dirsOf(root, tool string, t *testing.T) []string {
	t.Helper()
	mods, err := Find(root, tool)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	var out []string
	for _, m := range mods {
		rel, _ := filepath.Rel(root, m.Dir)
		out = append(out, rel)
		if m.Tool != tool {
			t.Errorf("module %s tool = %q, want %q", rel, m.Tool, tool)
		}
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFindTerraformModules(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.tf")
	write(t, root, "svc-a/main.tf")
	write(t, root, "svc-b/nested/main.tf")
	write(t, root, "docs/readme.md") // no .tf, skip

	eq(t, dirsOf(root, "terraform", t), []string{".", "svc-a", "svc-b/nested"})
}

func TestFindTerragruntModules(t *testing.T) {
	root := t.TempDir()
	write(t, root, "svc-a/terragrunt.hcl")
	write(t, root, "svc-a/main.tf") // has .tf too, but tool=terragrunt
	write(t, root, "svc-b/main.tf") // only .tf, not a terragrunt module

	eq(t, dirsOf(root, "terragrunt", t), []string{"svc-a"})
}

func TestSkipsHiddenAndVendorDirs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.tf")
	write(t, root, ".terraform/modules/x/main.tf")
	write(t, root, ".git/hooks/main.tf")
	write(t, root, ".hidden/main.tf")

	eq(t, dirsOf(root, "terraform", t), []string{"."})
}

func TestErrorsOnMissingPath(t *testing.T) {
	if _, err := Find(filepath.Join(t.TempDir(), "nope"), "terraform"); err == nil {
		t.Fatal("expected error for missing path")
	}
}
