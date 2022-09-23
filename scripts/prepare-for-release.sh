#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
makefile_path="${repo_root_path}/Makefile"

if ! command -v gum >/dev/null; then
    echo "error: required executable 'gum' not found" >&2
    exit 1
fi

upstream_repo_full_name="$(make -s -f "${makefile_path}" repo-full-name)"

export TERM="xterm"
xterm_color_red=$(tput setaf 1)
xterm_color_magenta=$(tput setaf 5)
xterm_format_reset=$(tput sgr 0)
xterm_style_bold=$(tput bold)

echo_red() {
    echo -e "${xterm_color_red}$1${xterm_format_reset}"
}

echo_bold() {
    echo -e "${xterm_style_bold}$1${xterm_format_reset}"
}

usage=$(cat << EOM
usage: $(basename $0) -h | [-c VERSION] [-d] [-r REPOSITORY] [-n VERSION]

    Prepare repository for a new release.
    * Create tag with version number
    * Update link URLs in documentation
    * Create pull request for updated documentation

    Options:
        -h             Display this message then exit.
        -c VERSION     New Helm Chart version, e.g. "1.0.0"
        -d             Create the pull request as a draft.
        -n VERSION     New NTH version, e.g. "2.0.0".
        -r REPOSITORY  Full name of the upstream GitHub repository. Defaults to "${upstream_repo_full_name}".

EOM
)

pr_draft=0
new_helm_chart_version=""
release_version=""

while getopts "c:dn:r:h" opt; do
    case "${opt}" in
        c ) new_helm_chart_version="${OPTARG}"
            ;;
        d ) pr_draft=1
            ;;
        n ) release_version="${OPTARG}"
            ;;
        r ) upstream_repo_full_name="${OPTARG}"
            ;;
        h ) echo "${usage}"
            exit 0
            ;;
        \?) echo "${usage}" >&2
            exit 1
            ;;
    esac
done

if [[ -z "${upstream_repo_full_name}" ]]; then
    echo "error: missing argument upstream_repo_full_name" >&2
    echo "${usage}" >&2
    exit 1
fi

release_branch=""
readme_path="${repo_root_path}/README.md"
helm_chart_name="aws-node-termination-handler-2"
helm_chart_yaml_path="${repo_root_path}/charts/${helm_chart_name}/Chart.yaml"
helm_chart_values_yaml_path="${repo_root_path}/charts/${helm_chart_name}/values.yaml"
modified_files=("${readme_path}" "${helm_chart_yaml_path}" "${helm_chart_values_yaml_path}")


rollback() {
    echo -e "⚠️ Attempting to roll back"

    if [[ -z "${release_branch}" ]]; then
        return 0
    fi

    # Delete the release branch from origin if it was pushed.
    if git show-branch "remotes/origin/${release_branch}" >/dev/null 2>&1; then
        git push origin --delete "${release_branch}" || :
    fi

    git restore -- "${modified_files[@]}" || :

    # Delete the release branch locally.
    if git show-branch "${release_branch}" >/dev/null 2>&1; then
        git checkout v2 && git branch -D "${release_branch}" || :
    fi
}

trap rollback EXIT

#################################################

echo
echo -e "🥑 Verify \"origin\" remote"

git_remote_origin_url="$(git remote get-url origin 2>&1)"

if [[ "${git_remote_origin_url}" =~ "no such remote" ]]; then
    echo_red "❌ Local repository has no configured remote \"origin\". This should point to your fork."
    exit 1
elif [[ "${git_remote_origin_url}" =~ "https://github.com/${upstream_repo_full_name}.git" ]]; then
    echo_red "❌ Local repository has misconfigured remote \"origin\" (${git_remote_origin_url}). This should point to your fork."
    exit 1
fi

echo -e "✅ Remote \"origin\" verified"

#################################################

echo
echo -e "🥑 Syncing tags from upstream"

git remote add the-real-upstream "https://github.com/${upstream_repo_full_name}.git" &>/dev/null
git fetch the-real-upstream

# Delete all the local tags -- maybe we should print a warning first?
git tag -l | xargs git tag -d

git fetch the-real-upstream --tags

git remote remove the-real-upstream

echo -e "✅ Upstream tags sync'd locally"

#################################################

echo
echo -e "🥑 Creating release branch"

previous_release_version="$(make -s -f "${makefile_path}" latest-release-tag | sed 's/^v//')"

if [[ -z "${release_version}" ]]; then
    echo
    release_version="$(gum input \
        --prompt="Enter new NTH version: " \
        --placeholder="${previous_release_version}" | \
        sed 's/^v//')"
fi

release_branch="pr/v${release_version}-release"

if [[ "$(git checkout -b "${release_branch}" 2>&1)" =~ "already exists" ]]; then
    echo_red "❌ Branch \"${release_branch}\" already exists"
    exit 1
fi

echo_bold "✅ Created new release branch \"${release_branch}\""

#################################################

echo
echo -e "🥑 Updating version numbers"

helm_chart_version="$(cat "${helm_chart_yaml_path}" | grep 'version:' | xargs | cut -d' ' -f2 | tr -d '[:space:]')"

if [[ -z "${new_helm_chart_version}" ]]; then
    echo
    new_helm_chart_version="$(gum input \
        --prompt="Enter new version of Helm chart: " \
        --placeholder="${helm_chart_version}")"
fi

sed -i.bak "s/${helm_chart_version}/${new_helm_chart_version}/" "${helm_chart_yaml_path}"
rm -f "${helm_chart_yaml_path}.bak"

sed -i.bak "s/${previous_release_version}/${release_version}/" "${modified_files[@]}"
for f in "${modified_files[@]}"; do
    rm -f "${f}.bak"
done

echo_bold "✅ Updated version numbers"
git diff

#################################################

echo
echo -e "🥑 Committing updated version numbers"

ec2_bot_user="ec2-bot 🤖"
ec2_bot_email="ec2-bot@users.noreply.github.com"

commit_msg="🥑🤖 ${release_version} release prep 🤖🥑"

git add "${modified_files[@]}"
git commit --author "${ec2_bot_user} <${ec2_bot_email}>" --message "${commit_msg}"

echo_bold "✅ Committed updated version numbers on branch \"${release_branch}\""

#################################################

echo
echo -e "🥑 Pull request summary"

target_base_branch="v2"
pr_title="🥑🤖 v${release_version} release prep"
pr_body="🥑🤖 Auto-generated PR for v${release_version} release. Updating release versions in repo."
pr_labels=("release-prep" "🤖 auto-generated🤖")
bool_to_yes_no=("no" "yes")

echo
cat << EOM
Draft        : ${bool_to_yes_no[${pr_draft}]}
Source branch: ${release_branch}
Target branch: ${upstream_repo_full_name}/${target_base_branch}
Title        : "${pr_title}"
Body         : "${pr_body}"
Labels       : $(printf '"%s" ' "${pr_labels[@]}")
Changes      :
${xterm_color_magenta}$(git diff HEAD^ HEAD)${xterm_format_reset}
EOM

echo
if ! gum confirm --default="No" "Create PR? "; then
    exit 0
fi

#################################################

echo
echo -e "🥑 Creating pull request"

git push -u origin "${release_branch}"
git checkout "${release_branch}"

gh_args=(
    --repo "${upstream_repo_full_name}"
    --base "${target_base_branch}"
    --title "${pr_title}"
    --body "${pr_body}"
)

for label in "${pr_labels[@]}"; do
    gh_args+=(--label "${label}")
done

if [[ $pr_draft -eq 1 ]]; then
    gh_args+=(--draft)
fi

if ! gh pr create "${gh_args[@]}"; then
    echo_red "❌ Failed to create pull request"
    exit 1
fi
# Now that the PR has been created there is no need to rollback
# if any subsequent commands fail.
trap - EXIT

echo -e "✅ Create pull request for ${release_branch}"
