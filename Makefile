VERSION ?= 1.0.0-SNAPSHOT
IMG ?= amazon/aws-node-termination-handler
IMG_W_TAG = ${IMG}:v${VERSION}
DOCKER_USERNAME ?= ""
DOCKER_PASSWORD ?= ""

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
