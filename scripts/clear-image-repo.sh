#!/usr/bin/env bash

usage=$(cat << EOM
usage: $(basename "$0") -h | REPO_NAME

    Delete all images in an ECR repository.

    Options:
        -h      Print usage message then exit.

    Arguments:
        REPO_NAME  Image repository name.

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

repo_name="$1"

if [[ -z "${repo_name}" ]]; then
    echo "error: missing repository name" 1>&2
    echo 1>&2
    echo "${usage}" 1>&2
    exit 1
fi


aws ecr batch-delete-image \
    --repository-name "${repo_name}" \
    --image-ids "$(aws ecr list-images --repository-name "${repo_name}" --query imageIds --output json)"
