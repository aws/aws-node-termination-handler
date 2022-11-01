#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
makefile_path="${repo_root_path}/Makefile"

if ! command -v jq >/dev/null; then
    echo "command not found: jq" >&2
    exit 1
fi

region="${AWS_REGION:-us-east-1}"
repo_root="${KO_DOCKER_REPO:-}"
version="$(make -s -f "${makefile_path}" version)"

usage=$(cat << EOM
usage: $(basename $0) -h | [-g REGION] [-r REPOSITORY] [-v VERSION]

    Upload the README.md to the AWS ECR controller and webhook repositories.

    Options:
        -h             Display this help message then exit.
        -g REGION      AWS Region of ECR image repository. Defaults to "${region}".
        -r REPOSITORY  Image repository to push the built images. Defaults to "${repo_root}".
        -v VERSION     Node Termination Handler version. Defaults to "${version}".

EOM
)

while getopts "r:h" opt; do
    case "${opt}" in
        g ) region="${OPTARG}"
            ;;
        r ) repo_root="${OPTARG}"
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

assert_not_empty region
assert_not_empty repo_root
assert_not_empty version

#################################################

latest_release_tag="$(make -s -f "${makefile_path}" latest-release-tag)"
previous_release_tag="$(make -s -f "${makefile_path}" previous-release-tag)"

if ! (git --no-pager diff --name-only "${previous_release_tag}" "${latest_release_tag}" | grep 'README.md' >/dev/null); then
    echo -e "⚠️ README.md did not change in the last commit. Not taking any action."
    exit 0
fi

#################################################

content=$(jq -n --arg msg "$(cat README.md)" '{"usageText": $msg}' | jq '.usageText' | sed 's/\\n/\
/g')
if [[ ${#content} -gt 10240 ]]; then
    truncation_msg="...

**truncated due to char limits**...
A complete version of the README can be found [here](https://github.com/aws/aws-node-termination-handler/blob/${version}/README.md).\""
    content="${content:0:$((10240-${#truncation_msg}))}"
    content+="${truncation_msg}"
fi

for repo in "controller" "webhook"; do
    if ! aws ecr-public put-repository-catalog-data \
        --region "${region}" \
        --repository-name="${repo_root}/${repo}" \
        --catalog-data aboutText="${content}",usageText="See About section" >/dev/null 2>&1; then
        echo -e "❌ Failed to upload README.md to ${repo_root}/${repo}" >&2
        exit 1
    fi

    echo -e "✅ Uploaded README.md to 'About' section of ${repo_root}/${repo}"
done

echo -e "✅ Finished sync'ing README.md to ECR Public"
