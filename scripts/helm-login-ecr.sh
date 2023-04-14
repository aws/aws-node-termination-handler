#!/usr/bin/env bash

set -euo pipefail

aws_region="${AWS_REGION:-}"

usage=$(cat << EOM
usage: $(basename $0) -h | [-g REGION] [-p] [-r REGISTRY]

    Login to an AWS ECR registry.

    Options:
        -h             Display this help message then exit.
        -g REGION      AWS Region. Defaults to "${aws_region}".
        -r REGISTRY    ECR Registry.

EOM
)

while getopts "g:r:h" opt; do
    case "${opt}" in
        g ) aws_region="${OPTARG}"
            ;;
        r ) ecr_registry="${OPTARG}"
            ;;
        h ) echo "${usage}"
            exit 0
            ;;
        \?) echo "${usage}" >&2
            exit 1
            ;;
    esac
done

function assert_not_empty {
    if [[ -z "${!1}" ]]; then
        echo "error: missing argument ${1}" >&2
        echo "${usage}" >&2
        exit 1
    fi
}

assert_not_empty aws_region
assert_not_empty ecr_registry

aws ecr-public get-login-password \
    --region "${aws_region}" | \
    helm registry login \
        --username AWS \
        --password-stdin \
        "${ecr_registry}"
