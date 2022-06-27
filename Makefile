PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
BIN_DIR := $(PROJECT_DIR)/bin
SCRIPTS_DIR := $(PROJECT_DIR)/scripts
CONTROLLER_GEN = $(BIN_DIR)controller-gen
KO = $(BIN_DIR)/ko
SETUP_ENVTEST = $(BIN_DIR)/setup-envtest
GINKGO = $(BIN_DIR)/ginkgo
HELM_BASE_OPTS ?= --set aws.region=${AWS_REGION},serviceAccount.name=${SERVICE_ACCOUNT_NAME},serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=${SERVICE_ACCOUNT_ROLE_ARN}
GINKGO_BASE_OPTS ?= --coverpkg $(shell head -n 1 $(PROJECT_DIR)/go.mod | cut -s -d ' ' -f 2)/pkg/...
KODATA = \
	cmd/controller/kodata/HEAD \
	cmd/controller/kodata/refs \
	cmd/webhook/kodata/HEAD \
	cmd/webhook/kodata/refs
CODECOVERAGE_OUT = $(PROJECT_DIR)/coverprofile.out

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
	GOBIN="$(BIN_DIR)" go install github.com/google/ko@v0.9.3

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
	helm upgrade --install dev charts/aws-node-termination-handler-2 --namespace nthv2 --create-namespace \
		$(HELM_BASE_OPTS) \
		$(HELM_OPTS) \
		--set controller.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/controller) \
		--set webhook.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/webhook) \
		--set fullnameOverride=nthv2

.PHONY: delete
delete:  ## Delete controller from current kubernetes cluster.
	helm uninstall dev --namespace nthv2
