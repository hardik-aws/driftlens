package main

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
		ok   bool
	}{
		{"debug", slog.LevelDebug, true},
		{"info", slog.LevelInfo, true},
		{"INFO", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"", slog.LevelInfo, false},
		{"trace", slog.LevelInfo, false},
	}
	for _, tt := range tests {
		got, err := parseLevel(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("parseLevel(%q) ok = %v, want %v (err %v)", tt.in, err == nil, tt.ok, err)
		}
		if tt.ok && got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
