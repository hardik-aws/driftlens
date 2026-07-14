# driftlens

Recursively scan a path for Terraform or Terragrunt root modules, run
`init` + `plan` across them in parallel, and report which directories have
**drifted** from their recorded state.

Built for CI gating and local audits.

## Install

**Download a prebuilt binary** from the [Releases page](https://github.com/hardik-aws/driftlens/releases).
Each release ships archives for linux / darwin / windows × amd64 / arm64 plus a
`checksums.txt`:

```bash
# example: linux amd64, v1.0.0
curl -sSL https://github.com/hardik-aws/driftlens/releases/download/v1.0.0/driftlens_1.0.0_linux_amd64.tar.gz | tar xz
sudo mv driftlens /usr/local/bin/
driftlens --version
```

Or with `go install`:

```bash
go install github.com/hardik-aws/driftlens/cmd/driftlens@latest
```

Or build from source:

```bash
go build -o driftlens ./cmd/driftlens
```

Requires the `terraform` and/or `terragrunt` binary on `PATH`.

### Docker

Public multi-arch (amd64/arm64) images are published to Docker Hub on every
release, with `driftlens`, `terraform`, **and** `terragrunt` already on
`PATH` — no local toolchain needed. Mount the directory you want to scan and
run:

```bash
docker pull hardikaws/driftlens:latest        # or a pinned version, e.g. :1.1.0
docker run --rm -v "$PWD:/work" -w /work hardikaws/driftlens ./infra
```

To write reports back to the host, mount a report directory too:

```bash
docker run --rm -v "$PWD:/work" -w /work hardikaws/driftlens --report=both --report-dir=/work/report ./infra
```

For terragrunt trees (state access needs cloud credentials, e.g. AWS):

```bash
docker run --rm -v "$PWD:/work" -w /work \
  -v "$HOME/.aws:/root/.aws:ro" -e AWS_PROFILE \
  hardikaws/driftlens --tool=terragrunt ./live
```

Or build the image locally from source:

```bash
docker build -t driftlens .
docker run --rm -v "$PWD:/work" -w /work driftlens ./infra
```

Images are built by GoReleaser during the release workflow
([`Dockerfile.goreleaser`](Dockerfile.goreleaser), `dockers:` in
[`.goreleaser.yaml`](.goreleaser.yaml)). Publishing requires two GitHub
Actions secrets on the repo: `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`
(a Docker Hub access token with Read/Write scope).

## Usage

```
driftlens [PATH] [flags]
```

`PATH` is the root directory to scan recursively (default `.`). It can be given
as a positional arg or with `--path`; if both are set, `--path` wins.

```bash
driftlens ./infra          # positional
driftlens --path=./infra   # flag (equivalent)
```

| Flag                 | Default     | Description                                                                                                                     |
| ----------------------| -------------| ---------------------------------------------------------------------------------------------------------------------------------|
| `--path`             | _(cwd)_     | Directory to scan; overrides positional `PATH`                                                                                  |
| `--tool`             | `terraform` | `terraform` or `terragrunt` (global; applies to whole run)                                                                      |
| `--parallelism`      | `4`         | Concurrent workers                                                                                                              |
| `--detailed`         | `false`     | Parse plan JSON to list each drifted resource                                                                                   |
| `--cleanup`          | `true`      | Delete each module's `.terraform` cache (and `.terragrunt-cache`) after evaluating it; keeps `tfplan` and `.terraform.lock.hcl` |
| `--lock`             | `false`     | Hold the backend **state lock** during `plan` (`-lock`). Off by default since drift detection is read-only; set `--lock` to serialize against concurrent state writers |
| `--upgrade`          | `false`     | Pass `-upgrade` to `init`: re-select providers and refresh the `.terraform.lock.hcl` checksums. Use when a run fails with _"the cached package … does not match any of the checksums recorded in the dependency lock file"_ (stale/other-platform lock hashes). Runs on cache copies, so committed lock files aren't touched |
| `--plugin-cache-dir` | _(off)_     | Shared provider cache so providers download once, not per module (created if missing). **terragrunt**: enables terragrunt's concurrency-safe provider-cache server — full `--parallelism` kept. **terraform**: sets `TF_PLUGIN_CACHE_DIR`, which is not concurrency-safe, so it forces `--parallelism=1` |
| `--format`           | `console`   | stdout format: `console` or `json`                                                                                              |
| `--report`           | `html`      | file report: `none`, `html`, `pdf`, or `both`                                                                                   |
| `--report-dir`       | `report`    | directory for report files                                                                                                      |
| `--log-level`        | `info`      | log verbosity: `debug`, `info`, `warn`, `error`                                                                                 |
| `--timeout`          | `10m`       | Per-directory init+plan timeout                                                                                                 |
| `--version`          |             | Print version (`driftlens <ver> (<commit>) built <date>`) and exit                                                              |

### Logging

Structured logs (`log/slog`, text format) go to **stderr** — stdout stays clean
for `--format=json`. Default `info` shows progress: scan start, modules
discovered, one line per module (clean=INFO, drift=WARN, error=ERROR), and a
final summary. `--log-level=debug` adds per-module "evaluating" lines; `warn`
or `error` quiet the run down to problems only.

```bash
driftlens --log-level=debug ./infra        # verbose
driftlens --log-level=error --format=json  # quiet, only failures logged
```

### Reports

`--format` controls **stdout**; `--report` writes a styled **file report** to
`--report-dir` (default `report/`), independently. An HTML report is generated
by default; pass `--report=none` to disable, `--report=pdf`, or `--report=both`.

| Mode | Files written to `report/` |
|------|-----------------------------|
| `none` | _(none)_ |
| `html` | `drift-report.html` |
| `pdf` | `drift-report.pdf` |
| `both` | `drift-report.html` + `drift-report.pdf` |

The HTML report has a client-side **search box** that filters at the
**individual resource** level — type `aws_iam_policy` or `s3_access` to show only
matching resource rows (matched against address, action, and plan diff), hiding
non-matching rows and any module left empty; clean/error modules match on their
directory and message. Plus **status filter** buttons
(All / Drift / Error / Clean) — no server, all in-page JS. The PDF is produced
by a pure-Go engine ([go-pdf/fpdf](https://codeberg.org/go-pdf/fpdf)) — no external
binary required, but it has no search/filter. Existing files are overwritten each run.

#### Dependency-graph report (terragrunt)

For terragrunt trees, the reports include a **dependency graph** built from
`terragrunt dag graph` (modern) / `graph-dependencies` (classic). The HTML shows
a layered SVG — nodes colored by drift status, laid out in columns by topological
level, each node linking to its module section — plus a `Module | Status |
Depends on | Required by` table that the search box also filters. The PDF renders
the same information as a per-level listing followed by a status table. Terraform
runs (no dependency graph) omit the section.

#### Elapsed time

Every run reports total wall-clock time: a `Completed in 3m42s` footer on the
console, an `elapsed_seconds` field in JSON, and a "Completed in …" note in the
HTML and PDF headers.

### Disk & provider cache

By default (`--cleanup=true`) each module's `.terraform` directory (plus
`.terragrunt-cache` for terragrunt) is deleted once the module has been
evaluated, so scanning a large tree doesn't leave gigabytes of duplicated
provider binaries behind. `tfplan` and `.terraform.lock.hcl` are kept. Pass
`--cleanup=false` to leave every cache in place (e.g. for repeated local runs).

`--plugin-cache-dir` points every module at one shared provider cache via
`TF_PLUGIN_CACHE_DIR`, so a provider is downloaded once and reused across all
modules instead of re-fetched per directory. The directory is created if it
doesn't exist. Combine the two for a fast, low-disk CI run:

```bash
driftlens --plugin-cache-dir=/tmp/tf-plugins ./infra   # download once, cleanup after
```

If `TF_PLUGIN_CACHE_DIR` is already exported in your environment, driftlens
inherits it; the flag just sets it explicitly for the run.

### Exit codes

Mirrors Terraform's `-detailed-exitcode`:

| Code | Meaning |
|------|---------|
| `0` | All modules clean |
| `2` | At least one module drifted (no errors) |
| `1` | At least one module errored (or bad flags / unreadable path) |

Aggregation precedence: any error → `1`, else any drift → `2`, else `0`.

## How it works

1. **Discover** — walk `PATH`; a directory is a module when it contains
   `*.tf` (terraform) or `terragrunt.hcl` (terragrunt). Hidden directories
   and `.terraform/` / `.git/` are skipped.
2. **Run** — a bounded worker pool (`--parallelism`) evaluates modules.
   - **terraform**: per module, `init -input=false` then
     `plan -detailed-exitcode -input=false -lock=<--lock>` (state lock off by
     default) (exit `0` → clean, `2` → drift, other → error); with `--detailed`,
     re-plan to `tfplan`, `show -json tfplan` to collect every resource whose
     `change.actions` is not `["no-op"]`, plus `show -no-color tfplan` to
     capture each resource's human-readable diff block.
   - **terragrunt**: first, two ordered dependency-graph-wide commands at the
     scan root — `run-all init` for every module, then `run-all plan`
     (`terragrunt run --all -- init` / `... -- plan` on terragrunt ≥ 0.73,
     classic `terragrunt run-all init` / `run-all plan` on older versions —
     detected via one `terragrunt --version` probe). Init runs across the whole
     tree before any plan; a non-zero init aborts before plan. Pass
     `--upgrade` to make init re-select providers and refresh lock-file
     checksums when a run trips over a provider-checksum mismatch.
     terragrunt's own dependency graph inits+plans every module in order, so
     `dependency "X" { ... }` blocks resolve against already-materialized
     state instead of racing driftlens's parallel workers for a shared
     `.terragrunt-cache` entry. Then the worker pool reads each module's
     resulting plan (`show -json` + `show -no-color`, same parsing as
     terraform's `--detailed` path). If the plan file isn't in the cache
     terragrunt regenerates for a standalone `show` (common for remote-sourced
     modules), the worker re-plans that module in place and reads the fresh
     plan. Detail collection is always on.
   One module failing never aborts the run.
3. **Report** — console table or JSON, plus optional HTML/PDF files, then set
   the process exit code. HTML/PDF group output into **one section per module**:
   a header band (directory, tool, status badge, drifted-resource count) above a
   per-resource table with columns **Action, Resource, Plan detail** — Plan
   detail being the raw `terraform plan` diff for that resource. Clean modules
   show a "No drift detected" note; errored modules show their error message.
   When a file report is requested, per-resource detail is collected
   automatically (no `--detailed` needed).

## Examples

Scan current dir, human-readable table:

```bash
driftlens
```

Scan a Terragrunt repo with per-resource detail, 8 workers:

```bash
driftlens --tool=terragrunt --detailed --parallelism=8 ./live
```

CI gate — fail the build on any drift, machine-readable output:

```bash
driftlens --format=json ./infra > drift.json
# exit 2 if drift, 1 on error
```

Sample console output:

```
DIR            TOOL        STATUS  DETAIL
.              terraform   clean
svc-a          terraform   drift   update aws_s3_bucket.logs (acl, tags); replace aws_iam_role.app
svc-b/nested   terraform   error   init exit 1: backend init failed
```

With `--detailed`, the console renders each drifted resource compactly as
`<action> <address> (<changed attributes>)`. The HTML and PDF reports instead
group resources into **per-module sections**: each module gets a header band
(directory, tool, status, count) and, for drifted modules, a table with one row
per resource (Action, Resource, Plan detail) using a color-coded action badge
(create / update / replace / delete / read) and the full raw plan diff. Clean
modules render a "No drift detected" note and errored modules an error box. PDF
is landscape A4 to fit the diff. `--format=json` emits an object
`{ "elapsed_seconds": <float>, "results": [...] }`, and each result's `drifted[]`
carries the same `detail` text per resource. Requesting any file report
(`--report=html|pdf|both`) auto-enables per-resource detail collection.

## Development

```bash
go test ./...     # unit tests (fake Commander; no real terraform needed)
go vet ./...
gofmt -l .
```

### Releasing

Versioning follows [SemVer](https://semver.org), starting at `1.0.0`. Releases are
cut by pushing a `v*` tag **whose commit is on `main`** — the
[`release` workflow](.github/workflows/release.yml) verifies the tagged commit is an
ancestor of `origin/main` and fails fast otherwise, then runs tests and
[GoReleaser](https://goreleaser.com), which cross-compiles all targets, builds archives
+ `checksums.txt`, and publishes a GitHub Release. Tags on feature branches are rejected.

```bash
git checkout main
git tag v1.0.0
git push origin v1.0.0   # triggers .github/workflows/release.yml
```

Version metadata is injected at link time (`-X main.version/commit/date`); a plain
`go build` reports `dev`. Config lives in [.goreleaser.yaml](.goreleaser.yaml).

### Layout

```
cmd/driftlens/    CLI entrypoint, flag parsing, exit code
internal/discover/   recursive module discovery
internal/runner/     worker pool, init+plan, exec wrapper (injectable Commander)
internal/report/     console table + JSON + HTML/PDF rendering
internal/model/      shared types + exit-code aggregation
```
