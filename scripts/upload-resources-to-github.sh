#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
makefile_path="${repo_root_path}/Makefile"

if ! command -v jq >/dev/null; then
    echo "error: required executable 'jq' not found" >&2
    exit 1
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    echo "error: environment variable 'GITHUB_TOKEN' is undefined or empty" >&2
    exit 1
fi

github_authorization_header="Authorization: token ${GITHUB_TOKEN}"

github_repo_full_name="$(make -s -f "${makefile_path}" repo-full-name)"
version="$(make -s -f "${makefile_path}" version)"

usage=$(cat << EOM
usage: $(basename "$0") -h | [-r REPOSITORY] [-v VERSION]

    Upload static resources to GitHub for latest release.

    Options:
        -h             Display this help message then exit.
        -r REPOSITORY  GitHub full repository name. Defaults to "${github_repo_full_name}".
        -v VERSION     Release version number. Defaults to "${version}".

EOM
)

while getopts "r:v:h" opt; do
    case "${opt}" in
        r ) github_repo_full_name="${OPTARG}"
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

assert_not_empty github_repo_full_name
assert_not_empty version

#################################################

release_id="$(curl \
    -s \
    -H "${github_authorization_header}" \
    "https://api.github.com/repos/${github_repo_full_name}/releases" | \
        jq --arg ver "${version}" '.[] | select(.tag_name==$ver) | .id')"

if [[ -z "${release_id}" ]]; then
    echo "error: no release id found" >&2
    exit 1
fi

#################################################

uploaded_asset_ids=()

rollback() {
    echo -e "⚠️  Attempting to roll back"

    for asset_id in "${uploaded_asset_ids[@]}"; do
        curl -X DELETE \
            -H "${github_authorization_header}" \
            "https://api.github.com/repos/${github_repo_full_name}/releases/assets/${asset_id}" && \
        echo -e "✅ Deleted asset with ID ${asset_id}" || \
        echo -e "❌ Failed to delete asset with ID ${asset_id}"
    done
}

trap rollback EXIT

#################################################

upload_asset() {
    local resource="$1"
    output="$(curl \
        -H "${github_authorization_header}" \
        -H "Content-Type: $(file -b --mime-type "${resource}")" \
        --silent \
        --data-binary @${resource} \
        --write-out '%{http_code}' \
        "https://uploads.github.com/repos/${github_repo_full_name}/releases/${release_id}/assets?name=$(basename ${resource})")"

    local resp_code="$(echo $output | sed 's/.*}//')"
    local resp_content="$(echo $output | sed "s/$resp_code//")"

    if [[ $resp_code -ne 201 ]]; then
        echo -e "❌ Failed to upload ${resource}"
        echo "  Status code: ${resp_code}"
        echo "  Response   : ${resp_content}"
        return 1
    fi

    local asset_id="$(echo $resp_content | jq '.id')"
    uploaded_asset_ids+=("${asset_id}")
    echo -e "✅ Uploaded ${resource} (asset ID ${asset_id})"
}

for f in $(find "${repo_root_path}/resources" -type f -maxdepth 1); do
    upload_asset "${f}"
done

# Now that the assets have been uploaded there is no need to rollback
# if any subsequent commands fail.
trap - EXIT
