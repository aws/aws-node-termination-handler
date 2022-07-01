#!/usr/bin/env bash

set -euo pipefail

usage=$(cat << EOM
usage: $(basename "$0") -h | DIR_PATH

    Download the ko binary to DIR_PATH.

    Arguments:
        -h  Print usage message then exit.

EOM
)

while getopts "h" opt; do
    case $opt in
        h ) echo "${usage}"
            exit 0
            ;;
        \? ) echo "${usage}" 1>&2
             exit 1
             ;;
    esac
done

dir_path="$1"

if [[ -z "${dir_path}" ]]; then
    echo "error: missing directory path" 1>&2
    echo 1>&2
    echo "${usage}" 1>&2
    exit 1
fi

if ! which wget >/dev/null ; then
    echo "error: wget not installed" 1>&2
    exit 1
fi

version="0.11.2"
os="$(go env GOHOSTOS)"
arch="$(go env GOHOSTARCH)"

if [[ "${arch}" == "amd64" ]]; then
    arch="x86_64"
elif [[ "${arch}" == "arm" ]]; then
    arch="arm64"
elif [[ "${arch}" == "386" ]]; then
    arch="i386"
fi

echo "Downloading github.com/google/ko@v${version} ..."

cd "${dir_path}"
wget https://github.com/google/ko/releases/download/v${version}/ko_${version}_${os}_${arch}.tar.gz -O - | \
    tar xzf - ko
