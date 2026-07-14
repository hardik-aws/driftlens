# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 - 2026-07-14

### Added

- Terragrunt provider-cache server: `--plugin-cache-dir` now keeps full
  parallelism for terragrunt (concurrency-safe) instead of clamping to 1.
- Total elapsed time reported across console, JSON (`elapsed_seconds`), HTML, and PDF.
- Dependency-graph visualization: HTML layered SVG + `Module | Depends on |
  Required by` table with per-node drift status; PDF level listing + status table.
- `internal/dag` package parsing `terragrunt dag graph` DOT into topological levels.
- `--cleanup` flag (default `true`): delete each module's `.terraform` cache
  (and `.terragrunt-cache` for terragrunt) after evaluating it, keeping `tfplan`
  and `.terraform.lock.hcl`. Pass `--cleanup=false` to retain caches.
- `--plugin-cache-dir` flag: point every module at one shared provider cache so
  providers download once instead of per module; the directory is created if missing.
- `Dockerfile` (plus `.dockerignore`) producing a small image with `driftlens` and
  `terraform` on `PATH`, for DockerHub publishing.

### Changed

- `--format=json` now emits an object `{ "elapsed_seconds": <float>, "results": [...] }`
  instead of a bare array.
- Plan-text diff capture now logs when a resource's human-readable diff can't be
  captured (Warn on `show -no-color` failure, Debug on a per-resource miss)
  instead of silently blanking `Detail`. JSON-derived drift is never discarded.
- Migrated the PDF engine dependency from the deprecated
  `github.com/go-pdf/fpdf` to `codeberg.org/go-pdf/fpdf` (v0.12.0). No API or
  behavior change.
