PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
CONTROLLER_GEN = $(PROJECT_DIR)/bin/controller-gen
KO = $(PROJECT_DIR)/bin/ko
ENVTEST = $(PROJECT_DIR)/bin/setup-envtest


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

.PHONY: toolchain
toolchain:  ## Download additional tools.
	@./scripts/toolchain.sh -d "$(PROJECT_DIR)/bin"

##@ Development

.PHONY: manifests
manifests:  ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate:  ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: verify
verify: ## Run go fmt and go vet against code.
	go fmt ./...
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

##@ Build

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

##@ Deployment

.PHONY: apply
apply:  ## Deploy the controller into the current kubernetes cluster.
	helm upgrade --install dev charts/aws-node-termination-handler-2 --namespace nthv2 --create-namespace \
		$(HELM_OPTS) \
		--set controller.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/controller) \
		--set webhook.image=$(shell $(KO) publish -B github.com/aws/aws-node-termination-handler/cmd/webhook) \
		--set fullnameOverride=nthv2

.PHONY: delete
delete:  ## Delete controller from current kubernetes cluster.
	helm uninstall dev --namespace nthv2
