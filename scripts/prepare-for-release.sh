#!/usr/bin/env bash

set -euo pipefail

repo_root_path="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
echo "DEBUG: repo_root_path=${repo_root_path}"
makefile_path="${repo_root_path}/Makefile"
echo "DEBUG: makefile_path=${makefile_path}"

if ! command -v gum >/dev/null; then
    echo "error: required executable 'gum' not found" >&2
    exit 1
fi

# TODO: rename "upstream_repo_full_name"?
github_repo_full_name="$(make -s -f "${makefile_path}" repo-full-name)"

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
usage: $(basename $0) -h | [-d] [-r REPOSITORY]

    Prepare repository for a new release.
    * Create tag with version number
    * Update link URLs in documentation
    * Create pull request for updated documentation

    Options:
        -h             Display this message then exit.
        -d             Create the pull request as a draft.
        -r REPOSITORY  Full name of the GitHub repository. Defaults to "${github_repo_full_name}".

EOM
)

pr_draft=0

while getopts "dr:h" opt; do
    case "${opt}" in
        d ) pr_draft=1
            ;;
        r ) github_repo_full_name="${OPTARG}"
            ;;
        h ) echo "${usage}"
            exit 0
            ;;
        \?) echo "${usage}" >&2
            exit 1
            ;;
    esac
done

echo "DEBUG: pr_draft=${pr_draft}"

if [[ -z "${github_repo_full_name}" ]]; then
    echo "error: missing argument github_repo_full_name" >&2
    echo "${usage}" >&2
    exit 1
fi

echo "DEBUG: github_repo_full_name=${github_repo_full_name}"

#################################################

echo
echo -e "🥑 Verify \"origin\" remote"

git_remote_origin_url="$(git remote get-url origin 2>&1)"

if [[ "${git_remote_origin_url}" =~ "no such remote" ]]; then
    echo_red "❌ Local repository has no configured remote \"origin\". This should point to your fork."
    exit 1
elif [[ "${git_remote_origin_url}" =~ "https://github.com/${github_repo_full_name}.git" ]]; then
    echo_red "❌ Local repository has misconfigured remote \"origin\" (${git_remote_origin_url}). This should point to your fork."
    exit 1
fi

echo -e "✅ Remote \"origin\" verified"

#################################################

echo
echo -e "🥑 Syncing tags from upstream"

#git remote add the-real-upstream "https://github.com/${github_repo_full_name}.git" &>/dev/null
git remote add the-real-upstream "https://github.com/${github_repo_full_name}.git"
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
echo "DEBUG: previous_release_version=${previous_release_version}"
release_version="$(gum input \
    --prompt="Enter new NTH version: " \
    --placeholder="${previous_release_version}" | \
    sed 's/^v//')"
echo "DEBUG: release_version=${release_version}" >&2
sleep 2

release_branch="pr/v${release_version}-release"
echo "DEBUG: release_branch=${release_branch}"

# TODO: inline?
git_checkout_output="$(git checkout -b "${release_branch}" 2>&1)"

if [[ "${git_checkout_output}" =~ "already exists" ]]; then
    echo_red "❌ Branch \"${release_branch}\" already exists"
    exit 1
fi
echo_bold "✅ Created new release branch \"${release_branch}\""

#################################################

echo
echo -e "🥑 Updating version numbers"

helm_chart_name="aws-node-termination-handler-2"
helm_chart_yaml_path="${repo_root_path}/charts/${helm_chart_name}/Chart.yaml"
helm_chart_version="$(cat "${helm_chart_yaml_path}" | grep 'version:' | xargs | cut -d' ' -f2 | tr -d '[:space:]')"
echo "DEBUG: helm_chart_version=${helm_chart_version}"

new_helm_chart_version="$(gum input \
    --prompt="Enter new version of Helm chart: " \
    --placeholder="${helm_chart_version}")"
echo "DEBUG: new_helm_chart_version=${new_helm_chart_version}"
sed -i.bak "s/${helm_chart_version}/${new_helm_chart_version}/" "${helm_chart_yaml_path}"
rm -f "${helm_chart_yaml_path}.bak"

readme_path="${repo_root_path}/README.md"
helm_chart_values_yaml_path="${repo_root_path}/charts/${helm_chart_name}/values.yaml"

modified_files=("${readme_path}" "${helm_chart_yaml_path}" "${helm_chart_values_yaml_path}")
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
pr_title="🥑🤖 [TEST - DO NOT MERGE] v${release_version} release prep"
pr_body="🥑🤖 [TEST - DO NOT MERGE] Auto-generated PR for v${release_version} release. Updating release versions in repo."
pr_labels=("release-prep" "🤖 auto-generated🤖")
bool_to_yes_no=("no" "yes")

echo
cat << EOM
Draft        : ${bool_to_yes_no[${pr_draft}]}
Source branch: ${release_branch}
Target branch: ${github_repo_full_name}/${target_base_branch}
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
    --repo "${github_repo_full_name}"
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

set -x
DEBUG=api gh pr create "${gh_args[@]}"
set +x
if [[ $? -ne 0 ]]; then
    echo_red "❌ Failed to create pull request"
    exit 1
fi

echo -e "✅ Create pull request for ${release_branch}"
