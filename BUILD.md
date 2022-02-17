# Setup Development Environment

Clone the repo:

```sh
git clone --branch v2 https://github.com/aws/aws-node-termination-handler.git 
```

Install build tools

```sh
make toolchain
```

Configure image repository location

```sh
export KO_DOCKER_REPO=my.image.repo/path
```

Build and deploy controller to Kubernetes cluster

```sh
make apply
```

Remove deployed controller from Kubernetes cluster

```sh
make delete
```