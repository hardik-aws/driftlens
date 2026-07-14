// Package discover walks a path and finds Terraform/Terragrunt root modules.
package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hardik-aws/driftlens/internal/model"
)

// Find walks root recursively and returns every directory that is a module for
// the given tool. terraform modules contain a *.tf file; terragrunt modules
// contain a terragrunt.hcl file. Hidden dirs and .terraform/.git are skipped.
func Find(root, tool string) ([]model.Module, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	var mods []model.Module
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDir(d.Name()) {
			return fs.SkipDir
		}
		if isModule(path, tool) {
			mods = append(mods, model.Module{Dir: path, Tool: tool})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mods, nil
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isModule(dir, tool string) bool {
	if tool == "terragrunt" {
		_, err := os.Stat(filepath.Join(dir, "terragrunt.hcl"))
		return err == nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tf"))
	return len(matches) > 0
}
