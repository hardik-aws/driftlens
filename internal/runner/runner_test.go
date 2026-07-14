package runner

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hardik-aws/driftlens/internal/model"
)

// fakeCommander returns scripted exit codes/output keyed by "dir:subcommand".
type fakeCommander struct {
	mu      sync.Mutex
	active  int32
	maxSeen int32
	exit    map[string]int      // key "dir/plan" etc -> exit code
	stdout  map[string]string   // key -> stdout
	stderr  map[string]string   // key -> stderr
	calls   []string            // resolved keys in invocation order
	argv    map[string][]string // key -> last full args seen
	argvAll [][]string          // every call's full args, in invocation order
}

func (f *fakeCommander) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, int, error) {
	n := atomic.AddInt32(&f.active, 1)
	for {
		m := atomic.LoadInt32(&f.maxSeen)
		if n <= m || atomic.CompareAndSwapInt32(&f.maxSeen, m, n) {
			break
		}
	}
	defer atomic.AddInt32(&f.active, -1)

	sub := args[0]
	key := dir + "/" + sub
	if sub == "show" {
		key = dir + "/show" // human plan text
		for _, a := range args {
			if a == "-json" {
				key = dir + "/show-json"
				break
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	if f.argv == nil {
		f.argv = map[string][]string{}
	}
	f.argv[key] = args
	f.argvAll = append(f.argvAll, args)
	return []byte(f.stdout[key]), []byte(f.stderr[key]), f.exit[key], nil
}

func resultByDir(rs []model.Result, dir string) model.Result {
	for _, r := range rs {
		if r.Dir == dir {
			return r
		}
	}
	return model.Result{}
}

func TestRunMapsExitCodes(t *testing.T) {
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
		stderr: map[string]string{"broken/plan": "boom"},
	}
	rs := Run(context.Background(), mods, Options{Commander: fc, Parallelism: 2})

	if got := resultByDir(rs, "clean").Status; got != model.StatusClean {
		t.Errorf("clean status = %q", got)
	}
	if got := resultByDir(rs, "drift").Status; got != model.StatusDrift {
		t.Errorf("drift status = %q", got)
	}
	br := resultByDir(rs, "broken")
	if br.Status != model.StatusError {
		t.Errorf("broken status = %q", br.Status)
	}
	if !strings.Contains(br.Err, "boom") {
		t.Errorf("broken err = %q, want contains boom", br.Err)
	}
}

func TestRunInitFailureIsError(t *testing.T) {
	mods := []model.Module{{Dir: "d", Tool: "terraform"}}
	fc := &fakeCommander{
		exit:   map[string]int{"d/init": 1, "d/plan": 0},
		stderr: map[string]string{"d/init": "init failed"},
	}
	rs := Run(context.Background(), mods, Options{Commander: fc, Parallelism: 1})
	if rs[0].Status != model.StatusError {
		t.Fatalf("status = %q, want error", rs[0].Status)
	}
	if !strings.Contains(rs[0].Err, "init failed") {
		t.Errorf("err = %q", rs[0].Err)
	}
}

func TestRunDetailedParsesResources(t *testing.T) {
	planJSON := `{"resource_changes":[
		{"address":"aws_s3_bucket.a","change":{"actions":["update"],
			"before":{"acl":"private","tags":{"a":"1"}},
			"after":{"acl":"public","tags":{"a":"1"}}}},
		{"address":"aws_iam_role.b","change":{"actions":["no-op"]}},
		{"address":"aws_instance.c","change":{"actions":["delete","create"]}},
		{"address":"aws_bucket.d","change":{"actions":["create"]}}
	]}`
	planText := `Terraform will perform the following actions:

  # aws_s3_bucket.a will be updated in-place
  ~ resource "aws_s3_bucket" "a" {
      ~ acl = "private" -> "public"
    }

  # aws_bucket.d will be created
  + resource "aws_bucket" "d" {}

Plan: 1 to add, 1 to change, 0 to destroy.
`
	mods := []model.Module{{Dir: "d", Tool: "terraform"}}
	fc := &fakeCommander{
		exit:   map[string]int{"d/init": 0, "d/plan": 2, "d/show-json": 0, "d/show": 0},
		stdout: map[string]string{"d/show-json": planJSON, "d/show": planText},
	}
	rs := Run(context.Background(), mods, Options{Commander: fc, Parallelism: 1, Detailed: true})
	got := rs[0].Drifted // sorted by address, no-op excluded
	want := []model.ResourceChange{
		{Address: "aws_bucket.d", Action: "create"},
		{Address: "aws_instance.c", Action: "replace"},
		{Address: "aws_s3_bucket.a", Action: "update", Changed: []string{"acl"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Address != want[i].Address || got[i].Action != want[i].Action {
			t.Errorf("change[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if strings.Join(got[i].Changed, ",") != strings.Join(want[i].Changed, ",") {
			t.Errorf("change[%d].Changed = %v, want %v", i, got[i].Changed, want[i].Changed)
		}
	}
	// per-resource human diff text attached where the plan provided it
	if d := got[2].Detail; !strings.Contains(d, `~ acl = "private" -> "public"`) {
		t.Errorf("aws_s3_bucket.a Detail missing diff body:\n%s", d)
	}
	if d := got[1].Detail; d != "" {
		t.Errorf("aws_instance.c had no plan-text block, Detail should be empty, got:\n%s", d)
	}
}

func TestRunRespectsParallelism(t *testing.T) {
	var mods []model.Module
	exit := map[string]int{}
	for i := 0; i < 20; i++ {
		d := fmt.Sprintf("d%d", i)
		mods = append(mods, model.Module{Dir: d, Tool: "terraform"})
		exit[d+"/init"] = 0
		exit[d+"/plan"] = 0
	}
	fc := &fakeCommander{exit: exit}
	Run(context.Background(), mods, Options{Commander: fc, Parallelism: 3})
	if fc.maxSeen > 3 {
		t.Errorf("max concurrent = %d, want <= 3", fc.maxSeen)
	}
}

func TestRunAllClassicSyntaxOnOldTerragrunt(t *testing.T) {
	fc := &fakeCommander{
		stdout: map[string]string{"root/--version": "terragrunt version v0.55.1"},
	}
	stderr, code, err := runAll(context.Background(), "root", Options{Commander: fc, Parallelism: 3})
	if err != nil || code != 0 {
		t.Fatalf("runAll = (%q, %d, %v), want (_, 0, nil)", stderr, code, err)
	}
	if len(fc.calls) != 3 || fc.calls[0] != "root/--version" ||
		fc.calls[1] != "root/run-all" || fc.calls[2] != "root/run-all" {
		t.Fatalf("calls = %v, want [root/--version root/run-all root/run-all]", fc.calls)
	}
	gotInit := strings.Join(fc.argvAll[1], " ")
	wantInit := "run-all init -input=false --terragrunt-non-interactive --terragrunt-parallelism 3"
	if gotInit != wantInit {
		t.Errorf("init args = %q, want %q", gotInit, wantInit)
	}
	gotPlan := strings.Join(fc.argvAll[2], " ")
	wantPlan := "run-all plan -out=tfplan -input=false -lock=false --terragrunt-non-interactive --terragrunt-parallelism 3"
	if gotPlan != wantPlan {
		t.Errorf("plan args = %q, want %q", gotPlan, wantPlan)
	}
}

func TestRunAllModernSyntaxOnNewTerragrunt(t *testing.T) {
	fc := &fakeCommander{
		stdout: map[string]string{"root/--version": "terragrunt version 0.80.4"},
	}
	stderr, code, err := runAll(context.Background(), "root", Options{Commander: fc, Parallelism: 3})
	if err != nil || code != 0 {
		t.Fatalf("runAll = (%q, %d, %v), want (_, 0, nil)", stderr, code, err)
	}
	if len(fc.calls) != 3 || fc.calls[1] != "root/run" || fc.calls[2] != "root/run" {
		t.Fatalf("calls = %v, want version probe then two root/run", fc.calls)
	}
	gotInit := strings.Join(fc.argvAll[1], " ")
	wantInit := "run --all --non-interactive --parallelism 3 -- init -input=false"
	if gotInit != wantInit {
		t.Errorf("init args = %q, want %q", gotInit, wantInit)
	}
	gotPlan := strings.Join(fc.argvAll[2], " ")
	wantPlan := "run --all --non-interactive --parallelism 3 -- plan -out=tfplan -input=false -lock=false"
	if gotPlan != wantPlan {
		t.Errorf("plan args = %q, want %q", gotPlan, wantPlan)
	}
}

func TestRunAllInitCarriesUpgradeWhenSet(t *testing.T) {
	fc := &fakeCommander{
		stdout: map[string]string{"root/--version": "terragrunt version 0.80.4"},
	}
	runAll(context.Background(), "root", Options{Commander: fc, Parallelism: 2, Upgrade: true})
	gotInit := strings.Join(fc.argvAll[1], " ")
	wantInit := "run --all --non-interactive --parallelism 2 -- init -input=false -upgrade"
	if gotInit != wantInit {
		t.Errorf("init args = %q, want %q", gotInit, wantInit)
	}
	// plan must not carry -upgrade
	if strings.Contains(strings.Join(fc.argvAll[2], " "), "-upgrade") {
		t.Errorf("plan args unexpectedly carry -upgrade: %v", fc.argvAll[2])
	}
}

func TestRunAllDefaultsToModernWhenVersionUnknown(t *testing.T) {
	// no stdout scripted for the version probe -> unparseable output
	fc := &fakeCommander{}
	runAll(context.Background(), "root", Options{Commander: fc, Parallelism: 1})
	if len(fc.calls) != 3 || fc.calls[1] != "root/run" || fc.calls[2] != "root/run" {
		t.Fatalf("calls = %v, want modern root/run (init+plan) when version unknown", fc.calls)
	}
}

func TestEvaluateShowBuildsResultFromPlan(t *testing.T) {
	planJSON := `{"resource_changes":[
		{"address":"aws_s3_bucket.a","change":{"actions":["update"],
			"before":{"acl":"private"},"after":{"acl":"public"}}}
	]}`
	fc := &fakeCommander{
		exit:   map[string]int{"d/show-json": 0, "d/show": 0},
		stdout: map[string]string{"d/show-json": planJSON},
	}
	r := evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Logger: slog.New(slog.DiscardHandler)})
	if r.Status != model.StatusDrift {
		t.Fatalf("status = %q, want drift", r.Status)
	}
	if len(r.Drifted) != 1 || r.Drifted[0].Address != "aws_s3_bucket.a" {
		t.Fatalf("drifted = %+v", r.Drifted)
	}
}

func TestEvaluateShowReplansWhenCachedPlanMissing(t *testing.T) {
	// Pass 1's tfplan isn't in the cache the standalone show regenerates, so
	// show fails; evaluateShow must fall back to an in-place plan.
	fc := &fakeCommander{
		exit: map[string]int{"d/show-json": 1, "d/plan": 0},
	}
	r := evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Logger: slog.New(slog.DiscardHandler)})
	if r.Status != model.StatusClean {
		t.Fatalf("status = %q, want clean", r.Status)
	}
	sawPlan := false
	for _, c := range fc.calls {
		if c == "d/plan" {
			sawPlan = true
		}
	}
	if !sawPlan {
		t.Errorf("expected fallback plan call, calls = %v", fc.calls)
	}
}

func TestEvaluateShowFallbackInitsBeforeReplan(t *testing.T) {
	// The standalone show regenerated a fresh cache that is neither
	// backend-initialised nor provider-upgraded, so the fallback must run
	// init (carrying -upgrade when set) before plan — otherwise plan fails
	// with "Backend initialization required" / "must use init -upgrade".
	fc := &fakeCommander{
		exit: map[string]int{"d/show-json": 1, "d/init": 0, "d/plan": 0},
	}
	evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Upgrade: true, Logger: slog.New(slog.DiscardHandler)})

	var initIdx, planIdx = -1, -1
	for i, c := range fc.calls {
		switch c {
		case "d/init":
			initIdx = i
		case "d/plan":
			planIdx = i
		}
	}
	if initIdx == -1 || planIdx == -1 || initIdx > planIdx {
		t.Fatalf("want init before plan in fallback, calls = %v", fc.calls)
	}
	if got := strings.Join(fc.argv["d/init"], " "); got != "init -input=false -upgrade" {
		t.Errorf("fallback init args = %q, want %q", got, "init -input=false -upgrade")
	}
}

func TestEvaluateShowFallbackInitErrorIsError(t *testing.T) {
	// A failing fallback init surfaces as an error, not a plan attempt.
	fc := &fakeCommander{
		exit:   map[string]int{"d/show-json": 1, "d/init": 1},
		stderr: map[string]string{"d/init": "backend init failed"},
	}
	r := evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Logger: slog.New(slog.DiscardHandler)})
	if r.Status != model.StatusError {
		t.Fatalf("status = %q, want error", r.Status)
	}
	if !strings.Contains(r.Err, "backend init failed") {
		t.Errorf("err = %q", r.Err)
	}
}

func TestEvaluateShowFallbackPlanErrorIsError(t *testing.T) {
	// show fails (no cached plan) and the fallback plan also fails -> error.
	fc := &fakeCommander{
		exit:   map[string]int{"d/show-json": 1, "d/plan": 1},
		stderr: map[string]string{"d/plan": "backend init failed"},
	}
	r := evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Logger: slog.New(slog.DiscardHandler)})
	if r.Status != model.StatusError {
		t.Fatalf("status = %q, want error", r.Status)
	}
	if !strings.Contains(r.Err, "backend init failed") {
		t.Errorf("err = %q", r.Err)
	}
}

func TestEvaluateShowCleanWhenNoChanges(t *testing.T) {
	fc := &fakeCommander{
		exit: map[string]int{"d/show-json": 0, "d/show": 0},
		stdout: map[string]string{
			"d/show-json": `{"resource_changes":[{"address":"a","change":{"actions":["no-op"]}}]}`,
		},
	}
	r := evaluateShow(context.Background(), model.Module{Dir: "d", Tool: "terragrunt"},
		Options{Commander: fc, Logger: slog.New(slog.DiscardHandler)})
	if r.Status != model.StatusClean {
		t.Fatalf("status = %q, want clean", r.Status)
	}
}

func TestTerragruntRunsRunAllBeforeShow(t *testing.T) {
	mods := []model.Module{
		{Dir: "root/a", Tool: "terragrunt"},
		{Dir: "root/b", Tool: "terragrunt"},
	}
	fc := &fakeCommander{
		stdout: map[string]string{"root/--version": "terragrunt version 0.80.4"},
		exit: map[string]int{
			"root/run":         0,
			"root/a/show-json": 0, "root/a/show": 0,
			"root/b/show-json": 0, "root/b/show": 0,
		},
	}
	Run(context.Background(), mods, Options{Commander: fc, Parallelism: 2, Root: "root"})

	if len(fc.calls) < 3 || fc.calls[0] != "root/--version" ||
		fc.calls[1] != "root/run" || fc.calls[2] != "root/run" {
		t.Fatalf("calls = %v, want version probe then run-all init+plan before shows", fc.calls)
	}
	for _, c := range fc.calls[3:] {
		if c == "root/run" || c == "root/run-all" {
			t.Fatalf("run-all invoked more than twice (init+plan): %v", fc.calls)
		}
	}
}

func TestRunAllArgsInjectProviderCache(t *testing.T) {
	modern := runAllArgs(true, 4, "/cache", "init", "-input=false")
	got := strings.Join(modern, " ")
	want := "run --all --non-interactive --parallelism 4 --provider-cache --provider-cache-dir /cache -- init -input=false"
	if got != want {
		t.Errorf("modern = %q, want %q", got, want)
	}

	classic := runAllArgs(false, 4, "/cache", "init", "-input=false")
	gotC := strings.Join(classic, " ")
	wantC := "run-all init -input=false --terragrunt-non-interactive --terragrunt-parallelism 4 --terragrunt-provider-cache --terragrunt-provider-cache-dir /cache"
	if gotC != wantC {
		t.Errorf("classic = %q, want %q", gotC, wantC)
	}

	none := runAllArgs(true, 4, "", "init")
	if strings.Contains(strings.Join(none, " "), "provider-cache") {
		t.Errorf("empty dir must inject no provider-cache flags: %v", none)
	}
}

func TestTerraformInitCarriesUpgradeWhenSet(t *testing.T) {
	fc := &fakeCommander{exit: map[string]int{"d/init": 0, "d/plan": 0}}
	Run(context.Background(), []model.Module{{Dir: "d", Tool: "terraform"}},
		Options{Commander: fc, Parallelism: 1, Upgrade: true})
	got := strings.Join(fc.argv["d/init"], " ")
	if got != "init -input=false -upgrade" {
		t.Errorf("init args = %q, want %q", got, "init -input=false -upgrade")
	}
}

func TestTerraformNeverCallsRunAll(t *testing.T) {
	fc := &fakeCommander{exit: map[string]int{"d/init": 0, "d/plan": 0}}
	Run(context.Background(), []model.Module{{Dir: "d", Tool: "terraform"}}, Options{Commander: fc, Parallelism: 1})
	for _, c := range fc.calls {
		if strings.HasSuffix(c, "/run-all") {
			t.Fatalf("terraform path called run-all: %v", fc.calls)
		}
	}
}

func TestDetailedLogsWhenPlanTextUnavailable(t *testing.T) {
	planJSON := `{"resource_changes":[
		{"address":"aws_s3_bucket.a","change":{"actions":["update"],
			"before":{"acl":"private"},"after":{"acl":"public"}}}
	]}`
	fc := &fakeCommander{
		exit:   map[string]int{"d/init": 0, "d/plan": 2, "d/show-json": 0, "d/show": 1},
		stdout: map[string]string{"d/show-json": planJSON},
		stderr: map[string]string{"d/show": "plan file not found"},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	rs := Run(context.Background(), []model.Module{{Dir: "d", Tool: "terraform"}},
		Options{Commander: fc, Parallelism: 1, Detailed: true, Logger: logger})

	got := rs[0].Drifted
	if len(got) != 1 {
		t.Fatalf("want 1 drifted resource, got %d: %+v", len(got), got)
	}
	if got[0].Detail != "" {
		t.Errorf("Detail should be empty when plan text unavailable, got %q", got[0].Detail)
	}
	if !strings.Contains(buf.String(), "plan-text diff unavailable") {
		t.Errorf("expected warn log about missing plan text, got:\n%s", buf.String())
	}
}
