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

### Logging

Setting the `TF_DEMUX_LOG` environment variable to any non-empty value will cause `terraform-demux` to write out debug logs to `stderr`.

## Cache Directory

`terraform-demux` keeps a cache of Hashicorp's releases index and downloaded Terraform binaries in the directory returned by [os.UserCacheDir](https://golang.org/pkg/os/#UserCacheDir), under `terraform-demux/` (e.g. `~/Library/Caches/terraform-demux/` on MacOS).
