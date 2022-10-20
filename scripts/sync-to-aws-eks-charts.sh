#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
makefile_path="${repo_root_path}/Makefile"

if ! command -v gh >/dev/null; then
    echo "error: required executable 'gh' not found" >&2
    exit 1
fi

github_nth_full_repo_name="$(make -s -f "${makefile_path}" repo-full-name)"
nth_version="$(make -s -f "${makefile_path}" version)"

helm_chart_name="aws-node-termination-handler-2"
helm_chart_path="${repo_root_path}/charts/${helm_chart_name}"

github_eks_charts_full_repo_name="aws/eks-charts"
github_username="${GITHUB_USERNAME:-}"
github_token="${GITHUB_TOKEN:-}"

include_notes=0

usage=$(cat << EOM
usage: $(basename $0) -h | [-n] [-r REPOSITORY] [-e REPOSITORY]

    Open Pull Request to eks-charts repository with the latest ${helm_chart_name} Helm chart.

    Options:
        -h             Display this help message then exit.
        -n             Include application release node in the created Pull Request.
        -r REPOSITORY  Node Termination Handler GitHub repository. Defaults to "${github_nth_full_repo_name}".

    Options for testing:
        -e REPOSITORY  EKS Charts GitHub repository. Defaults to "${github_eks_charts_full_repo_name}".
        -u USERNAME    GitHub username. Defaults to the value of the environment variable, GITHUB_USERNAME.
        -t TOKEN       GitHub token. Defaults to the value of the environment variable, GITHUB_TOKEN .
        -v VERSION     NTH version. Defaults to "${nth_version}".

EOM
)

while getopts "nr:e:u:t:v:h" opt; do
    case "${opt}" in
        n ) include_notes=1
            ;;
        r ) github_nth_full_repo_name="${OPTARG}"
            ;;
        e ) github_eks_charts_full_repo_name="${OPTARG}"
            ;;
        u ) github_username="${OPTARG}"
            ;;
        t ) github_token="${OPTARG}"
            ;;
        v ) nth_version="${OPTARG}"
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

assert_not_empty github_nth_full_repo_name
assert_not_empty github_eks_charts_full_repo_name
assert_not_empty github_username
assert_not_empty github_token
assert_not_empty nth_version

github_eks_charts_repo_name="$(echo "${github_eks_charts_full_repo_name}" | cut -d '/' -f2)"

#################################################

echo -e "🥑 Configure gh cli"

gh_client_config_dir="${HOME}/.config/gh"
gh_client_config_path="${gh_client_config_dir}/config.yml"
gh_client_config_backup_path="${gh_client_config_dir}/config.yml.backup"

mkdir -p "${gh_client_config_dir}"

restore_gh_config() {
    if [[ -f "${gh_client_config_backup_path}" ]]; then
        echo -e "🥑 Restore gh cli configuration"
        mv -f "${gh_client_config_backup_path}" "${gh_client_config_path}" || :
    fi
}


if [[ -e "${gh_client_config_path}" ]]; then
    echo "Backing up existing configuration"
    mv -f "${gh_client_config_path}" "${gh_client_config_backup_path}"
    trap restore_gh_config EXIT
fi

echo "Writing custom configuration"
cat << EOF > "${gh_client_config_path}"
hosts:
    github.com:
        oauth_token: "${github_token}"
        user: "${github_username}"
EOF

#################################################

echo -e "🥑 Clone ${github_eks_charts_full_repo_name}"

eks_sync_path="${repo_root_path}/build/eks-sync"
rm -rf "${eks_sync_path}"
mkdir -p "${eks_sync_path}"

cd "${eks_sync_path}"
gh repo fork "${github_eks_charts_full_repo_name}" --clone --remote
eks_charts_repo_path="${eks_sync_path}/${github_eks_charts_repo_name}"

cd "${eks_charts_repo_path}"
git_default_branch="$(git rev-parse --abbrev-ref HEAD | tr -d '\n')"
git merge --ff-only "upstream/${git_default_branch}"
git remote set-url origin "https://${github_username}:${github_token}@github.com/${github_username}/${github_eks_charts_repo_name}.git"
git push origin "${git_default_branch}"

#################################################

echo -e "🥑 Check whether chart is in sync"

eks_charts_nth_path="${eks_charts_repo_path}/stable/${helm_chart_name}"
if diff -x ".*" -r "${helm_chart_path}/" "${eks_charts_nth_path}/" &>/dev/null ; then
    echo " ✅ Charts already in sync; no updates needed"
    exit
fi

echo -e "🚨 Charts are NOT in sync"

#################################################

echo -e "🥑 Copy updates to chart"

rm -rf "${eks_charts_nth_path}"
cp -R "${helm_chart_path}/" "${eks_charts_nth_path}/"

#################################################

echo -e "🥑 Commit updates"

git config --local user.name "ec2-bot 🤖"
git config --local user.email "ec2-bot@users.noreply.github.com"

helm_chart_version="$(cat "${helm_chart_path}/Chart.yaml" | grep "version:" | cut -d ' ' -f2 | tr -d '"')"
pr_id="$(uuidgen | cut -d '-' -f1)"
git_release_branch="${helm_chart_name}-${helm_chart_version}-${pr_id}"
git checkout -b "${git_release_branch}"

git add --all
git commit -m "${helm_chart_name}: ${helm_chart_version}"
git push -u origin "${git_release_branch}"

#################################################

echo -e "🥑 Create pull request"

format_release_notes() {
    echo "## ${helm_chart_name} ${helm_chart_version} Automated Chart Sync! 🤖🤖"

    if [[ ${include_notes} -ne 1 ]]; then
        return 0
    fi

    local authorization_header="Authorization: token ${GITHUB_TOKEN}"
    local release_id="$(curl -s -H "${authorization_header}" \
        https://api.github.com/repos/${github_nth_full_repo_name}/releases | \
        jq --arg ver "${nth_version}" '.[] | select(.tag_name==$ver) | .id')"

    echo
    echo "### 📝 Release Notes 📝"
    echo
    echo "$(curl -s -H "${authorization_header}" \
        https://api.github.com/repos/${github_nth_full_repo_name}/releases/${release_id} | \
        jq -r '.body')"
    echo
}

if ! gh pr create \
    --title "🥳 ${helm_chart_name} ${helm_chart_version} Automated Release! 🥑" \
    --body "$(format_release_notes)" \
    --repo "${github_eks_charts_full_repo_name}" \
    --base "master"; then
    echo -e "❌ Failed to create pull request"
    exit 1
fi

echo -e "✅ Pull request created"

echo -e "✅ EKS charts sync complete"
