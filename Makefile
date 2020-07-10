VERSION = $(shell git describe --tags --always --dirty)
IMG ?= amazon/aws-node-termination-handler
IMG_TAG ?= ${VERSION}
IMG_W_TAG = ${IMG}:${IMG_TAG}
DOCKER_USERNAME ?= ""
DOCKER_PASSWORD ?= ""
GOOS ?= linux
GOARCH ?= amd64
GOPROXY ?= "https://proxy.golang.org,direct"
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
BUILD_DIR_PATH = ${MAKEFILE_PATH}/build
SUPPORTED_PLATFORMS ?= "linux/amd64,linux/arm64,linux/arm"

$(shell mkdir -p ${BUILD_DIR_PATH} && touch ${BUILD_DIR_PATH}/_go.mod)

compile:
	@echo ${MAKEFILE_PATH}
	go build -a -tags nth${GOOS} -o ${BUILD_DIR_PATH}/node-termination-handler ${MAKEFILE_PATH}/cmd/node-termination-handler.go

clean:
	rm -rf ${BUILD_DIR_PATH}/

fmt:
	goimports -w ./

docker-build:
	${MAKEFILE_PATH}/scripts/build-docker-images -d -p ${GOOS}/${GOARCH} -r ${IMG} -v ${VERSION}

docker-run:
	docker run ${IMG_W_TAG}

docker-push:
	@echo ${DOCKER_PASSWORD} | docker login -u ${DOCKER_USERNAME} --password-stdin
	docker push ${IMG_W_TAG}

build-docker-images:
	${MAKEFILE_PATH}/scripts/build-docker-images -p ${SUPPORTED_PLATFORMS} -r ${IMG} -v ${VERSION}

push-docker-images:
	@echo ${DOCKER_PASSWORD} | docker login -u ${DOCKER_USERNAME} --password-stdin
	${MAKEFILE_PATH}/scripts/push-docker-images -p ${SUPPORTED_PLATFORMS} -r ${IMG} -v ${VERSION} -m

version:
	@echo ${VERSION}

image:
	@echo ${IMG_W_TAG}

e2e-test:
	${MAKEFILE_PATH}/test/k8s-local-cluster-test/run-test -b e2e-test -d

compatibility-test:
	${MAKEFILE_PATH}/test/k8s-compatibility-test/run-k8s-compatibility-test.sh -p -d

license-test:
	${MAKEFILE_PATH}/test/license-test/run-license-test.sh

go-report-card-test:
	${MAKEFILE_PATH}/test/go-report-card-test/run-report-card-test.sh

helm-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-sync-test

helm-version-sync-test:
	${MAKEFILE_PATH}/test/helm-sync-test/run-helm-version-sync-test

helm-lint:
	${MAKEFILE_PATH}/test/helm/helm-lint

build-binaries:
	${MAKEFILE_PATH}/scripts/build-binaries -p ${SUPPORTED_PLATFORMS} -v ${VERSION}

upload-resources-to-github:
	${MAKEFILE_PATH}/scripts/upload-resources-to-github

generate-k8s-yaml:
	${MAKEFILE_PATH}/scripts/generate-k8s-yaml

sync-readme-to-dockerhub:
	${MAKEFILE_PATH}/scripts/sync-readme-to-dockerhub

unit-test:
	go test -bench=. ${MAKEFILE_PATH}/... -v -coverprofile=coverage.txt -covermode=atomic -outputdir=${BUILD_DIR_PATH}

unit-test-linux:
	${MAKEFILE_PATH}/scripts/run-unit-tests-in-docker

shellcheck:
	${MAKEFILE_PATH}/test/shellcheck/run-shellcheck

spellcheck:
	${MAKEFILE_PATH}/test/readme-test/run-readme-spellcheck

build: compile

helm-tests: helm-sync-test helm-version-sync-test helm-lint

release: build-binaries build-docker-images push-docker-images generate-k8s-yaml upload-resources-to-github

test: spellcheck shellcheck unit-test e2e-test compatibility-test license-test go-report-card-test helm-sync-test helm-version-sync-test helm-lint

help:
	@grep -E '^[a-zA-Z_-]+:.*$$' $(MAKEFILE_LIST) | sort
