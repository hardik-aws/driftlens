package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hardik-aws/driftlens/internal/model"
)

func mkDir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestRemoveCache(t *testing.T) {
	t.Run("terraform removes .terraform only", func(t *testing.T) {
		dir := t.TempDir()
		mkDir(t, filepath.Join(dir, ".terraform"))
		mkDir(t, filepath.Join(dir, ".terragrunt-cache"))
		mkFile(t, filepath.Join(dir, "tfplan"))
		mkFile(t, filepath.Join(dir, ".terraform.lock.hcl"))

		if err := removeCache(dir, "terraform"); err != nil {
			t.Fatalf("removeCache: %v", err)
		}
		if exists(filepath.Join(dir, ".terraform")) {
			t.Error(".terraform should be removed")
		}
		if !exists(filepath.Join(dir, ".terragrunt-cache")) {
			t.Error(".terragrunt-cache should be untouched for terraform")
		}
		if !exists(filepath.Join(dir, "tfplan")) {
			t.Error("tfplan should be kept")
		}
		if !exists(filepath.Join(dir, ".terraform.lock.hcl")) {
			t.Error(".terraform.lock.hcl should be kept")
		}
	})

	t.Run("terragrunt removes both caches", func(t *testing.T) {
		dir := t.TempDir()
		mkDir(t, filepath.Join(dir, ".terraform"))
		mkDir(t, filepath.Join(dir, ".terragrunt-cache"))
		mkFile(t, filepath.Join(dir, "tfplan"))

		if err := removeCache(dir, "terragrunt"); err != nil {
			t.Fatalf("removeCache: %v", err)
		}
		if exists(filepath.Join(dir, ".terraform")) {
			t.Error(".terraform should be removed")
		}
		if exists(filepath.Join(dir, ".terragrunt-cache")) {
			t.Error(".terragrunt-cache should be removed for terragrunt")
		}
		if !exists(filepath.Join(dir, "tfplan")) {
			t.Error("tfplan should be kept")
		}
	})

	t.Run("missing dir is not an error", func(t *testing.T) {
		if err := removeCache(t.TempDir(), "terraform"); err != nil {
			t.Fatalf("removeCache on empty dir: %v", err)
		}
	})
}

func TestEvaluateCleanupRemovesCache(t *testing.T) {
	dir := t.TempDir()
	mkDir(t, filepath.Join(dir, ".terraform", "providers"))
	fc := &fakeCommander{exit: map[string]int{dir + "/init": 0, dir + "/plan": 2}}

	Run(context.Background(), []model.Module{{Dir: dir, Tool: "terraform"}},
		Options{Commander: fc, Parallelism: 1, Cleanup: true})

	if exists(filepath.Join(dir, ".terraform")) {
		t.Error(".terraform should be removed after evaluate when Cleanup=true")
	}
}

func TestTerragruntCleanupRunsAfterPass2(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	mkDir(t, filepath.Join(modDir, ".terraform"))
	mkDir(t, filepath.Join(modDir, ".terragrunt-cache"))

	fc := &fakeCommander{
		exit: map[string]int{
			root + "/run":         0,
			modDir + "/show-json": 0, modDir + "/show": 0,
		},
	}
	Run(context.Background(), []model.Module{{Dir: modDir, Tool: "terragrunt"}},
		Options{Commander: fc, Parallelism: 1, Root: root, Cleanup: true})

	if exists(filepath.Join(modDir, ".terraform")) {
		t.Error(".terraform should be removed after Pass 2 when Cleanup=true")
	}
	if exists(filepath.Join(modDir, ".terragrunt-cache")) {
		t.Error(".terragrunt-cache should be removed after Pass 2 when Cleanup=true")
	}
}

func TestEvaluateCleanupDisabledKeepsCache(t *testing.T) {
	dir := t.TempDir()
	mkDir(t, filepath.Join(dir, ".terraform"))
	fc := &fakeCommander{exit: map[string]int{dir + "/init": 0, dir + "/plan": 2}}

	Run(context.Background(), []model.Module{{Dir: dir, Tool: "terraform"}},
		Options{Commander: fc, Parallelism: 1, Cleanup: false})

	if !exists(filepath.Join(dir, ".terraform")) {
		t.Error(".terraform should remain when Cleanup=false")
	}
}
