#
# Copyright (c) 2025 Intel Corporation.
#
# SPDX-License-Identifier: Apache-2.0
#
SHELL       := bash -e -o pipefail

# Prepend asdf shims so tool versions from .tool-versions win over any
# globally-installed binaries (e.g. /root/go/bin/ginkgo).
ENV_PATH := $(HOME)/.asdf/shims:$(shell printf "%s" "$$PATH")

# Optional local proxy env file (do not commit). Format:
#   HTTP_PROXY=http://...
#   HTTPS_PROXY=http://...
#   NO_PROXY=localhost,127.0.0.1,...
PROXY_ENV_FILE ?= $(HOME)/.config/cluster-tests/proxy.env

CLUSTERCTL_VERSION = v1.10.7

CAPI_K3S_FORK_REPO_URL ?=
CAPI_K3S_VERSION ?= v0.3.0
CAPI_OPERATOR_HELM_VERSION ?= 0.23.0

# Providers versions/URLs as needed
export CAPI_CORE_VERSION="v1.10.7"

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

.PHONY: preflight
preflight: ## Verify local prerequisites for running `make test` (vEN mode by default)
	@set -euo pipefail; \
	missing=0; \
	edge_node_provider="$${EDGE_NODE_PROVIDER:-ven}"; \
	echo "Preflight checks (EDGE_NODE_PROVIDER=$${edge_node_provider})"; \
	echo ""; \
	echo "[1/5] Checking required commands"; \
	for c in make go kubectl kind docker jq ssh scp uuidgen sudo; do \
		if command -v "$$c" >/dev/null 2>&1; then \
			echo "  OK   $$c -> $$(command -v $$c)"; \
		else \
			echo "  MISS $$c"; \
			missing=1; \
		fi; \
	done; \
	echo ""; \
	echo "[2/5] Checking test entrypoint files"; \
	for f in Makefile .test-dependencies.yaml scripts/ven/bootstrap_vm_cluster_agent.sh; do \
		if [ -f "$$f" ]; then \
			echo "  OK   $$f"; \
		else \
			echo "  MISS $$f"; \
			missing=1; \
		fi; \
	done; \
	echo ""; \
	echo "[3/5] Checking Docker daemon"; \
	if docker info >/dev/null 2>&1; then \
		echo "  OK   docker daemon reachable"; \
	else \
		echo "  FAIL docker daemon is not reachable"; \
		missing=1; \
	fi; \
	echo ""; \
	echo "[4/5] Checking kubectl client/context"; \
	if kubectl version --client >/dev/null 2>&1; then \
		echo "  OK   kubectl client available"; \
	else \
		echo "  FAIL kubectl client check failed"; \
		missing=1; \
	fi; \
	if ctx="$$(kubectl config current-context 2>/dev/null)" && [ -n "$$ctx" ]; then \
		echo "  OK   current context: $$ctx"; \
	else \
		echo "  WARN no current kubectl context configured yet"; \
	fi; \
	echo "  Checking default local test ports (8080/8081)"; \
	if ss -ltn 2>/dev/null | grep -q ':8080 '; then \
		echo "  FAIL local port 8080 is already in use (possible stale kubectl port-forward)"; \
		missing=1; \
	else \
		echo "  OK   local port 8080 is free"; \
	fi; \
	if ss -ltn 2>/dev/null | grep -q ':8081 '; then \
		echo "  FAIL local port 8081 is already in use (possible stale kubectl port-forward)"; \
		missing=1; \
	else \
		echo "  OK   local port 8081 is free"; \
	fi; \
	echo ""; \
	echo "[5/5] Checking provider-specific runtime"; \
	if [ "$$edge_node_provider" = "ven" ]; then \
		for c in virsh virt-install cloud-localds qemu-img; do \
			if command -v "$$c" >/dev/null 2>&1; then \
				echo "  OK   $$c -> $$(command -v $$c)"; \
			else \
				echo "  MISS $$c"; \
				missing=1; \
			fi; \
		done; \
		if virsh list --all >/dev/null 2>&1; then \
			echo "  OK   libvirt is reachable"; \
		else \
			echo "  FAIL libvirt is not reachable (virsh list --all failed)"; \
			missing=1; \
		fi; \
	else \
		echo "  INFO skipping vEN/libvirt checks for EDGE_NODE_PROVIDER=$$edge_node_provider"; \
	fi; \
	echo ""; \
	if [ "$$missing" -ne 0 ]; then \
		echo "Preflight FAILED. Fix missing/failed checks above, then retry."; \
		exit 2; \
	fi; \
	echo "Preflight PASSED. Environment looks ready for make test."

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
	PATH=${ENV_PATH} \
		EDGE_NODE_PROVIDER=$${EDGE_NODE_PROVIDER:-ven} \
		VEN_BOOTSTRAP_CMD=$${VEN_BOOTSTRAP_CMD:-./scripts/ven/bootstrap_vm_cluster_agent.sh} \
		DISABLE_AUTH=$${DISABLE_AUTH:-true} \
		PROXY_ENV_FILE="$(PROXY_ENV_FILE)" \
		mage test:bootstrap
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

# GitHub ref (branch, tag, or commit) of cluster-manager to fetch templates from.
# Should match the version used in .test-dependencies.yaml.
CLUSTER_MANAGER_REPO_REF ?= main
CLUSTER_MANAGER_REPO_URL  ?= https://raw.githubusercontent.com/open-edge-platform/cluster-manager

.PHONY: sync-cluster-templates
sync-cluster-templates: ## Fetch baseline cluster templates from upstream cluster-manager (CLUSTER_MANAGER_REPO_REF=main)
	@echo "Syncing cluster templates from cluster-manager@$(CLUSTER_MANAGER_REPO_REF)..."
	curl -fsSL "$(CLUSTER_MANAGER_REPO_URL)/$(CLUSTER_MANAGER_REPO_REF)/default-cluster-templates/baseline-k3s.json" \
		-o configs/baseline-cluster-template-k3s.json
	@echo "Updated configs/baseline-cluster-template-k3s.json"

.PHONY: test
test: render-capi-operator bootstrap ## Runs cluster orch cluster api smoke tests. This step bootstraps the env before running the test
	PATH=${ENV_PATH} \
		EDGE_NODE_PROVIDER=$${EDGE_NODE_PROVIDER:-ven} \
		VEN_BOOTSTRAP_CMD=$${VEN_BOOTSTRAP_CMD:-./scripts/ven/bootstrap_vm_cluster_agent.sh} \
		DISABLE_AUTH=$${DISABLE_AUTH:-true} \
		SKIP_DELETE_CLUSTER=$${SKIP_DELETE_CLUSTER:-false} \
		PROXY_ENV_FILE="$(PROXY_ENV_FILE)" \
		bash -lc 'set -euo pipefail; if [ -n "${PROXY_ENV_FILE:-}" ] && [ -f "${PROXY_ENV_FILE}" ]; then set -a; source "${PROXY_ENV_FILE}"; set +a; fi; if [ -f .ven.env ]; then source .ven.env; fi; mage test:ClusterOrchClusterApiSmokeTest'

.PHONY: cluster-api-all-test
cluster-api-all-test: bootstrap ## Runs cluster orch functional tests
	PATH=${ENV_PATH} \
		EDGE_NODE_PROVIDER=$${EDGE_NODE_PROVIDER:-ven} \
		VEN_BOOTSTRAP_CMD=$${VEN_BOOTSTRAP_CMD:-./scripts/ven/bootstrap_vm_cluster_agent.sh} \
		DISABLE_AUTH=$${DISABLE_AUTH:-true} \
		SKIP_DELETE_CLUSTER=false \
		PROXY_ENV_FILE="$(PROXY_ENV_FILE)" \
		bash -lc 'set -euo pipefail; if [ -n "${PROXY_ENV_FILE:-}" ] && [ -f "${PROXY_ENV_FILE}" ]; then set -a; source "${PROXY_ENV_FILE}"; set +a; fi; if [ -f .ven.env ]; then source .ven.env; fi; mage test:ClusterOrchClusterApiAllTest'

.PHONY: template-api-smoke-test
template-api-smoke-test: ## Runs cluster orch template API smoke tests
	PATH=${ENV_PATH} DISABLE_AUTH=$${DISABLE_AUTH:-true} mage test:ClusterOrchTemplateApiSmoleTest

.PHONY: template-api-all-test
template-api-all-test: ## Runs cluster orch template API all tests
	PATH=${ENV_PATH} DISABLE_AUTH=$${DISABLE_AUTH:-true} mage test:ClusterOrchTemplateApiAllTest
  
.PHONY: robustness-test
robustness-test: bootstrap ## Runs cluster orch robustness tests
	PATH=${ENV_PATH} \
		EDGE_NODE_PROVIDER=$${EDGE_NODE_PROVIDER:-ven} \
		VEN_BOOTSTRAP_CMD=$${VEN_BOOTSTRAP_CMD:-./scripts/ven/bootstrap_vm_cluster_agent.sh} \
		DISABLE_AUTH=$${DISABLE_AUTH:-true} \
		SKIP_DELETE_CLUSTER=false \
		PROXY_ENV_FILE="$(PROXY_ENV_FILE)" \
		bash -lc 'set -euo pipefail; if [ -n "${PROXY_ENV_FILE:-}" ] && [ -f "${PROXY_ENV_FILE}" ]; then set -a; source "${PROXY_ENV_FILE}"; set +a; fi; if [ -f .ven.env ]; then source .ven.env; fi; mage test:ClusterOrchRobustness'

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


