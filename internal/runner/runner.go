// Package runner executes init+plan against modules in a bounded worker pool.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hardik-aws/driftlens/internal/model"
)

// ansiEscape matches ANSI CSI sequences (colors, cursor moves) that terraform
// and terragrunt emit on stderr. Stripping them keeps captured error text
// readable across console/JSON/HTML/PDF output instead of raw escape soup.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// Commander runs a command in dir and returns stdout, stderr, exit code, err.
// err is non-nil only for failures to start/execute (not for non-zero exits).
type Commander interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// Options configures a Run.
type Options struct {
	Commander        Commander
	Parallelism      int
	Detailed         bool
	Cleanup          bool   // delete .terraform cache after each module
	Lock             bool   // hold the backend state lock during plan (-lock)
	Upgrade          bool   // pass -upgrade to init (re-selects providers, refreshes lock hashes)
	Root             string // scan root; used for terragrunt's run-all (Pass 1)
	ProviderCacheDir string // terragrunt provider-cache server dir; enables concurrency-safe caching
	Timeout          time.Duration
	Logger           *slog.Logger // nil disables logging
}

// Run evaluates every module and returns one Result per module.
func Run(ctx context.Context, mods []model.Module, opts Options) []model.Result {
	if opts.Parallelism < 1 {
		opts.Parallelism = 1
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if len(mods) > 0 && mods[0].Tool == "terragrunt" {
		return runTerragrunt(ctx, mods, opts)
	}
	return runPool(ctx, mods, opts, evaluate)
}

// runPool fans mods out across opts.Parallelism goroutines, running eval on
// each, and collects the results.
func runPool(ctx context.Context, mods []model.Module, opts Options, eval func(context.Context, model.Module, Options) model.Result) []model.Result {
	work := make(chan model.Module)
	out := make(chan model.Result)

	var wg sync.WaitGroup
	for i := 0; i < opts.Parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range work {
				out <- eval(ctx, m, opts)
			}
		}()
	}
	go func() {
		for _, m := range mods {
			work <- m
		}
		close(work)
	}()
	go func() {
		wg.Wait()
		close(out)
	}()

	var results []model.Result
	for r := range out {
		results = append(results, r)
	}
	return results
}

// tgVersion extracts major.minor from `terragrunt --version` output, e.g.
// "terragrunt version v0.55.1" or "terragrunt version 0.80.4".
var tgVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

// terragruntModernCLI probes the terragrunt binary and reports whether it
// uses the redesigned CLI (>= 0.73), which replaced `run-all` with
// `run --all`, renamed --terragrunt-* global flags, and requires terraform
// args after a `--` separator (otherwise e.g. -out is rejected as an
// unknown global flag). When the probe fails or the output is unparseable,
// assume the modern CLI — that is what current installs ship.
func terragruntModernCLI(ctx context.Context, root string, opts Options) bool {
	stdout, _, code, err := opts.Commander.Run(ctx, root, "terragrunt", "--version")
	if err != nil || code != 0 {
		return true
	}
	m := tgVersion.FindStringSubmatch(string(stdout))
	if m == nil {
		return true
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 0 || minor >= 73
}

// lockArg renders the -lock flag for plan from opts.Lock.
func lockArg(opts Options) string { return fmt.Sprintf("-lock=%t", opts.Lock) }

// runAllArgs wraps a terraform subcommand's args in terragrunt's run-all
// invocation, matching the CLI style: modern (≥0.73) uses
// `run --all ... -- <tfArgs>`, classic uses `run-all <tfArgs> --terragrunt-...`.
// When providerCacheDir is non-empty it enables terragrunt's concurrency-safe
// provider-cache server (global flag, before the `--` separator on modern).
func runAllArgs(modern bool, parallelism int, providerCacheDir string, tfArgs ...string) []string {
	p := strconv.Itoa(parallelism)
	if modern {
		args := []string{"run", "--all", "--non-interactive", "--parallelism", p}
		if providerCacheDir != "" {
			args = append(args, "--provider-cache", "--provider-cache-dir", providerCacheDir)
		}
		args = append(args, "--")
		return append(args, tfArgs...)
	}
	args := append([]string{"run-all"}, tfArgs...)
	args = append(args, "--terragrunt-non-interactive", "--terragrunt-parallelism", p)
	if providerCacheDir != "" {
		args = append(args, "--terragrunt-provider-cache", "--terragrunt-provider-cache-dir", providerCacheDir)
	}
	return args
}

// runAll resolves terragrunt's dependency DAG at the scan root in two ordered
// native commands — `run-all init` then `run-all plan` — so `dependency`
// blocks read already-materialized state instead of racing driftlens's own
// worker pool for a shared .terragrunt-cache entry. Init runs first for every
// module, then plan; a non-zero init aborts before plan. Plan writes a tfplan
// into each module's cache; Pass 2 (evaluateShow) reads that plan when present,
// else re-plans the module in place.
// The parallelism flag reuses opts.Parallelism, which callers already clamp
// to 1 when a shared plugin cache is in play (see cmd/driftlens's
// safeParallelism) — the same corruption risk applies to run-all's own
// internal scheduling.
func runAll(ctx context.Context, root string, opts Options) ([]byte, int, error) {
	modern := terragruntModernCLI(ctx, root, opts)

	initTF := []string{"init", "-input=false"}
	if opts.Upgrade {
		initTF = append(initTF, "-upgrade")
	}
	initArgs := runAllArgs(modern, opts.Parallelism, opts.ProviderCacheDir, initTF...)
	if _, stderr, code, err := opts.Commander.Run(ctx, root, "terragrunt", initArgs...); err != nil || code != 0 {
		return stderr, code, err
	}

	planArgs := runAllArgs(modern, opts.Parallelism, opts.ProviderCacheDir, "plan", "-out=tfplan", "-input=false", lockArg(opts))
	_, stderr, code, err := opts.Commander.Run(ctx, root, "terragrunt", planArgs...)
	return stderr, code, err
}

// runTerragrunt resolves the dependency tree once via run-all (Pass 1), then
// reads each module's resulting plan (Pass 2). Cache cleanup is deferred
// until after Pass 2 completes, since dependency caches live in ancestor
// module dirs and must survive until every module has read its plan.
func runTerragrunt(ctx context.Context, mods []model.Module, opts Options) []model.Result {
	if stderr, code, err := runAll(ctx, opts.Root, opts); err != nil || code != 0 {
		opts.Logger.Warn("run-all plan reported errors", "root", opts.Root,
			"stderr", strings.TrimSpace(stripANSI(string(stderr))), "exit", code)
	}

	results := runPool(ctx, mods, opts, evaluateShow)

	if opts.Cleanup {
		for _, m := range mods {
			if err := removeCache(m.Dir, m.Tool); err != nil {
				opts.Logger.Warn("cache cleanup failed", "dir", m.Dir, "error", err)
			}
		}
	}
	return results
}

// evaluateShow derives a Result for a terragrunt module after Pass 1's
// run-all. It first tries to read the tfplan Pass 1 wrote (fast path, no
// init/plan). For remote-sourced modules terragrunt regenerates a fresh
// .terragrunt-cache on the standalone `show`, so that tfplan is absent and the
// show fails; evaluateShow then re-plans in place (in the cache the failed
// show just initialised) and reads that plan instead. Either way no cache
// cleanup — runTerragrunt defers cleanup until every module is read.
func evaluateShow(ctx context.Context, m model.Module, opts Options) model.Result {
	start := time.Now()
	r := model.Result{Dir: m.Dir, Tool: m.Tool}
	opts.Logger.Debug("reading plan", "dir", m.Dir, "tool", m.Tool)
	defer func() {
		r.Duration = time.Since(start)
		logResult(opts.Logger, r)
	}()

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Fast path: read the plan Pass 1's run-all already wrote.
	if drifted, err := detailed(ctx, m, opts); err == nil {
		r.Drifted = drifted
		if len(drifted) == 0 {
			r.Status = model.StatusClean
		} else {
			r.Status = model.StatusDrift
		}
		return r
	}

	// Fallback: the cached plan wasn't found (terragrunt regenerated the
	// cache on `show`). That fresh cache is neither backend-initialised nor
	// provider-upgraded, so init it first (carrying -upgrade when set) before
	// planning — otherwise plan fails with "Backend initialization required"
	// or "must use init -upgrade". Dependencies are already resolvable because
	// Pass 1's run-all materialised them in dependency order; then read the
	// fresh plan.
	opts.Logger.Warn("cached plan unavailable; re-planning module", "dir", m.Dir)
	initArgs := []string{"init", "-input=false"}
	if opts.Upgrade {
		initArgs = append(initArgs, "-upgrade")
	}
	if _, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool, initArgs...); err != nil || code != 0 {
		r.Status = model.StatusError
		r.Err = errMsg("init", err, stderr, code)
		return r
	}
	_, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool,
		"plan", "-detailed-exitcode", "-input=false", lockArg(opts), "-out=tfplan")
	switch {
	case err != nil:
		r.Status = model.StatusError
		r.Err = errMsg("plan", err, stderr, code)
		return r
	case code == 0:
		r.Status = model.StatusClean
		return r
	case code == 2:
		r.Status = model.StatusDrift
	default:
		r.Status = model.StatusError
		r.Err = errMsg("plan", nil, stderr, code)
		return r
	}

	drifted, err := detailed(ctx, m, opts)
	if err != nil {
		r.Status = model.StatusError
		r.Err = err.Error()
		return r
	}
	r.Drifted = drifted
	return r
}

func evaluate(ctx context.Context, m model.Module, opts Options) model.Result {
	start := time.Now()
	r := model.Result{Dir: m.Dir, Tool: m.Tool}
	opts.Logger.Debug("evaluating module", "dir", m.Dir, "tool", m.Tool)
	defer func() {
		r.Duration = time.Since(start)
		logResult(opts.Logger, r)
	}()

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if opts.Cleanup {
		defer func() {
			if err := removeCache(m.Dir, m.Tool); err != nil {
				opts.Logger.Warn("cache cleanup failed", "dir", m.Dir, "error", err)
			}
		}()
	}

	// init
	initArgs := []string{"init", "-input=false"}
	if opts.Upgrade {
		initArgs = append(initArgs, "-upgrade")
	}
	if _, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool, initArgs...); err != nil || code != 0 {
		r.Status = model.StatusError
		r.Err = errMsg("init", err, stderr, code)
		return r
	}

	// plan. In detailed mode write a plan file so `show -json` can read it.
	planArgs := []string{"plan", "-detailed-exitcode", "-input=false", lockArg(opts)}
	if opts.Detailed {
		planArgs = append(planArgs, "-out=tfplan")
	}
	_, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool, planArgs...)
	switch {
	case err != nil:
		r.Status = model.StatusError
		r.Err = errMsg("plan", err, stderr, code)
		return r
	case code == 0:
		r.Status = model.StatusClean
		return r
	case code == 2:
		r.Status = model.StatusDrift
	default:
		r.Status = model.StatusError
		r.Err = errMsg("plan", nil, stderr, code)
		return r
	}

	if opts.Detailed {
		if drifted, derr := detailed(ctx, m, opts); derr != nil {
			r.Err = derr.Error()
		} else {
			r.Drifted = drifted
		}
	}
	return r
}

// removeCache deletes a module's provider/module cache to reclaim disk. It
// removes <dir>/.terraform for both tools and <dir>/.terragrunt-cache for
// terragrunt. tfplan and .terraform.lock.hcl are left in place. RemoveAll on a
// missing path returns nil, so this is safe on modules that never initialised.
func removeCache(dir, tool string) error {
	if err := os.RemoveAll(filepath.Join(dir, ".terraform")); err != nil {
		return err
	}
	if tool == "terragrunt" {
		if err := os.RemoveAll(filepath.Join(dir, ".terragrunt-cache")); err != nil {
			return err
		}
	}
	return nil
}

// detailed re-runs plan to JSON and extracts per-resource changes.
func detailed(ctx context.Context, m model.Module, opts Options) ([]model.ResourceChange, error) {
	stdout, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool, "show", "-json", "tfplan")
	if err != nil || code != 0 {
		return nil, fmt.Errorf("%s", errMsg("show", err, stderr, code))
	}
	var parsed struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string       `json:"actions"`
				Before  map[string]any `json:"before"`
				After   map[string]any `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		return nil, fmt.Errorf("parse plan json: %w", err)
	}
	var out []model.ResourceChange
	for _, rc := range parsed.ResourceChanges {
		action := classifyAction(rc.Change.Actions)
		if action == "" {
			continue // no-op
		}
		out = append(out, model.ResourceChange{
			Address: rc.Address,
			Action:  action,
			Changed: changedKeys(rc.Change.Before, rc.Change.After),
		})
	}

	// attach the human-readable plan diff for each resource. When the show
	// call fails, surface it in the log rather than silently blanking every
	// resource; keep the JSON-derived changes regardless.
	text, stderr, code, err := opts.Commander.Run(ctx, m.Dir, m.Tool, "show", "-no-color", "tfplan")
	if err != nil || code != 0 {
		opts.Logger.Warn("plan-text diff unavailable", "dir", m.Dir, "stderr", strings.TrimSpace(string(stderr)))
	} else {
		details := parsePlanDetails(string(text))
		for i := range out {
			out[i].Detail = details[out[i].Address]
			if out[i].Detail == "" {
				opts.Logger.Debug("no plan-text block matched", "dir", m.Dir, "address", out[i].Address)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}

// classifyAction maps terraform plan actions to a human verb. "" means no-op.
func classifyAction(actions []string) string {
	switch {
	case len(actions) == 0, len(actions) == 1 && actions[0] == "no-op":
		return ""
	case len(actions) == 2: // ["create","delete"] or ["delete","create"]
		return "replace"
	default:
		switch actions[0] {
		case "create":
			return "create"
		case "delete":
			return "delete"
		case "update":
			return "update"
		case "read":
			return "read"
		default:
			return actions[0]
		}
	}
}

// changedKeys returns top-level attribute names whose values differ.
func changedKeys(before, after map[string]any) []string {
	seen := map[string]bool{}
	for k, bv := range before {
		if !reflect.DeepEqual(bv, after[k]) {
			seen[k] = true
		}
	}
	for k, av := range after {
		if _, ok := before[k]; !ok && av != nil {
			seen[k] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// logResult emits one line per module at a level matching its status.
func logResult(l *slog.Logger, r model.Result) {
	attrs := []any{"dir", r.Dir, "tool", r.Tool, "status", string(r.Status), "duration", r.Duration}
	switch r.Status {
	case model.StatusDrift:
		if len(r.Drifted) > 0 {
			attrs = append(attrs, "resources", len(r.Drifted))
		}
		l.Warn("drift detected", attrs...)
	case model.StatusError:
		l.Error("module failed", append(attrs, "error", r.Err)...)
	default:
		l.Info("module clean", attrs...)
	}
}

func errMsg(phase string, err error, stderr []byte, code int) string {
	s := strings.TrimSpace(stripANSI(string(stderr)))
	switch {
	case err != nil && s != "":
		return fmt.Sprintf("%s: %v: %s", phase, err, s)
	case err != nil:
		return fmt.Sprintf("%s: %v", phase, err)
	case s != "":
		return fmt.Sprintf("%s exit %d: %s", phase, code, s)
	default:
		return fmt.Sprintf("%s exit %d", phase, code)
	}
}
