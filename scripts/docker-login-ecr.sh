#!/usr/bin/env bash

set -euo pipefail

aws_region="${AWS_REGION:-}"
ecr_repository="${KO_DOCKER_REPO:-}"

usage=$(cat << EOM
usage: $(basename $0) -h | [-g REGION] [-p] [-r REPOSITORY]

    Login to an AWS ECR repository.

    Options:
        -h             Display this help message then exit.
        -g REGION      AWS Region. Defaults to "${aws_region}".
        -r REPOSITORY  ECR Registry. Defaults to "${ecr_repository}".

EOM
)

while getopts "g:r:h" opt; do
    case "${opt}" in
        g ) aws_region="${OPTARG}"
            ;;
        r ) ecr_repository="${OPTARG}"
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
assert_not_empty ecr_repository

ecr_cmd="ecr"
if echo "${ecr_repository}" | grep '^public\.ecr\.aws' >/dev/null; then
    ecr_cmd="ecr-public"
fi

aws ${ecr_cmd} get-login-password \
    --region "${aws_region}" | \
    docker login \
        --username AWS \
        --password-stdin \
        "${ecr_repository}"
