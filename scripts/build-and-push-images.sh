#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
makefile_path="${repo_root_path}/Makefile"

if ! command -v ko >/dev/null; then
    echo "error: required executable 'ko' not found" >&2
    exit 1
fi

version="$(make -s -f "${makefile_path}" version)"
platforms="linux/amd64"
image_repository="${KO_DOCKER_REPO:-}"
goproxy="direct|https://proxy.golang.org"

usage=$(cat << EOM
usage: $(basename $0) -h | [-p PLATFORMS] [-r REPOSITORY] [-v VERSION]

    Build and push Docker images for each platform.

    Optional:
        -h             Display this help message then exit.
        -g GOPROXY     See documentation in "go help environment". Defaults to "${goproxy}".
        -p PLATFORMS   Comma separated list of OS and Arch identifiers, e.g. "linux/amd64,linux/arm64". Defaults to "${platforms}".
        -r REPOSITORY  Image repository to push the built images. Defaults to "${image_repository}".
        -v VERSION     Version to include in tag of docker image. Defaults "${version}".

EOM
)

while getopts "g:p:r:v:h" opt; do
    case "${opt}" in
        g ) goproxy="${OPTARG}"
            ;;
        p ) platforms="${OPTARG}"
            ;;
        r ) image_repository="${OPTARG}"
            ;;
        v ) version="${OPTARG}"
            ;;
        h ) echo "${usage}"
            exit 0
            ;;
        \?) echo "${usage}" >&2
            exit 1
            ;;
    esac
done

assert_not_empty() {
    if [[ -z "${!1}" ]]; then
        echo "error: missing argument ${1}" >&2
        echo "${usage}" >&2
        exit 1
    fi
}

assert_not_empty goproxy
assert_not_empty platforms
assert_not_empty image_repository
assert_not_empty version

for app in "controller" "webhook"; do
    GOPROXY="${goproxy}" KO_DOCKER_REPO="${image_repository}" ko publish \
        --base-import-paths \
        --tags "${version}" \
        --platform "${platforms}" \
        "github.com/aws/aws-node-termination-handler/cmd/${app}"
done
