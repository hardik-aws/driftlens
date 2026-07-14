package report

import (
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{42 * time.Second, "42s"},
		{3*time.Minute + 42*time.Second, "3m42s"},
		{time.Minute + 5*time.Second, "1m05s"},
		{time.Hour + 4*time.Minute, "1h04m"},
		{2*time.Hour + 30*time.Minute + 9*time.Second, "2h30m"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
