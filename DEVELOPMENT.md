# Setup Development Environment

## 1. Clone the repo

```sh
git clone --branch v2 https://github.com/aws/aws-node-termination-handler.git nthv2
cd nthv2
```

## 2. Specify an EKS Cluster

*Tip:* Several steps in this guide, and utility scripts, use environment variables. Saving these environment variables in a file, or using a shell extension like [direnv](https://direnv.net), will make it easy to restore your development environment in a new shell instance.

```sh
export CLUSTER_NAME=<name>
export AWS_REGION=<region>
```

### 2.1. (Optional) Create the EKS Cluster

Skip this set if you already have an EKS cluster.

```sh
envsubst <resources/eks-cluster.yaml.tmpl | eksctl create cluster --kubeconfig "${PWD}/kubeconfig" -f -

export KUBECONFIG="$PWD/kubeconfig"
```

If you do not want to use `envsubst` you can copy the template file and substitute the referenced values.

## 3. Create Infrastructure

```sh
export INFRASTRUCTURE_STACK_NAME="nth-${CLUSTER_NAME}"

aws cloudformation deploy \
    --template-file resources/infrastructure.yaml \
    --stack-name "${INFRASTRUCTURE_STACK_NAME}" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides ClusterName="${CLUSTER_NAME}"

export DEV_INFRASTRUCTURE_STACK_NAME="${INFRASTRUCTURE_STACK_NAME}-dev"

aws cloudformation deploy \
    --template-file resources/dev-infrastructure.yaml \
    --stack-name "${DEV_INFRASTRUCTURE_STACK_NAME}" \
    --parameter-overrides ClusterName="${CLUSTER_NAME}"

export QUEUE_NAME=<name>
export QUEUE_STACK_NAME="${INFRASTRUCTURE_STACK_NAME}-queue-${QUEUE_NAME}"

aws cloudformation deploy \
    --template-file resources/queue-infrastructure.yaml \
    --stack-name "${QUEUE_STACK_NAME}" \
    --parameter-overrides \
        ClusterName="${CLUSTER_NAME}" \
        QueueName="${QUEUE_NAME}"
```

## 4. Connect Infrastructure to EKS Cluster

```sh
export CLUSTER_NAMESPACE=<namespace>
export SERVICE_ACCOUNT_NAME="nth-${CLUSTER_NAME}-serviceaccount"

eksctl create iamserviceaccount \
    --cluster "${CLUSTER_NAME}" \
    --namespace "${CLUSTER_NAMESPACE}" \
    --name "${SERVICE_ACCOUNT_NAME}" \
    --role-name "${SERVICE_ACCOUNT_NAME}" \
    --attach-policy-arn $(./scripts/get-cfn-stack-output.sh "${INFRASTRUCTURE_STACK_NAME}" ServiceAccountPolicyARN) \
    --role-only \
    --approve

export SERVICE_ACCOUNT_ROLE_ARN=$(eksctl get iamserviceaccount \
    --cluster "${CLUSTER_NAME}" \
    --namespace "${CLUSTER_NAMESPACE}" \
    --name "${SERVICE_ACCOUNT_NAME}" \
    --output json | \
    jq -r '.[0].status.roleARN')
```

## 5. Configure and Login to Image Repository

```sh
export KO_DOCKER_REPO=$(./scripts/get-cfn-stack-output.sh "${DEV_INFRASTRUCTURE_STACK_NAME}" RepositoryBaseURI)

./scripts/docker-login-ecr.sh
```

## 6. Build and deploy controller to EKS cluster

```sh
make apply
```

### 6.1. (Optional) Providing additional Helm values

The `apply` target sets some Helm chart values for you based on environment variables. To set additional Helm values use the `HELM_OPTS` make argument. For example:

```sh
make HELM_OPTS='--set logging.level=debug' apply
```

## 7. Define and deploy a Terminator to EKS cluster

```sh
export TERMINATOR_NAME=<name>
export QUEUE_URL=$(./scripts/get-cfn-stack-output.sh "${QUEUE_STACK_NAME}" QueueURL)

envsubst <resources/terminator.yaml.tmpl >terminator-${TERMINATOR_NAME}.yaml

kubectl apply -f terminator-${TERMINATOR_NAME}.yaml
```

If you do not want to use `envsubst` you can copy the template file and substitute the referenced values.

## 8. Remove deployed controller from EKS cluster

```sh
make delete
```

# Tear down Development Environment

```sh
make delete

eksctl delete cluster --name "${CLUSTER_NAME}"

aws cloudformation delete-stack --stack-name "${QUEUE_STACK_NAME}"

./scripts/clear-image-repo.sh "$(./scripts/get-cfn-stack-output.sh ${DEV_INFRASTRUCTURE_STACK_NAME} ControllerRepositoryName)"
./scripts/clear-image-repo.sh "$(./scripts/get-cfn-stack-output.sh ${DEV_INFRASTRUCTURE_STACK_NAME} WebhookRepositoryName)"
aws cloudformation delete-stack --stack-name "${DEV_INFRASTRUCTURE_STACK_NAME}"

aws cloudformation delete-stack --stack-name "${INFRASTRUCTURE_STACK_NAME}"
```
