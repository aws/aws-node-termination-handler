#!/usr/bin/env bash

set -euo pipefail

usage=$(cat << EOM
usage: $(basename "$0") -h | -d PATH

    Download tools to PATH.

    Arguments:
        -h        Print usage message then exit.
        -d PATH   Destination directory for downloaded tools.

EOM
)

tools=(
    "github.com/google/ko@v0.9.3"
    "sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0"
    # setup-envtest version specifiers:
    #   https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest/versions?tab=versions
    "sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20220217150738-f62a0f579d73"
)

bin_dir=""

while getopts "d:h" opt; do
    case $opt in
        d ) bin_dir="$OPTARG"
            ;;
        * ) echo "$usage" 1>&2
            exit 1
            ;;
    esac
done

if [[ -z "$bin_dir" ]]; then
    echo "error: missing destination path"
    echo "$usage" 1>&2
    exit 1
fi

tmp_dir="$(mktemp -d)"
trap "rm -rf \"$tmp_dir\"" EXIT

cd "$tmp_dir"
go mod init tmp >/dev/null 2>&1

for tool in ${tools[@]}; do
    echo "Downloading $tool"
    GOBIN="$bin_dir" go install "$tool"
done
