package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		commit       string
		date         string
		wantContains []string
	}{
		{
			name:         "full release metadata",
			version:      "1.0.0",
			commit:       "abc1234",
			date:         "2026-06-16T00:00:00Z",
			wantContains: []string{"driftlens", "1.0.0", "abc1234", "2026-06-16T00:00:00Z"},
		},
		{
			name:         "dev build with no commit or date",
			version:      "dev",
			commit:       "",
			date:         "",
			wantContains: []string{"driftlens", "dev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionString(tt.version, tt.commit, tt.date)
			for _, w := range tt.wantContains {
				if !strings.Contains(got, w) {
					t.Errorf("versionString(%q,%q,%q) = %q, missing %q", tt.version, tt.commit, tt.date, got, w)
				}
			}
			if strings.HasSuffix(got, "\n") {
				t.Errorf("versionString should not end with newline, got %q", got)
			}
		})
	}
}

func TestEffectiveVersion(t *testing.T) {
	mod := func(v string) *debug.BuildInfo {
		bi := &debug.BuildInfo{}
		bi.Main.Version = v
		return bi
	}
	tests := []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		want    string
	}{
		{"ldflags version wins over buildinfo", "1.0.0", mod("v9.9.9"), "1.0.0"},
		{"falls back to module version from go install", "dev", mod("v1.2.3"), "v1.2.3"},
		{"ignores devel placeholder", "dev", mod("(devel)"), "dev"},
		{"no build info stays dev", "dev", nil, "dev"},
		{"empty module version stays dev", "dev", mod(""), "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveVersion(tt.ldflags, tt.info); got != tt.want {
				t.Errorf("effectiveVersion(%q, %v) = %q, want %q", tt.ldflags, tt.info, got, tt.want)
			}
		})
	}
}
