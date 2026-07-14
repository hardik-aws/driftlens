package main

import "testing"

func TestPluginCacheEnv(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/root"}

	t.Run("empty dir returns base unchanged", func(t *testing.T) {
		got := pluginCacheEnv(base, "")
		if len(got) != len(base) {
			t.Fatalf("len = %d, want %d: %v", len(got), len(base), got)
		}
		for _, e := range got {
			if e == "TF_PLUGIN_CACHE_DIR=" {
				t.Errorf("should not add empty TF_PLUGIN_CACHE_DIR: %v", got)
			}
		}
	})

	t.Run("non-empty dir appends one entry", func(t *testing.T) {
		got := pluginCacheEnv(base, "/cache/tf")
		if len(got) != len(base)+1 {
			t.Fatalf("len = %d, want %d: %v", len(got), len(base)+1, got)
		}
		if got[len(got)-1] != "TF_PLUGIN_CACHE_DIR=/cache/tf" {
			t.Errorf("last entry = %q, want TF_PLUGIN_CACHE_DIR=/cache/tf", got[len(got)-1])
		}
	})
}

func TestSafeParallelism(t *testing.T) {
	cases := []struct {
		name        string
		parallelism int
		cacheDir    string
		tool        string
		want        int
		clamped     bool
	}{
		{"terraform no cache keeps parallelism", 4, "", "terraform", 4, false},
		{"terraform cache clamps to 1", 4, "/cache/tf", "terraform", 1, true},
		{"terraform cache with 1 unchanged", 1, "/cache/tf", "terraform", 1, false},
		{"terragrunt cache keeps parallelism", 4, "/cache/tf", "terragrunt", 4, false},
		{"terragrunt no cache keeps parallelism", 4, "", "terragrunt", 4, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, clamped := safeParallelism(c.parallelism, c.cacheDir, c.tool)
			if got != c.want || clamped != c.clamped {
				t.Errorf("safeParallelism(%d, %q, %q) = (%d, %v), want (%d, %v)",
					c.parallelism, c.cacheDir, c.tool, got, clamped, c.want, c.clamped)
			}
		})
	}
}
