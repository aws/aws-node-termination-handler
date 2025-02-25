VERSION = $(shell git describe --tags --always --dirty)
LATEST_RELEASE_TAG=$(shell git describe --tags --abbrev=0)
LATEST_COMMIT_HASH=$(shell git rev-parse HEAD)
LATEST_COMMIT_CHART_VERSION=$(shell git --no-pager show ${LATEST_COMMIT_HASH}:config/helm/aws-node-termination-handler/Chart.yaml | grep 'version:' | cut -d' ' -f2 | tr -d '[:space:]')
PREVIOUS_RELEASE_TAG=$(shell git describe --abbrev=0 --tags `git rev-list --tags --skip=1  --max-count=1`)
REPO_FULL_NAME=aws/aws-node-termination-handler
ECR_REGISTRY ?= public.ecr.aws/aws-ec2
ECR_REPO ?= ${ECR_REGISTRY}/aws-node-termination-handler
ECR_REPO_CHART ?= aws-node-termination-handler
IMG ?= amazon/aws-node-termination-handler
IMG_TAG ?= ${VERSION}
IMG_W_TAG = ${IMG}:${IMG_TAG}
GOOS ?= linux
GOARCH ?= amd64
GOPROXY ?= "https://proxy.golang.org,direct"
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
BUILD_DIR_PATH = ${MAKEFILE_PATH}/build
BIN_DIR = ${MAKEFILE_PATH}/bin
SUPPORTED_PLATFORMS_LINUX ?= "linux/amd64,linux/arm64"

# Each windows version needs a separate make target because each build 
# needs to happen on a separate GitHub runner
# A windows version is specified by major-minor-build-revision. 
# The build number of the OS must match the build number of the container image
# The revision does not matter for windows 2019 and 2022.
# Reference: https://learn.microsoft.com/en-us/virtualization/windowscontainers/deploy-containers/version-compatibility
WINDOWS_2019 ?= "windows-10.0.17763.6189/amd64"
WINDOWS_2022 ?= "windows-10.0.20348.2582/amd64"

BINARY_NAME ?= "node-termination-handler"
THIRD_PARTY_LICENSES = "${MAKEFILE_PATH}/THIRD_PARTY_LICENSES.md"
GOLICENSES = $(BIN_DIR)/go-licenses
K8S_1_29_ASSET_SUFFIX = "_k8s-1-29-or-newer"
AMAZON_ECR_CREDENTIAL_HELPER_VERSION = 0.7.1

$(shell mkdir -p ${BUILD_DIR_PATH} && touch ${BUILD_DIR_PATH}/_go.mod)

$(GOLICENSES):
	GOBIN="$(BIN_DIR)" go install github.com/google/go-licenses@v1.6.0

compile:
	@echo ${MAKEFILE_PATH}
	go build -a -tags nth${GOOS} -ldflags="-s -w" -o ${BUILD_DIR_PATH}/node-termination-handler ${MAKEFILE_PATH}/cmd/node-termination-handler.go

clean:
	rm -rf ${BUILD_DIR_PATH}/

fmt:
	goimports -w ./ && gofmt -s -w ./

docker-build:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${GOOS}/${GOARCH} -r ${IMG} -v ${VERSION}

docker-run:
	docker run ${IMG_W_TAG}

build-docker-images:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -r ${IMG} -v ${VERSION}

build-docker-images-windows-2019:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${WINDOWS_2019} -r ${IMG} -v ${VERSION}

build-docker-images-windows-2022:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${WINDOWS_2022} -r ${IMG} -v ${VERSION}

ecr-public-login:
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login

push-docker-images:
	${MAKEFILE_PATH}/scripts/retag-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -v ${VERSION} -o ${IMG} -n ${ECR_REPO}
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${SUPPORTED_PLATFORMS_LINUX} -r ${ECR_REPO} -v ${VERSION} -m

amazon-ecr-credential-helper:
	bash ${MAKEFILE_PATH}/scripts/install-amazon-ecr-credential-helper $(AMAZON_ECR_CREDENTIAL_HELPER_VERSION)

push-docker-images-windows-2019:
	${MAKEFILE_PATH}/scripts/retag-docker-images -p ${WINDOWS_2019} -v ${VERSION} -o ${IMG} -n ${ECR_REPO}
	bash ${MAKEFILE_PATH}/scripts/install-amazon-ecr-credential-helper $(AMAZON_ECR_CREDENTIAL_HELPER_VERSION)
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${WINDOWS_2019} -r ${ECR_REPO} -v ${VERSION} -m

push-docker-images-windows-2022:
	${MAKEFILE_PATH}/scripts/retag-docker-images -p ${WINDOWS_2022} -v ${VERSION} -o ${IMG} -n ${ECR_REPO}
	bash ${MAKEFILE_PATH}/scripts/install-amazon-ecr-credential-helper $(AMAZON_ECR_CREDENTIAL_HELPER_VERSION)
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${WINDOWS_2022} -r ${ECR_REPO} -v ${VERSION} -m

push-helm-chart:
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/helm-login
	${MAKEFILE_PATH}/scripts/push-helm-chart -r ${ECR_REPO_CHART} -v ${LATEST_COMMIT_CHART_VERSION} -h ${ECR_REGISTRY}

version:
	@echo ${VERSION}

chart-version:
	@echo ${LATEST_COMMIT_CHART_VERSION}

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

.PHONY: third-party-licenses
third-party-licenses: $(GOLICENSES)
	@$(GOLICENSES) report \
		--include_tests \
		--template "${MAKEFILE_PATH}/templates/third-party-licenses.tmpl" \
		"${MAKEFILE_PATH}/..." > "${THIRD_PARTY_LICENSES}"

.PHONY: license-test
license-test: $(GOLICENSES)
	@$(GOLICENSES) check \
		--allowed_licenses="Apache-2.0,BSD-2-Clause,BSD-3-Clause,BSD-4-Clause,ISC,MIT" \
		--include_tests \
		"${MAKEFILE_PATH}/..." \
		&& echo "✅ Passed" || echo "❌ Failed"

go-linter:
	golangci-lint run

helm-version-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-version-sync-test

helm-lint:
	${MAKEFILE_PATH}/test/helm/helm-lint

helm-validate-chart-versions:
	${MAKEFILE_PATH}/test/helm/validate-chart-versions

build-binaries:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${SUPPORTED_PLATFORMS_LINUX} -v ${VERSION}

build-binaries-windows-2019:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${WINDOWS_2019} -v ${VERSION}

build-binaries-windows-2022:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${WINDOWS_2022} -v ${VERSION}

upload-resources-to-github:
	${MAKEFILE_PATH}/scripts/upload-resources-to-github
	${MAKEFILE_PATH}/scripts/upload-resources-to-github -k -s ${K8S_1_29_ASSET_SUFFIX}

upload-resources-to-github-windows:
	powershell -File ${MAKEFILE_PATH}/scripts/upload-resources-to-github-windows.ps1 -BinariesOnly

generate-k8s-yaml:
	${MAKEFILE_PATH}/scripts/generate-k8s-yaml
	${MAKEFILE_PATH}/scripts/generate-k8s-yaml -k "1.32.2" -s ${K8S_1_29_ASSET_SUFFIX}

sync-readme-to-ecr-public:
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/ecr-public-login
	${MAKEFILE_PATH}/scripts/sync-readme-to-ecr-public

sync-catalog-information-for-helm-chart:
	@ECR_REGISTRY=${ECR_REGISTRY} ${MAKEFILE_PATH}/scripts/helm-login
	${MAKEFILE_PATH}/scripts/sync-catalog-information-for-helm-chart

unit-test:
	go test -bench=. ${MAKEFILE_PATH}/... -v -coverprofile=coverage.txt -covermode=atomic -outputdir=${BUILD_DIR_PATH}

unit-test-linux:
	${MAKEFILE_PATH}/scripts/run-unit-tests-in-docker

shellcheck:
	${MAKEFILE_PATH}/test/shellcheck/run-shellcheck

spellcheck:
	${MAKEFILE_PATH}/test/readme-test/run-readme-spellcheck

build: compile

helm-tests: helm-version-sync-test helm-lint helm-validate-chart-versions

eks-cluster-test:
	${MAKEFILE_PATH}/test/eks-cluster-test/run-test

release: build-binaries build-docker-images push-docker-images generate-k8s-yaml upload-resources-to-github

release-windows-2019: build-binaries-windows-2019 build-docker-images-windows-2019 push-docker-images-windows-2019

release-windows-2022: build-binaries-windows-2022 build-docker-images-windows-2022 push-docker-images-windows-2022

test: spellcheck shellcheck unit-test e2e-test compatibility-test license-test go-linter helm-version-sync-test helm-lint

help:
	@grep -E '^[a-zA-Z_-]+:.*$$' $(MAKEFILE_LIST) | sort

## Targets intended to be run in preparation for a new release
create-local-release-tag-major:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -m

create-local-release-tag-minor:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -i

create-local-release-tag-patch:
	${MAKEFILE_PATH}/scripts/create-local-tag-for-release -p

create-release-pr:
	${MAKEFILE_PATH}/scripts/prepare-for-release

create-release-pr-draft:
	${MAKEFILE_PATH}/scripts/prepare-for-release -d

release-prep-major: create-local-release-tag-major create-release-pr

release-prep-minor: create-local-release-tag-minor create-release-pr

release-prep-patch: create-local-release-tag-patch create-release-pr

release-prep-custom: # Run make NEW_VERSION=v1.2.3 release-prep-custom to prep for a custom release version
ifdef NEW_VERSION
	$(shell echo "${MAKEFILE_PATH}/scripts/create-local-tag-for-release -v $(NEW_VERSION) && echo && make create-release-pr")
endif
