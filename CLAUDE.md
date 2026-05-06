# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

`terraform-demux` is a wrapper around the Terraform CLI that lets a single `terraform` invocation Just Work across projects with differing `required_version` constraints. It inspects the Terraform module in the current working directory (walking up to parents if needed), picks the newest stable release that satisfies all constraints, downloads it (cached on disk), and execs it — so users never manually switch Terraform versions when moving between projects.

## Common commands

- `go build ./cmd/terraform-demux` — build the binary.
- `go test -race ./...` — run unit tests with the race detector (matches CI).
- `go test ./internal/wrapper -run TestCheckStateCommand` — run a single test.
- `./test.sh` — integration tests against `testdata/` fixtures. Uses `TF_DEMUX_CACHE_HOME` to point the wrapper at a temp dir so the user cache isn't touched.
- `goreleaser build --snapshot` — local cross-platform build check (matches CI).
- `GOOS=windows go build ./...` — quick verify the build-tagged Windows files compile.
- `go run ./cmd/terraform-demux -- -version` — exercise the launcher against the current directory's module.

## Environment variables

- `TF_DEMUX_LOG` — non-empty enables verbose logging to stderr; otherwise logs are buffered and only flushed on error.
- `TF_DEMUX_ARCH` — overrides `runtime.GOARCH` when picking which platform binary to download (commonly `amd64` on Apple Silicon when an older Terraform release has no arm64 build).
- `TF_DEMUX_ALLOW_STATE_COMMANDS` — truthy values bypass the state-command guard. The wrapper strips all `TF_DEMUX_*` from the env it passes to the child terraform.
- `TF_DEMUX_CACHE_HOME` — override the cache directory (`os.UserCacheDir()/terraform-demux` by default; useful on macOS where `XDG_CACHE_HOME` is ignored).

## Architecture

- **Entrypoint** — `cmd/terraform-demux/main.go`: gets verbose flag from `TF_DEMUX_LOG`, calls `wrapper.SetupLogging`, picks arch from `TF_DEMUX_ARCH` or `runtime.GOARCH`, then calls `wrapper.RunTerraform`. On error, calls the flush callback to dump buffered logs to stderr.
- **`internal/wrapper`**:
  - `wrapper.go` — the orchestration: ensure cache dir → walk from cwd up looking for a module declaring `required_version` via `tfconfig.LoadModule` (split into `getTerraformVersionConstraints` + per-dir `loadConstraintsAt` helper) → fetch the release index → pick the newest non-prerelease version satisfying all constraints → guard state commands → download → exec. If no constraint is found anywhere up the tree, the latest stable release is used. Pre-release versions are filtered out. Parse errors in unrelated parent directories are logged and skipped; only a parse error in the user's own cwd is fatal.
  - `logging.go` — `SetupLogging(verbose, errSink)` returns a `flush` func; non-verbose mode buffers log output until `flush()` is called on the error path.
  - `checkargs.go` — refuses `terraform import` (≥1.5), `state mv` (≥1.1), and `state rm` (≥1.7) unless `TF_DEMUX_ALLOW_STATE_COMMANDS` is truthy. Matches positional subcommands only, so flag values like `-var=action=import` don't trigger the guard.
  - `signals_unix.go` / `signals_windows.go` — list of forwarded signals. On Unix: SIGINT, SIGTERM, SIGHUP, SIGQUIT. On Windows: SIGINT only (the rest aren't delivered to console processes).
  - `exitcode_unix.go` / `exitcode_windows.go` — Unix returns `128+signum` for signaled children (matches shell convention); Windows just returns the child's exit code.
- **`internal/releaseapi/client.go`** — HTTP client backed by `httpcache` + `diskcache` against `releases.hashicorp.com/terraform/index.json`, plus a 5-minute local TTL on the parsed index (avoids HTTP revalidation for back-to-back invocations). Verifies SHA256 against the published `*_SHA256SUMS` file, unzips, atomically writes the binary into the cache dir, chmods it `0700`. Per-binary `flock` (`gofrs/flock`) prevents concurrent processes from re-downloading the same archive. The download path is split into named helpers (`findBuild`, `expectedChecksumFor`, `verifyChecksum`, `openZipReader`, `extractTerraformBinary`, `writeZipEntryAsExecutable`) so each step is independently testable; tests in `client_test.go` use `httptest` via the shared `serveRelease` / `fakeReleaseServer` / `newTestClient` fixtures.
- **Cache dir** — `os.UserCacheDir()/terraform-demux/` (e.g. `~/Library/Caches/terraform-demux/` on macOS). Has two subdirs: `http/` for httpcache entries, `bin/` for the extracted `terraform_<ver>_<os>_<arch>` binaries. The local index cache lives at `index.json` in the parent.

## CI

- `.github/workflows/ci.yaml` runs two jobs:
  - `unit-tests` — matrix across `ubuntu-latest`, `macos-latest`, `windows-latest`. Runs `go test -race ./...`.
  - `integration` — Linux only. Runs `./test.sh` and a `goreleaser build --snapshot` smoke build.
- Go version is pinned to `1.24` in both workflows and `go.mod`. The `1.24.0` patch in `go.mod` is forced by `gofrs/flock` (which declares `go 1.24.0` in its own go.mod); Go's module rules require our directive to be ≥ the dep-closure max.

## Release / distribution

- GoReleaser (`.goreleaser.yaml`, schema `version: 2`) builds linux/windows/darwin × amd64/arm64 (no windows/arm64) on tag pushes. The workflow uses `goreleaser-action@v7`.
- `ldflags: -X main.version=v{{.Version}}` injects the release tag into `cmd/terraform-demux/main.go`'s `version` var, so released binaries report the actual version (not the package default `v0.0.1+dev`).
- Tag pushes trigger `goreleaser release --clean` in `.github/workflows/release.yaml`. Goreleaser publishes the GitHub release, uploads binaries, and pushes the updated Homebrew formula to `Formula/terraform-demux.rb` over SSH using a deploy key (secret `RELEASE_DEPLOY_KEY`). The deploy key is the bypass actor for `main`'s ruleset; it's the only identity that can write to `Formula/` while branch protection is enforced.
- The recurring `chore: regenerate homebrew formula` commits on `main` come from this flow — hand-edits to that file get clobbered on the next release.

## Workflow notes

- Don't push directly to `main`; open a PR.
- Don't mention Claude / Claude Code in commit messages or PR descriptions.
- Run `terraform fmt` on any Terraform code you modify (including `testdata/` fixtures).
