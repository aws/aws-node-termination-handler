#!/bin/bash
set -euo pipefail

REPO_ROOT_PATH="$(cd "$(dirname "$0")"; cd ..; pwd -P)"
MAKE_FILE_PATH=$REPO_ROOT_PATH/Makefile
CHART_VERSION=$(make -s -f $MAKE_FILE_PATH chart-version)
HELM_CHART_PATH=$REPO_ROOT_PATH/charts/aws-node-termination-handler-2

USAGE=$(cat << 'EOM'
  Usage: push-helm-chart
  Pushes helm charts
          Optional:
            -h          HELM CHART REGISTRY: set the helm chart registry
            -v          CHART VERSION: The chart version [DEFAULT: output of `make chart-version`]
            -r          HELM CHART REPOSITORY: Set the helm chart repository
EOM
)

# Process our input arguments
while getopts "r:v:h:" opt; do
  case ${opt} in
    r ) # Helm Chart Repository
        HELM_CHART_REPOSITORY="$OPTARG"
      ;;
    v ) # Image Version
        CHART_VERSION="$OPTARG"
      ;;
    h ) # Helm Chart Registry
        ECR_REGISTRY="$OPTARG"
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

CHART_EXISTS=$(aws ecr-public describe-images --repository-name "helm/$HELM_CHART_REPOSITORY" --region us-east-1 --query "imageDetails[?contains(imageTags, '$CHART_VERSION')].imageTags[]" --output text)

if [[ -n "$CHART_EXISTS" ]]; then
    echo "chart with version $CHART_VERSION already exists in the repository, skipping pushing of chart..."
    exit 0
fi

echo "chart with version $CHART_VERSION not found in repository, pushing new chart..."
#Package the chart
helm package $HELM_CHART_PATH --destination $REPO_ROOT_PATH/bin
#Pushing helm chart
helm push $REPO_ROOT_PATH/bin/$HELM_CHART_REPOSITORY-$CHART_VERSION.tgz oci://$ECR_REGISTRY/helm