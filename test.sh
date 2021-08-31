#!/usr/bin/env bash

set -e

GIT_ROOT="$(git rev-parse --show-toplevel)"
TMP_DIR="$(mktemp -d)"

if ! [[ $TMP_DIR ]]; then
    panic "could not create temporary directory"
else
    # shellcheck disable=SC2064
    trap "rm -r $TMP_DIR" EXIT

    export XDG_CACHE_HOME="$TMP_DIR"
fi

function terraform_demux_version() {
    go run "$GIT_ROOT/cmd/..." -- -version >test-log 2>&1
}

function expect_log() {
    (grep -q "$1" test-log && pass "Found '$1' in test-log") || fail "Expected to find '$1' in test-log"
}

function pass() {
    echo "PASS:" "$@" >&2
}

function fail() {
    echo "FAIL:" "$@" >&2
    exit 1
}

function panic() {
    echo "PANIC:" "$@" >&2
    exit 1
}

function terraform_demux_version_test() {
    local dir="$1"
    local expected_version="$2"

    pushd "$dir" >/dev/null

    terraform_demux_version

    (grep -q "$expected_version" test-log && pass "Found '$expected_version' in $dir/test-log") \
        || fail "Expected to find '$expected_version' in $dir/test-log"

    popd >/dev/null
}

function test_pre_0.12_exact() {
    terraform_demux_version_test "testdata/terraform-pre-0.12-exact" "Terraform v0.11.15"
}

function test_post_0.12_exact() {
    terraform_demux_version_test "testdata/terraform-post-0.12-exact" "Terraform v1.0.3"
}

function test_pessimistic_constraint() {
    terraform_demux_version_test "testdata/terraform-pessimistic" "Terraform v0.14."
}

test_pre_0.12_exact

test_post_0.12_exact

test_pessimistic_constraint
