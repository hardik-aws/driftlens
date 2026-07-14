package main

import "testing"

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		posArgs []string
		want    string
	}{
		{"default", "", nil, "."},
		{"positional", "", []string{"/infra"}, "/infra"},
		{"flag", "/live", nil, "/live"},
		{"flag wins over positional", "/live", []string{"/infra"}, "/live"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePath(tt.flag, tt.posArgs); got != tt.want {
				t.Errorf("resolvePath(%q, %v) = %q, want %q", tt.flag, tt.posArgs, got, tt.want)
			}
		})
	}
}
