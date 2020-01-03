VERSION = $(shell git describe --tags --always --dirty)
IMG ?= amazon/aws-node-termination-handler
IMG_W_TAG = ${IMG}:${VERSION}
DOCKER_USERNAME ?= ""
DOCKER_PASSWORD ?= ""

build:
	go build -a -o node-termination-handler cmd/node-termination-handler.go

clean:
	rm -rf build/

fmt:
	goimports -w ./

docker-build:
	docker build . -t ${IMG_W_TAG}

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
	test/k8s-local-cluster-test/run-test -b e2e-test -d

compatibility-test:
	test/k8s-compatibility-test/run-k8s-compatibility-test.sh -p "-d"

license-test:
	test/license-test/run-license-test.sh

go-report-card-test:
	test/go-report-card-test/run-report-card-test.sh

helm-sync-test:
	@echo "testing credential leak scenario"
	@echo ${DOCKER_PASSWORD}
	test/helm-sync-test/run-helm-sync-test

test: e2e-test compatibility-test license-test go-report-card-test helm-sync-test
