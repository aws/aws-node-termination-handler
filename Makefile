PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
BIN_DIR := $(PROJECT_DIR)/bin
SCRIPTS_DIR := $(PROJECT_DIR)/scripts
CONTROLLER_GEN = $(BIN_DIR)controller-gen
KO = $(BIN_DIR)/ko
SETUP_ENVTEST = $(BIN_DIR)/setup-envtest
GINKGO = $(BIN_DIR)/ginkgo
GUM = $(BIN_DIR)/gum
GH = $(BIN_DIR)/gh
GOLICENSES = $(BIN_DIR)/go-licenses
HELM_BASE_OPTS ?= --set aws.region=${AWS_REGION},serviceAccount.name=${SERVICE_ACCOUNT_NAME},serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=${SERVICE_ACCOUNT_ROLE_ARN}
GINKGO_BASE_OPTS ?= --coverpkg $(shell head -n 1 $(PROJECT_DIR)/go.mod | cut -s -d ' ' -f 2)/pkg/...
KODATA = \
	cmd/controller/kodata/HEAD \
	cmd/controller/kodata/refs \
	cmd/webhook/kodata/HEAD \
	cmd/webhook/kodata/refs
CODECOVERAGE_OUT = $(PROJECT_DIR)/coverprofile.out
THIRD_PARTY_LICENSES = $(PROJECT_DIR)/THIRD_PARTY_LICENSES.md
GITHUB_REPO_FULL_NAME = aws/aws-node-termination-handler
ECR_PUBLIC_REGION = us-east-1
ECR_PUBLIC_REGISTRY ?= public.ecr.aws/aws-ec2
ECR_PUBLIC_REPOSITORY_ROOT = aws-node-termination-handler-2

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

$(CONTROLLER_GEN):
	GOBIN="$(BIN_DIR)" go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0

$(GINKGO):
	GOBIN="$(BIN_DIR)" go install github.com/onsi/ginkgo/v2/ginkgo@v2.1.3

$(KO):
	@$(PROJECT_DIR)/scripts/download-ko.sh "$(BIN_DIR)"

$(GUM):
	@$(PROJECT_DIR)/scripts/download-gum.sh "$(BIN_DIR)"

$(GH):
	@$(PROJECT_DIR)/scripts/download-gh.sh "$(BIN_DIR)"

$(GOLICENSES):
	GOBIN="$(BIN_DIR)" go install github.com/google/go-licenses@v1.4.0

$(SETUP_ENVTEST):
	GOBIN="$(BIN_DIR)" go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20220217150738-f62a0f579d73
	PATH="$(BIN_DIR):$(PATH)" $(SCRIPTS_DIR)/download-kubebuilder-assets.sh

.PHONY: tools
tools: $(CONTROLLER_GEN) $(GINKGO) $(KO) $(SETUP_ENVTEST) ## Pre-install additional tools.

.PHONY: generate
generate: $(CONTROLLER_GEN)  ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: verify
verify: ## Run go fmt and go vet against code.
	go fmt $(PROJECT_DIR)/...
	go vet $(PROJECT_DIR)/...

$(CODECOVERAGE_OUT): $(GINKGO)
	go vet $(PROJECT_DIR)/...
	$(GINKGO) run $(GINKGO_BASE_OPTS) $(GINKGO_OPTS) $(PROJECT_DIR)/test/

.PHONY: delete-test-coverage
delete-test-coverage:
	rm -f $(CODECOVERAGE_OUT)

.PHONY: test
test: delete-test-coverage $(CODECOVERAGE_OUT)  ## Run tests.

.PHONY: explore-test-coverage
explore-test-coverage: $(CODECOVERAGE_OUT) ## Display test coverage report in default web browser.
	go tool cover -html=$<

##@ Build

.PHONY: run
run: ## Run a controller from your host.
	go run ./main.go

##@ Deployment

$(KODATA):
	mkdir -p $(@D)
	cd $(@D) && ln -s `git rev-parse --git-path $(@F)` $(@F)

.PHONY: apply
apply: $(KO) $(KODATA) ## Deploy the controller into the current kubernetes cluster.
	helm upgrade --install dev charts/aws-node-termination-handler-2 \
		--namespace ${CLUSTER_NAMESPACE} \
		--create-namespace \
		$(HELM_BASE_OPTS) \
		$(HELM_OPTS) \
		--set controller.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/controller) \
		--set webhook.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/webhook) \
		--set fullnameOverride=nthv2

.PHONY: delete
delete:  ## Delete controller from current kubernetes cluster.
	helm uninstall dev --namespace ${CLUSTER_NAMESPACE}

.PHONY: ecr-login
ecr-login: ## Login to default AWS ECR repository.
	@$(PROJECT_DIR)/scripts/docker-login-ecr.sh

##@ Release

.PHONY: build-and-push-images
build-and-push-images: $(KO) $(KODATA) ecr-public-login ## Build controller and webhook images and push to ECR public repository.
	@PATH="$(BIN_DIR):$(PATH)" $(PROJECT_DIR)/scripts/build-and-push-images.sh -r "$(ECR_PUBLIC_REGISTRY)/$(ECR_PUBLIC_REPOSITORY_ROOT)"

.PHONY: create-release-prep-pr
create-release-prep-pr: $(GUM) ## Update version numbers in documents and open a PR.
	@PATH="$(BIN_DIR):$(PATH)" $(PROJECT_DIR)/scripts/prepare-for-release.sh

.PHONY: create-release-prep-pr-draft
create-release-prep-pr-draft: $(GUM) ## Update version numbers in documents and open a draft PR.
	@PATH="$(BIN_DIR):$(PATH)" $(PROJECT_DIR)/scripts/prepare-for-release.sh -d

.PHONY: ecr-public-login
ecr-public-login: ## Login to the AWS ECR public repository.
	@$(PROJECT_DIR)/scripts/docker-login-ecr.sh -g "$(ECR_PUBLIC_REGION)" -r "$(ECR_PUBLIC_REGISTRY)/$(ECR_PUBLIC_REPOSITORY_ROOT)"

.PHONY: upload-resources-to-github
upload-resources-to-github: ## Upload contents of resources/ as part of the most recent published release.
	@$(PROJECT_DIR)/scripts/upload-resources-to-github.sh

.PHONY: latest-release-tag
latest-release-tag: ## Get tag of most recent release.
	@git describe --tags --abbrev=0 `git rev-parse --abbrev-ref HEAD`

.PHONY: previous-release-tag
previous-release-tag: ## Get tag of second most recent release.
	@git describe --tags --abbrev=0 `git rev-parse --abbrev-ref HEAD`^

.PHONY: release
release: build-and-push-images upload-resources-to-github ## Build and push images to ECR Public and upload resources to GitHub.

.PHONY: repo-full-name
repo-full-name: ## Get the full name of the GitHub repository for Node Termination Handler.
	@echo "$(GITHUB_REPO_FULL_NAME)"

.PHONY: ekscharts-sync-release
ekscharts-sync-release: $(GH)
	@PATH="$(BIN_DIR):$(PATH)" $(PROJECT_DIR)/scripts/sync-to-aws-eks-charts.sh -n

.PHONY: sync-readme-to-ecr-public
sync-readme-to-ecr-public: ecr-public-login ## Upload the README.md to ECR public controller and webhook repositories.
	@$(PROJECT_DIR)/scripts/sync-readme-to-ecr-public.sh -r "$(ECR_PUBLIC_REGISTRY)/$(ECR_PUBLIC_REPOSITORY_ROOT)"

.PHONY: version
version: latest-release-tag ## Get the most recent release version.

.PHONY: third-party-licenses
third-party-licenses: $(GOLICENSES) ## Save list of third party licenses.
	@$(GOLICENSES) report \
		--template "$(PROJECT_DIR)/templates/third-party-licenses.tmpl" \
		$(PROJECT_DIR)/cmd/controller \
		$(PROJECT_DIR)/cmd/webhook \
		$(PROJECT_DIR)/test > $(THIRD_PARTY_LICENSES)
