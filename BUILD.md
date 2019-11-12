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

By default a linux/amd64 image will be build. To build for a different target the build-arg `GOARCH` can be changed.

```
$ docker build --build-arg=GOARCH=amd64 -t ${USER}/aws-node-termination-handler-amd64:v1.0.0 .
$ docker build --build-arg=GOARCH=arm64 -t ${USER}/aws-node-termination-handler-arm64:v1.0.0 .
```

To push a multi-arch image the helper tool [manifest-tool](https://github.com/estesp/manifest-tool) can be used.

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