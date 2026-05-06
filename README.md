# terraform-demux

A drop-in `terraform` that picks the right Terraform version for every project — automatically.

[![CI](https://github.com/etsy/terraform-demux/actions/workflows/ci.yaml/badge.svg)](https://github.com/etsy/terraform-demux/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/etsy/terraform-demux)](https://goreportcard.com/report/github.com/etsy/terraform-demux)
[![Latest release](https://img.shields.io/github/v/release/etsy/terraform-demux)](https://github.com/etsy/terraform-demux/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/etsy/terraform-demux.svg)](https://pkg.go.dev/github.com/etsy/terraform-demux)
[![License: Apache-2.0](https://img.shields.io/github/license/etsy/terraform-demux)](LICENSE)

![demo of running `terraform-demux` with different `required_version` constraints](https://user-images.githubusercontent.com/1906605/117176639-15f8d880-ad9e-11eb-9e0d-65c0bd0ce8f9.gif)

## Why terraform-demux?

Switching Terraform versions between projects is annoying. `tfenv install`, `tfswitch`, asdf shims — they all need maintenance, and you still forget to run them. `terraform-demux` reads each project's `required_version` constraint, downloads the matching Terraform release, verifies its SHA-256 against HashiCorp's published `SHA256SUMS`, caches it, and execs it — so a single `terraform` command Just Works in every project.

No per-version installs. No shims. No surprises.

## Features

- **Zero-config** — reads `required_version` from your `terraform { ... }` block, walking parent directories until it finds one
- **Drop-in** — installs a `terraform` symlink, so existing scripts, CI, and IDE plugins keep working
- **Verified downloads** — every archive is checked against HashiCorp's published `SHA256SUMS` before it's cached or executed
- **Concurrency-safe cache** — per-binary `flock` keeps parallel invocations from re-downloading the same release
- **Apple Silicon friendly** — `TF_DEMUX_ARCH=amd64` lets you run older Terraform releases that have no `arm64` build
- **Safer state operations** — refuses `terraform import`, `state mv`, and `state rm` on Terraform versions that have config-block alternatives ([`import`](https://developer.hashicorp.com/terraform/language/import), [`moved`](https://developer.hashicorp.com/terraform/language/modules/develop/refactoring), [`removed`](https://developer.hashicorp.com/terraform/language/resources/syntax)); opt back in with one env var when you need to
- **Cross-platform** — Linux, macOS, and Windows on `amd64` and `arm64` (no `windows/arm64`)

## Quick start

### Homebrew (recommended)

```sh
brew tap etsy/terraform-demux https://github.com/etsy/terraform-demux
brew install terraform-demux
```

The formula installs the binary as `terraform-demux` and creates a `terraform` symlink, so you can keep typing `terraform` everywhere.

```sh
terraform -version
```

### Manual

1. Grab the binary for your platform from the [latest release](https://github.com/etsy/terraform-demux/releases/latest).
2. Drop it into a directory on your `$PATH`. Either keep it as `terraform-demux`, or rename/symlink it to `terraform` if you want it to be invoked as the default.

## How does it compare?

| | reads `required_version` directly | drop-in `terraform` (no shim) | auto-installs new versions on first use |
| --- | :-: | :-: | :-: |
| **terraform-demux** | ✅ | ✅ | ✅ |
| [tfenv](https://github.com/tfutils/tfenv) | partial (via `tfenv use min-required`) | shim wrapper | ❌ (`tfenv install <ver>` first) |
| [tfswitch](https://github.com/warrensbox/terraform-switcher) | ✅ | ❌ (manages a symlink you switch) | ✅ (interactive) |

`terraform-demux`'s niche: you never run a `demux` subcommand or remember to install a version. You just run `terraform`.

## Configuration

| Env var | Purpose |
| --- | --- |
| `TF_DEMUX_LOG` | Any non-empty value enables verbose logging to stderr. By default logs are buffered and only printed if something goes wrong. |
| `TF_DEMUX_ARCH` | Override the architecture used to pick a Terraform binary. Common case: `TF_DEMUX_ARCH=amd64` on Apple Silicon for older Terraform releases that have no native `arm64` build. |
| `TF_DEMUX_ALLOW_STATE_COMMANDS` | `1`, `true`, or `yes` bypasses the state-command guard described below. |
| `TF_DEMUX_CACHE_HOME` | Override the cache directory (otherwise `os.UserCacheDir()/terraform-demux/`). Useful for sandboxes and CI; also handy on macOS, where `XDG_CACHE_HOME` is ignored by `os.UserCacheDir`. |

Suggested shell alias for `amd64` invocations on Apple Silicon:

```sh
alias terraform-amd64="TF_DEMUX_ARCH=amd64 terraform"
```

`TF_DEMUX_*` variables are stripped from the environment passed to the child Terraform process, so wrapper-internal config can't accidentally leak into providers or nested invocations.

## State-command guard

Native Terraform refactoring blocks are safer and reviewable in code, so `terraform-demux` refuses these CLI commands by default on the Terraform versions where a block alternative exists:

- `terraform import` — refused on Terraform `>= 1.5.0` (use the [`import`](https://developer.hashicorp.com/terraform/language/import) block).
- `terraform state mv` — refused on Terraform `>= 1.1.0` (use the [`moved`](https://developer.hashicorp.com/terraform/language/modules/develop/refactoring) block).
- `terraform state rm` — refused on Terraform `>= 1.7.0` (use the [`removed`](https://developer.hashicorp.com/terraform/language/resources/syntax) block).

When you really do need to run one of them, set the override:

```sh
TF_DEMUX_ALLOW_STATE_COMMANDS=true terraform state mv ...
```

The guard matches positional Terraform subcommands only, so flag values like `-var=action=import` won't trigger it.

## Cache directory

`terraform-demux` caches HashiCorp's release index and downloaded Terraform binaries under `os.UserCacheDir()/terraform-demux/` (e.g. `~/Library/Caches/terraform-demux/` on macOS), split into `http/` and `bin/` subdirectories. Each binary is checksum-verified before it lands in `bin/`.

Set `TF_DEMUX_CACHE_HOME` to point the cache somewhere else.

## Contributing

PRs and issues welcome. Run `go test ./...` for the unit tests; `./test.sh` exercises the end-to-end flow against the fixtures in `testdata/`. CI runs on Linux, macOS, and Windows on every push.

## License

Apache-2.0 — see [LICENSE](LICENSE).
