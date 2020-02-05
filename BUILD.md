# Build
If you would like to build and run the project locally you can follow these steps:

Clone the repo:
```
git clone https://github.com/aws/aws-node-termination-handler.git
```
Build the latest version of the docker image:
```
make docker-build
```

### Multi-Target

By default a linux/amd64 image will be built. To build for a different target the build-arg `GOARCH` can be changed.

```
$ docker build --build-arg=GOARCH=amd64 -t ${USER}/aws-node-termination-handler-amd64:v1.0.0 .
$ docker build --build-arg=GOARCH=arm64 -t ${USER}/aws-node-termination-handler-arm64:v1.0.0 .
```

To push a multi-arch image, the helper tool [manifest-tool](https://github.com/estesp/manifest-tool) can be used.

```
$ cat << EOF > manifest.yaml
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
$ manifest-tool push from-spec manifest.yaml
```

### Go Module Proxy 

By default, Go 1.13+ uses the proxy.golang.org proxy for go module downloads. This can be changed to a different go module proxy or revert back to pre-go 1.13 default which was "direct". `GOPROXY=direct` will pull from the VCS provider directly instead of going through a proxy at all.  

```
## No Proxy
docker build --build-arg=GOPROXY=direct -t ${USER}/aws-node-termination-handler:v1.0.0 .

## My Corp Proxy
docker build --build-arg=GOPROXY=go-proxy.mycorp.com -t ${USER}/aws-node-termination-handler:v1.0.0 .
```

### Kubernetes Object Files

We use Kustomize to create a master Kubernetes yaml file. You can apply the base (default confg), use the provided overlays, or write your own custom overlays. 

*NOTE: Kustomize was built into kubectl starting with kubernetes 1.14. If you are using an older version of kubernetes or `kubectl`, you can download the `kustomize` binary for your platform on their github releases page: https://github.com/kubernetes-sigs/kustomize/releases*

```
## Apply base kustomize directly kubernetes
kubectl apply -k $REPO_ROOT/config/base/deploy 

## OR apply an overlay specifying a node selector to run the daemonset only on spot instances
## This will use the base and add a node selector into the daemonset K8s object definition
kubectl apply -k $REPO_ROOT/config/deploy/overlays/spot-node-selector 
```

Read more about Kustomize and Overlays: https://kustomize.io