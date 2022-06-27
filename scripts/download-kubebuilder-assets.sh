#!/usr/bin/env bash

set -euo pipefail

k8s_version="${K8S_VERSION:-1.21.x}"
kubebuilder_assets_dir="${KUBEBUILDER_ASSETS:-$HOME/.kubebuilder/bin}"

usage=$(cat << EOM
usage: $(basename "$0") -h | [-k VERSION] [-d PATH]

    Download kubebuilder assets to PATH.

    Arguments:
        -h         Print usage message then exit.
        -k VERSION Kubernetes version (default ${k8s_version}).
        -d PATH    Destination directory for downloaded assets (default ${kubebuilder_assets_dir}).

EOM
)

while getopts "k:d:h" opt; do
    case $opt in
        k ) k8s_version="$OPTARG"
            ;;
        d ) kubebuilder_assets_dir="$OPTARG"
            ;;
        * ) echo "$usage" 1>&2
            exit 1
            ;;
    esac
done

# Kubebuilder does not support darwin/arm64 so use amd64 through Rosetta instead.
arch="$(go env GOARCH)"
if [[ "$(go env GOOS)/$arch" == "darwin/arm64" ]]; then
    arch="amd64"
fi

mkdir -p "$kubebuilder_assets_dir"
ln -sf "$(setup-envtest use -p path "$k8s_version" --arch="$arch" --bin-dir="$kubebuilder_assets_dir")"/* "$kubebuilder_assets_dir"
find "$kubebuilder_assets_dir"
