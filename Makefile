#
# Copyright (c) 2025 Intel Corporation.
#
# SPDX-License-Identifier: Apache-2.0
#
SHELL       := bash -e -o pipefail

ENV_PATH = "$(shell echo "${PATH}")":${HOME}/.asdf/shims

CLUSTERCTL_VERSION = v1.9.5


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

.PHONY: bootstrap
bootstrap: deps ## Bootstrap the test environment before running tests
	PATH=${ENV_PATH} mage test:bootstrap
	kubectl get pods -A -o wide
	kubectl get deployments -A -o wide
	kubectl get svc -A -o wide

.PHONY: bootstrap-mac
bootstrap-mac: deps ## Bootstrap the test environment on MacOS before running tests
	sed -i '' "s/skip-local-build: true/skip-local-build: false/g" .test-dependencies.yaml
	PATH=${ENV_PATH} mage test:bootstrap
	kubectl get pods -A -o wide
	kubectl get deployments -A -o wide
	kubectl get svc -A -o wide

.PHONY: test
test: bootstrap ## Runs cluster orch smoke tests. This step bootstraps the env before running the test
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=false mage test:clusterOrchSmoke

.PHONY: functional-test
functional-test: bootstrap ## Runs cluster orch functional tests
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=false mage test:ClusterOrchFunctional

.PHONY: template-api-test-smoke
template-api-test-smoke: ## Runs cluster orch template API tests
	PATH=${ENV_PATH} mage test:ClusterOrchTemplateApiSmoke

.PHONY: template-api-test-nightly
template-api-test-nightly: ## Runs cluster orch template API Nightly tests
	PATH=${ENV_PATH} mage test:ClusterOrchTemplateApiNightly

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


