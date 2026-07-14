package runner

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/hardik-aws/driftlens/internal/model"
)

func TestRunLogsPerModuleLevels(t *testing.T) {
	mods := []model.Module{
		{Dir: "clean", Tool: "terraform"},
		{Dir: "drift", Tool: "terraform"},
		{Dir: "broken", Tool: "terraform"},
	}
	fc := &fakeCommander{
		exit: map[string]int{
			"clean/init": 0, "clean/plan": 0,
			"drift/init": 0, "drift/plan": 2,
			"broken/init": 0, "broken/plan": 1,
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	Run(context.Background(), mods, Options{Commander: fc, Parallelism: 1, Logger: logger})

	out := buf.String()
	// each module produces a line mentioning its dir
	for _, d := range []string{"clean", "drift", "broken"} {
		if !strings.Contains(out, "dir="+d) {
			t.Errorf("log missing dir=%s\n%s", d, out)
		}
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("drift should log at WARN\n%s", out)
	}
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("error should log at ERROR\n%s", out)
	}
}

func TestRunNilLoggerSafe(t *testing.T) {
	mods := []model.Module{{Dir: "d", Tool: "terraform"}}
	fc := &fakeCommander{exit: map[string]int{"d/init": 0, "d/plan": 0}}
	// must not panic with nil Logger
	Run(context.Background(), mods, Options{Commander: fc, Parallelism: 1})
}
