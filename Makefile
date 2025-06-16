#
# Copyright (c) 2025 Intel Corporation.
#
# SPDX-License-Identifier: Apache-2.0
#
SHELL       := bash -e -o pipefail

ENV_PATH = "$(shell echo "${PATH}")":${HOME}/.asdf/shims

CLUSTERCTL_VERSION = v1.9.5

CAPI_K3S_FORK_REPO_URL ?=
CAPI_K3S_VERSION ?= v0.2.1
CAPI_OPERATOR_HELM_VERSION ?= 0.20.0

# Providers versions/URLs as needed
export CAPI_CORE_VERSION="v1.9.7"
export CAPI_RKE2_VERSION="v0.14.0"
export CAPI_KUBEADM_VERSION="v1.9.0"
export CAPI_DOCKER_VERSION="v1.9.7"

export CAPI_OPERATOR_HELM_VERSION
export CAPI_K3S_BOOTSTRAP_URL
export CAPI_K3S_CONTROLPLANE_URL
export CAPI_K3S_VERSION

# URL k3s official (default) 
CAPI_K3S_OFFICIAL_BOOTSTRAP_URL = https://github.com/k3s-io/cluster-api-k3s/releases/download/$(CAPI_K3S_VERSION)/bootstrap-components.yaml
CAPI_K3S_OFFICIAL_CONTROLPLANE_URL = https://github.com/k3s-io/cluster-api-k3s/releases/download/$(CAPI_K3S_VERSION)/control-plane-components.yaml
# URL for the forked repository if provided
# If CAPI_K3S_FORK_REPO_URL is set, it will override the official URLs
CAPI_K3S_BOOTSTRAP_URL = $(if $(CAPI_K3S_FORK_REPO_URL),$(CAPI_K3S_FORK_REPO_URL)/releases/$(CAPI_K3S_VERSION)/bootstrap-components.yaml,$(CAPI_K3S_OFFICIAL_BOOTSTRAP_URL))
CAPI_K3S_CONTROLPLANE_URL = $(if $(CAPI_K3S_FORK_REPO_URL),$(CAPI_K3S_FORK_REPO_URL)/releases/$(CAPI_K3S_VERSION)/control-plane-components.yaml,$(CAPI_K3S_OFFICIAL_CONTROLPLANE_URL))

# example of how to set the CAPI_K3S_FORK_REPO_URL
# make test CAPI_K3S_FORK_REPO_URL=https://github.com/jdanieck/cluster-api-k3s CAPI_K3S_VERSION=v0.2.2-dev-196ba04


# Set the default target
.DEFAULT_GOAL := all

.PHONY: all
all: help

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

.PHONY: all
all: help

.PHONY: deps
deps: ## Install dependencies
	@if ! command -v mage &> /dev/null; then \
		echo "Mage not found, installing..."; \
		go install github.com/magefile/mage@latest; \
	fi
	@if ! command -v clusterctl &> /dev/null; then \
		ARCH=$$(uname -m); \
		if [ "$$ARCH" = "x86_64" ]; then \
			curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/$(CLUSTERCTL_VERSION)/clusterctl-linux-amd64 -o clusterctl; \
		elif [ "$$ARCH" = "arm64" ]; then \
			curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/$(CLUSTERCTL_VERSION)/clusterctl-darwin-arm64 -o clusterctl; \
		fi; \
		chmod +x ./clusterctl; \
		sudo mv ./clusterctl /usr/local/bin/; \
	fi;
	@if ! command -v asdf &> /dev/null; then \
		echo "asdf not found, installing..."; \
		go install github.com/asdf-vm/asdf/cmd/asdf@v0.16.3; \
	fi
	mage asdfPlugins

.PHONY: lint
lint: deps ## Run linters
	PATH=${ENV_PATH} mage lint:golang
	PATH=${ENV_PATH} mage lint:markdown
	PATH=${ENV_PATH} mage lint:yaml

.PHONY: render-capi-operator
render-capi-operator:
	envsubst < configs/capi-operator.yaml > /tmp/capi-operator.yaml

.PHONY: bootstrap
bootstrap: deps ## Bootstrap the test environment before running tests
	PATH=${ENV_PATH} mage test:bootstrap
	kubectl get pods -A -o wide
	kubectl get deployments -A -o wide
	kubectl get svc -A -o wide
	kubectl get bootstrapproviders -A
	kubectl get controlplaneproviders -A
	kubectl get coreproviders -A
	kubectl get node -o wide

.PHONY: bootstrap-mac
bootstrap-mac: deps ## Bootstrap the test environment on MacOS before running tests
	sed -i '' "s/skip-local-build: true/skip-local-build: false/g" .test-dependencies.yaml
	PATH=${ENV_PATH} mage test:bootstrap
	kubectl get pods -A -o wide
	kubectl get deployments -A -o wide
	kubectl get svc -A -o wide

.PHONY: test
test: bootstrap ## Runs cluster orch cluster api smoke tests. This step bootstraps the env before running the test
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=false mage test:ClusterOrchClusterApiSmokeTest

.PHONY: cluster-api-all-test
cluster-api-all-test: bootstrap ## Runs cluster orch functional tests
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=false mage test:ClusterOrchClusterApiAllTest

.PHONY: template-api-smoke-test
template-api-smoke-test: ## Runs cluster orch template API smoke tests
	PATH=${ENV_PATH} mage test:ClusterOrchTemplateApiSmoleTest

.PHONY: template-api-all-test
template-api-all-test: ## Runs cluster orch template API all tests
	PATH=${ENV_PATH} mage test:ClusterOrchTemplateApiAllTest
  
.PHONY: robustness-test
robustness-test: bootstrap ## Runs cluster orch robustness tests
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=false mage test:ClusterOrchRobustness

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


