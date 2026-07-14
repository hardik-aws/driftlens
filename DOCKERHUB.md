# driftlens

Recursively scan a directory tree for **Terraform** or **Terragrunt** root
modules, run `init` + `plan` across them in parallel, and report which modules
have **drifted** from their recorded state — as a console table, JSON, HTML, or
PDF report.

Built for CI gating and local audits. This image bundles `driftlens`,
`terraform`, **and** `terragrunt` on `PATH` — no local toolchain needed.

## Quick start

Mount the directory you want to scan and run:

```bash
docker pull hardikaws/driftlens:latest

# scan ./infra (Terraform)
docker run --rm -v "$PWD:/work" -w /work hardikaws/driftlens ./infra
```

Write reports back to the host:

```bash
docker run --rm -v "$PWD:/work" -w /work \
  hardikaws/driftlens --report=both --report-dir=/work/report ./infra
```

Terragrunt trees (state access needs cloud credentials, e.g. AWS):

```bash
docker run --rm -v "$PWD:/work" -w /work \
  -v "$HOME/.aws:/root/.aws:ro" -e AWS_PROFILE \
  hardikaws/driftlens --tool=terragrunt ./live
```

Machine-readable output for a CI gate:

```bash
docker run --rm -v "$PWD:/work" -w /work \
  hardikaws/driftlens --format=json ./infra > drift.json
```

## Exit codes

Mirrors Terraform's `-detailed-exitcode`, aggregated across all modules:

| Code | Meaning |
|------|---------|
| `0`  | All modules clean |
| `2`  | At least one module drifted (no errors) |
| `1`  | At least one module errored (or bad flags / unreadable path) |

Precedence: any error → `1`, else any drift → `2`, else `0`.

## Common flags

| Flag                 | Default     | Description |
| -------------------- | ----------- | ----------- |
| `--tool`             | `terraform` | `terraform` or `terragrunt` |
| `--parallelism`      | `4`         | Concurrent workers |
| `--detailed`         | `false`     | List each drifted resource (auto-on for file reports) |
| `--plugin-cache-dir` | _(off)_     | Shared provider cache; download providers once, not per module |
| `--format`           | `console`   | stdout format: `console` or `json` |
| `--report`           | `html`      | file report: `none`, `html`, `pdf`, or `both` |
| `--report-dir`       | `report`    | directory for report files |
| `--timeout`          | `10m`       | per-directory init+plan timeout |
| `--version`          |             | print version and exit |

Run `docker run --rm hardikaws/driftlens --help` for the full flag list.

## Reports

- **Console / JSON** → stdout (`--format`). JSON is an object
  `{ "elapsed_seconds": <float>, "results": [...] }`.
- **HTML** → interactive: per-resource search + status filter, and for
  terragrunt a layered dependency-graph SVG colored by drift status.
- **PDF** → landscape A4, pure-Go engine (no external binary), static.

Every run reports total wall-clock time (`Completed in …`).

## Reproduce the image

Built by GoReleaser during the release workflow. To build locally from source:

```bash
git clone https://github.com/hardik-aws/driftlens
cd driftlens
docker build -t driftlens .
docker run --rm -v "$PWD:/work" -w /work driftlens ./infra
```

## License

See the [repository](https://github.com/hardik-aws/driftlens) for license and
full documentation.
