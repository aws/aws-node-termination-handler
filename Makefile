IMG ?= amazon/aws-node-termination-handler:v1.0.0

docker-build:
	docker build . -t ${IMG}

docker-run:
	docker run ${IMG}

docker-push:
	docker push ${IMG}
