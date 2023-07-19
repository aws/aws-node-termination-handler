# Build
If you would like to build and run the project locally you can follow these steps:

Clone the repo:
```
git clone https://github.com/aws/aws-node-termination-handler.git
```
Build the latest version of the docker image for `linux/amd64`:
```
make docker-build
```

### Multi-Target

If you instead want to build for all support Linux architectures (`linux/amd64` and `linux/arm64`), you can run this make target:
```
make build-docker-images
```

Under the hood, this passes each architecture as the `--platform` argument to `docker buildx build`, like this:
```
docker buildx create --use
docker buildx build --load --platform "linux/amd64" -t ${USER}/aws-node-termination-handler-amd64:v1.0.0 .
docker buildx build --load --platform "linux/arm64" -t ${USER}/aws-node-termination-handler-arm64:v1.0.0 .
```

To push a multi-arch image, you can use the helper tool [manifest-tool](https://github.com/estesp/manifest-tool).

```
cat << EOF > manifest.yaml
image: ${USER}/aws-node-termination-handler:v1.0.0
manifests:
  -
    image: ${USER}/aws-node-termination-handler-amd64:v1.0.0
    platform:
      architecture: amd64
      os: linux
  -
    image: ${USER}/aws-node-termination-handler-arm64:v1.0.0
    platform:
      architecture: arm64
      os: linux
EOF
manifest-tool push from-spec manifest.yaml
```

### Building for Windows

You can build the Windows docker image with the following command:
```
make build-docker-images-windows
```
Currently, our `windows/amd64` builds use the older `docker build` system, not `docker buildx build` because it does not seem to be well supported. We hope to unify them in the future.

### Go Module Proxy

By default, Go 1.13+ uses the proxy.golang.org proxy for go module downloads. You can change this to a different go module proxy or revert back to pre-go 1.13 default which was "direct". `GOPROXY=direct` will pull from the VCS provider directly instead of going through a proxy at all.

```
## No Proxy
docker buildx build --load --build-arg=GOPROXY=direct -t ${USER}/aws-node-termination-handler:v1.0.0 .

## My Corp Proxy
docker buildx build --load --build-arg=GOPROXY=go-proxy.mycorp.com -t ${USER}/aws-node-termination-handler:v1.0.0 .
```

### Kubernetes Object Files

We use Kustomize to create a master Kubernetes yaml file. You can apply the base (default confg), use the provided overlays, or write your own custom overlays.

*NOTE: Kustomize was built into kubectl starting with kubernetes 1.14. If you are using an older version of kubernetes or `kubectl`, you can download the `kustomize` binary for your platform on their github releases page: https://github.com/kubernetes-sigs/kustomize/releases*

```
## Apply base kustomize directly kubernetes
kubectl apply -k $REPO_ROOT/config/base

## OR apply an overlay specifying a node selector to run the daemonset only on spot instances
## This will use the base and add a node selector into the daemonset K8s object definition
kubectl apply -k $REPO_ROOT/config/overlays/spot-node-selector
```

Read more about Kustomize and Overlays: https://kustomize.io
