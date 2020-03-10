VERSION = $(shell git describe --tags --always --dirty)
IMG ?= amazon/aws-node-termination-handler
IMG_W_TAG = ${IMG}:${VERSION}
DOCKER_USERNAME ?= ""
DOCKER_PASSWORD ?= ""
GOOS ?= "linux"
GOARCH ?= "amd64"
GOPROXY ?= "https://proxy.golang.org,direct"
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
BUILD_DIR_PATH = ${MAKEFILE_PATH}/build

compile:
	@echo ${MAKEFILE_PATH}
	go build -a -o ${BUILD_DIR_PATH}/node-termination-handler ${MAKEFILE_PATH}/cmd/node-termination-handler.go

create-build-dir:
	mkdir -p ${BUILD_DIR_PATH}

clean:
	rm -rf ${BUILD_DIR_PATH}/

fmt:
	goimports -w ./

docker-build:
	docker build --build-arg GOOS=${GOOS} --build-arg GOARCH=${GOARCH} --build-arg GOPROXY=${GOPROXY} ${MAKEFILE_PATH} -t ${IMG_W_TAG}

docker-run:
	docker run ${IMG_W_TAG}

docker-push:
	@echo ${DOCKER_PASSWORD} | docker login -u ${DOCKER_USERNAME} --password-stdin
	docker push ${IMG_W_TAG}

version:
	@echo ${VERSION}

image:
	@echo ${IMG_W_TAG}

e2e-test:
	${MAKEFILE_PATH}/test/k8s-local-cluster-test/run-test -b e2e-test -d

compatibility-test:
	${MAKEFILE_PATH}/test/k8s-compatibility-test/run-k8s-compatibility-test.sh -p "-d"

license-test:
	${MAKEFILE_PATH}/test/license-test/run-license-test.sh

go-report-card-test:
	${MAKEFILE_PATH}/test/go-report-card-test/run-report-card-test.sh

helm-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-sync-test

build-binaries:
	${MAKEFILE_PATH}/scripts/build-binaries

upload-binaries-to-github:
	${MAKEFILE_PATH}/scripts/upload-binaries-to-github

generate-k8s-yaml:
	${MAKEFILE_PATH}/scripts/generate-k8s-yaml

sync-readme-to-dockerhub:
	${MAKEFILE_PATH}/scripts/sync-readme-to-dockerhub

release: create-build-dir build-binaries generate-k8s-yaml upload-binaries-to-github

docker-build-and-push: docker-build docker-push

test: e2e-test compatibility-test license-test go-report-card-test helm-sync-test

unit-test: create-build-dir
	go test ${MAKEFILE_PATH}/... -v -coverprofile=coverage.txt -covermode=atomic -outputdir=${BUILD_DIR_PATH}

build: create-build-dir compile
