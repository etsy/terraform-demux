# terraform-demux

A seamless launcher for Terraform.

![demo of running `terraform-demux` with different `required_version` constraints](https://user-images.githubusercontent.com/1906605/117176639-15f8d880-ad9e-11eb-9e0d-65c0bd0ce8f9.gif)


## Installation

### Homebrew

**Note:** installing `terraform-demux` via Homebrew will automatically create a symlink named `terraform`.

1. `brew tap etsy/terraform-demux https://github.com/etsy/terraform-demux`
2. `brew install terraform-demux`

### Manual

1. Grab the latest binary from the [releases page](https://github.com/etsy/terraform-demux/releases)
2. Copy it to a location in your `$PATH` as `terraform` (or leave it as `terraform-demux` if you'd like)

## Usage

Simply navigate to any folder that contains Terraform configuration and run `terraform` as you usually would. `terraform-demux` will attempt to locate the appropriate [version constraint](https://www.terraform.io/docs/language/expressions/version-constraints.html) by searching in the current working directory and recursively through parent directories. If `terraform-demux` cannot determine a constraint, it will default to the latest possible version.

### Architecture Compatability

`terraform-demux` supports a native `arm64` build that can also run `amd64` versions of `terraform` by specifying the `TF_DEMUX_ARCH` environment variable. This might be necessary for `terraform` workspaces that need older `terraform` versions that do not have `arm64` builds, or use older providers that do not have `arm64` builds.

It is recommended to set up the following shell alias for handy `amd64` invocations:

```sh
alias terraform-amd64="TF_DEMUX_ARCH=amd64 terraform-demux"
```

### Enhanced State Operations Control

We highly encourage leveraging native Terraform refactoring blocks whenever feasible, provided your Terraform version supports them. In line with this, we've implemented stricter controls over state operations to enhance security and stability. It's important to note that state operations now require the `TF_DEMUX_ALLOW_STATE_COMMANDS` environment variable to be set for execution.

Usage Details

* For Terraform 1.1.0 and above: We recomment utilizing Terraform [moved](https://developer.hashicorp.com/terraform/language/modules/develop/refactoring) block instead `terraform state mv` command.

* For Terraform 1.5.0 and above: We recomment utilizing Terraform [import](https://developer.hashicorp.com/terraform/language/import) block instead `terraform import` command.

* For Terraform 1.7.0 and above:  We recomment utilizing Terraform [removed](https://developer.hashicorp.com/terraform/language/resources/syntax) block instead `terraform state rm` command.

However, if necessary, you can still utilize the Terraform CLI to manipulate states. Before proceeding, ensure to set the environment variable `TF_DEMUX_ALLOW_STATE_COMMANDS=true` to confirm your intent.

### Logging

Setting the `TF_DEMUX_LOG` environment variable to any non-empty value will cause `terraform-demux` to write out debug logs to `stderr`.

## Cache Directory

`terraform-demux` keeps a cache of Hashicorp's releases index and downloaded Terraform binaries in the directory returned by [os.UserCacheDir](https://golang.org/pkg/os/#UserCacheDir), under `terraform-demux/` (e.g. `~/Library/Caches/terraform-demux/` on MacOS).
