package model

import "testing"

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		in   []Result
		want int
	}{
		{"empty", nil, 0},
		{"all clean", []Result{{Status: StatusClean}, {Status: StatusClean}}, 0},
		{"drift wins over clean", []Result{{Status: StatusClean}, {Status: StatusDrift}}, 2},
		{"error wins over drift", []Result{{Status: StatusDrift}, {Status: StatusError}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.in); got != tt.want {
				t.Errorf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}
