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

k8s_version="${K8S_VERSION:-1.21.x}"
kubebuilder_assets_dir="${KUBEBUILDER_ASSETS:-$HOME/.kubebuilder/bin}"

tools=(
    "github.com/google/ko@v0.9.3"
    "sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0"
    # setup-envtest version specifiers:
    #   https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest/versions?tab=versions
    "sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20220217150738-f62a0f579d73"
    "github.com/onsi/ginkgo/v2/ginkgo@v2.1.3"
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

for tool in ${tools[@]}; do
    echo "Downloading $tool"
    GOBIN="$bin_dir" go install "$tool"
done

# Kubebuilder does not support darwin/arm64 so use amd64 through Rosetta instead.
arch="$(go env GOARCH)"
if [[ "$(go env GOOS)/$arch" == "darwin/arm64" ]]; then
    arch="amd64"
fi

mkdir -p "$kubebuilder_assets_dir"
ln -sf "$("$bin_dir/setup-envtest" use -p path "$k8s_version" --arch="$arch" --bin-dir="$kubebuilder_assets_dir")"/* "$kubebuilder_assets_dir"
find "$kubebuilder_assets_dir"
