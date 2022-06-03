# Setup Development Environment

## Clone the repo

```sh
git clone --branch v2 https://github.com/aws/aws-node-termination-handler.git 
cd aws-node-termination-handler
```

## Set environment variables

```sh
export AWS_REGION=<region>
export AWS_ACCOUNT_ID=<account-id>
export CLUSTER_NAME=<name>
```

## Create Image Repositories

```sh
aws ecr create-repository \
    --repository-name nthv2/controller \
    --image-scanning-configuration scanOnPush=true \
    --region "${AWS_REGION}"

aws ecr create-repository \
    --repository-name nthv2/webhook \
    --image-scanning-configuration scanOnPush=true \
    --region "${AWS_REGION}"

export KO_DOCKER_REPO="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/nthv2"

./scripts/docker-login-ecr.sh
```

## Create an EKS Cluster

```sh
envsubst src/resources/eks-cluster.yaml.tmpl | eksctl create cluster -f -
```

### Create the Controller IAM Role

```sh
aws cloudformation deploy \
    --template-file src/resources/controller-iam-role.yaml \
    --stack-name "nthv2-${CLUSTER_NAME}" \
    --capabilities CAPABILITY_NAMED_IAM \
    --parameter-overrides "ClusterName=${CLUSTER_NAME}"

eksctl create iamserviceaccount \
    --cluster "${CLUSTER_NAME}" \
    --name nthv2 \
    --namespace nthv2 \
    --role-name "${CLUSTER_NAME}-nthv2" \
    --attach-policy-arn "arn:aws:iam::${AWS_ACCOUNT_ID}:policy/Nthv2ControllerPolicy-${CLUSTER_NAME}" \
    --role-only \
    --approve

export NTHV2_IAM_ROLE_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:role/${CLUSTER_NAME}-nthv2
```

## Build and deploy controller to Kubernetes cluster

```sh
make apply
```

## Remove deployed controller from Kubernetes cluster

```sh
make delete
```