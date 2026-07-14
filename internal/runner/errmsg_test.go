package runner

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[90m16:20:23\x1b[m \x1b[31mSTDERR\x1b[m plain \x1b[1m\x1b[31mError:\x1b[0m boom"
	got := stripANSI(in)
	want := "16:20:23 STDERR plain Error: boom"
	if got != want {
		t.Errorf("stripANSI = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("escape survived: %q", got)
	}
}

func TestErrMsgStripsANSI(t *testing.T) {
	stderr := []byte("\x1b[31m\xe2\x95\xb7\x1b[0m\n\x1b[31m\xe2\x94\x82\x1b[0m \x1b[1m\x1b[31mError: \x1b[0mFailed to load plugin schemas")
	got := errMsg("plan", nil, stderr, 1)
	if strings.Contains(got, "\x1b") {
		t.Errorf("errMsg leaked ANSI escapes: %q", got)
	}
	if !strings.Contains(got, "Failed to load plugin schemas") {
		t.Errorf("errMsg dropped message text: %q", got)
	}
}
