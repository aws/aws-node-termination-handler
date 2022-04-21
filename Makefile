VERSION = $(shell git describe --tags --always --dirty)
LATEST_RELEASE_TAG=$(shell git describe --tags --abbrev=0)
PREVIOUS_RELEASE_TAG=$(shell git describe --abbrev=0 --tags `git rev-list --tags --skip=1  --max-count=1`)
REPO_FULL_NAME=aws/aws-node-termination-handler
ECR_REGISTRY ?= public.ecr.aws/aws-ec2
ECR_REPO ?= ${ECR_REGISTRY}/aws-node-termination-handler
IMG ?= amazon/aws-node-termination-handler
IMG_TAG ?= ${VERSION}
IMG_W_TAG = ${IMG}:${IMG_TAG}
GOOS ?= linux
GOARCH ?= amd64
GOPROXY ?= "https://proxy.golang.org,direct"
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
BUILD_DIR_PATH = ${MAKEFILE_PATH}/build
SUPPORTED_PLATFORMS_LINUX ?= "linux/amd64,linux/arm64"
SUPPORTED_PLATFORMS_WINDOWS ?= "windows/amd64"
BINARY_NAME ?= "node-termination-handler"

$(shell mkdir -p ${BUILD_DIR_PATH} && touch ${BUILD_DIR_PATH}/_go.mod)

compile:
	@echo ${MAKEFILE_PATH}
	go build -a -tags nth${GOOS} -ldflags="-s -w" -o ${BUILD_DIR_PATH}/node-termination-handler ${MAKEFILE_PATH}/cmd/node-termination-handler.go

clean:
	rm -rf ${BUILD_DIR_PATH}/

fmt:
	goimports -w ./ && gofmt -s -w ./

docker-build:
	${MAKEFILE_PATH}/scripts/build-docker-images -d -p ${GOOS}/${GOARCH} -r ${IMG} -v ${VERSION}

docker-run:
	docker run ${IMG_W_TAG}

build-docker-images:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -r ${IMG} -v ${VERSION}

build-docker-images-windows:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${SUPPORTED_PLATFORMS_WINDOWS} -r ${IMG} -v ${VERSION}

push-docker-images:
	${MAKEFILE_PATH}/scripts/retag-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -v ${VERSION} -o ${IMG} -n ${ECR_REPO}
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -r ${ECR_REPO} -v ${VERSION} -m

push-docker-images-windows:
	${MAKEFILE_PATH}/scripts/retag-docker-images -p ${SUPPORTED_PLATFORMS_WINDOWS} -v ${VERSION} -o ${IMG} -n ${ECR_REPO}
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${SUPPORTED_PLATFORMS_WINDOWS} -r ${ECR_REPO} -v ${VERSION} -m

version:
	@echo ${VERSION}

latest-release-tag:
	@echo ${LATEST_RELEASE_TAG}

previous-release-tag:
	@echo ${PREVIOUS_RELEASE_TAG}

repo-full-name:
	@echo ${REPO_FULL_NAME}

image:
	@echo ${IMG_W_TAG}

binary-name:
	@echo ${BINARY_NAME}

e2e-test:
	${MAKEFILE_PATH}/test/k8s-local-cluster-test/run-test -b e2e-test -d

compatibility-test:
	${MAKEFILE_PATH}/test/k8s-compatibility-test/run-k8s-compatibility-test.sh -p -d

license-test:
	${MAKEFILE_PATH}/test/license-test/run-license-test.sh

go-linter:
	golangci-lint run

helm-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-sync-test

helm-version-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-version-sync-test

helm-lint:
	${MAKEFILE_PATH}/test/helm/helm-lint

helm-validate-eks-versions:
	${MAKEFILE_PATH}/test/helm/validate-chart-versions

build-binaries:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${SUPPORTED_PLATFORMS_LINUX} -v ${VERSION} -d

build-binaries-windows:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${SUPPORTED_PLATFORMS_WINDOWS} -v ${VERSION} -d

upload-resources-to-github:
	${MAKEFILE_PATH}/scripts/upload-resources-to-github

upload-resources-to-github-windows:
	${MAKEFILE_PATH}/scripts/upload-resources-to-github -b

generate-k8s-yaml:
	${MAKEFILE_PATH}/scripts/generate-k8s-yaml

sync-readme-to-ecr-public:
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login
	${MAKEFILE_PATH}/scripts/sync-readme-to-ecr-public

ekscharts-sync:
	${MAKEFILE_PATH}/scripts/sync-to-aws-eks-charts -b ${BINARY_NAME} -r ${REPO_FULL_NAME}

ekscharts-sync-release:
	${MAKEFILE_PATH}/scripts/sync-to-aws-eks-charts -b ${BINARY_NAME} -r ${REPO_FULL_NAME} -n

unit-test:
	go test -bench=. ${MAKEFILE_PATH}/... -v -coverprofile=coverage.txt -covermode=atomic -outputdir=${BUILD_DIR_PATH}

unit-test-linux:
	${MAKEFILE_PATH}/scripts/run-unit-tests-in-docker

shellcheck:
	${MAKEFILE_PATH}/test/shellcheck/run-shellcheck

spellcheck:
	${MAKEFILE_PATH}/test/readme-test/run-readme-spellcheck

build: compile

helm-tests: helm-version-sync-test helm-lint helm-validate-eks-versions

eks-cluster-test:
	${MAKEFILE_PATH}/test/eks-cluster-test/run-test

release: build-binaries build-docker-images push-docker-images generate-k8s-yaml upload-resources-to-github

release-windows: build-binaries-windows build-docker-images-windows push-docker-images-windows upload-resources-to-github-windows

test: spellcheck shellcheck unit-test e2e-test compatibility-test license-test go-linter helm-sync-test helm-version-sync-test helm-lint

help:
	@grep -E '^[a-zA-Z_-]+:.*$$' $(MAKEFILE_LIST) | sort

## Targets intended to be run in preparation for a new release
create-local-release-tag-major:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -m

create-local-release-tag-minor:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -i

create-local-release-tag-patch:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -p

create-release-prep-pr:
	${MAKEFILE_PATH}/scripts/prepare-for-release

create-release-prep-pr-readme:
	${MAKEFILE_PATH}/scripts/prepare-for-release -m

create-release-prep-pr-draft:
	${MAKEFILE_PATH}/scripts/prepare-for-release -d

release-prep-major: create-local-release-tag-major create-release-prep-pr

release-prep-minor: create-local-release-tag-minor create-release-prep-pr

release-prep-patch: create-local-release-tag-patch create-release-prep-pr

release-prep-custom: # Run make NEW_VERSION=v1.2.3 release-prep-custom to prep for a custom release version
ifdef NEW_VERSION
	$(shell echo "${MAKEFILE_PATH}/scripts/create-local-tag-for-release -v $(NEW_VERSION) && echo && make create-release-prep-pr")
endif
