package main

import "testing"

func TestEffectiveDetailed(t *testing.T) {
	tests := []struct {
		name       string
		detailed   bool
		reportMode string
		want       bool
	}{
		{"explicit flag, no report", true, "none", true},
		{"no flag, no report", false, "none", false},
		{"no flag but html report forces detail", false, "html", true},
		{"no flag but pdf report forces detail", false, "pdf", true},
		{"no flag but both report forces detail", false, "both", true},
		{"flag and report", true, "both", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveDetailed(tt.detailed, tt.reportMode); got != tt.want {
				t.Errorf("effectiveDetailed(%v, %q) = %v, want %v", tt.detailed, tt.reportMode, got, tt.want)
			}
		})
	}
}
