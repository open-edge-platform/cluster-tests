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
	@if ! command -v oras &> /dev/null || [ "$$(oras version 2>/dev/null | grep -o 'Version: 1.1.0')" != "Version: 1.1.0" ]; then \
		echo "oras not found or incorrect version, installing..."; \
		wget -qO- https://github.com/oras-project/oras/releases/download/v1.1.0/oras_1.1.0_linux_amd64.tar.gz | tar -xzf - -C /tmp && sudo mv /tmp/oras /usr/local/bin/; \
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
test: render-capi-operator bootstrap ## Runs cluster orch cluster api smoke tests. This step bootstraps the env before running the test
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

##@ Staged Testing Workflow

.PHONY: bootstrap-infra
bootstrap-infra: deps render-capi-operator ## Bootstrap only infrastructure and providers (without cluster-agent)
	@echo "🚀 Starting Stage 1: Infrastructure Bootstrap..."
	@start_time=$$(date +%s); \
	PATH=${ENV_PATH} SKIP_COMPONENTS=cluster-agent mage test:bootstrap; \
	kubectl get pods -A -o wide; \
	kubectl get deployments -A -o wide; \
	kubectl get svc -A -o wide; \
	kubectl get bootstrapproviders -A; \
	kubectl get controlplaneproviders -A; \
	kubectl get coreproviders -A; \
	kubectl get node -o wide; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Stage 1 completed in $${duration}s (Infrastructure Bootstrap)"

.PHONY: deploy-cluster-agent
deploy-cluster-agent: deps ## Build and deploy only the cluster-agent component
	@echo "🔧 Starting Stage 2: Cluster Agent Deployment..."
	@start_time=$$(date +%s); \
	PATH=${ENV_PATH} ONLY_COMPONENTS=cluster-agent mage test:deployComponents; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Stage 2 completed in $${duration}s (Cluster Agent Deployment)"

.PHONY: validate-agents
validate-agents: ## Validate that agents are properly running before tests
	@echo "🔍 Starting Stage 2.5: Agent Validation..."
	@start_time=$$(date +%s); \
	echo "Checking cluster-agent pod readiness..."; \
	kubectl wait --for=condition=Ready pod/cluster-agent-0 --timeout=60s || (echo "❌ cluster-agent pod not ready" && exit 1); \
	echo "Checking agent services status..."; \
	kubectl exec cluster-agent-0 -- bash -c " \
		echo 'Agent services status:'; \
		systemctl list-units --all | grep -E 'agent|intel' || echo 'No agent services found'; \
		echo ''; \
		echo 'Checking agents.service status:'; \
		if systemctl is-active agents.service --quiet; then \
			echo '✅ agents.service is active'; \
		else \
			echo '❌ agents.service is not active:'; \
			systemctl status agents.service --no-pager -l || true; \
			echo 'Agent service failure detected - this will cause kubectl errors later'; \
			exit 1; \
		fi; \
		echo ''; \
		echo 'Checking for cluster-agent process:'; \
		if ps aux | grep -E 'cluster.*agent|edge.*agent' | grep -v grep > /dev/null; then \
			echo '✅ cluster-agent processes found:'; \
			ps aux | grep -E 'cluster.*agent|edge.*agent' | grep -v grep; \
		else \
			echo '❌ No cluster-agent processes found - agents not running properly'; \
			exit 1; \
		fi; \
		echo ''; \
		echo 'Checking Intel Infrastructure Provider connectivity (port 5991):'; \
		if curl -s --connect-timeout 3 localhost:5991 > /dev/null 2>&1; then \
			echo '✅ Port 5991 is responding to connections'; \
		else \
			echo '❌ Port 5991 not responding - Intel Infrastructure Provider cannot connect'; \
			echo 'This will cause cluster creation failures'; \
			exit 1; \
		fi; \
		echo ''; \
		echo 'Checking DNS resolution for cluster orchestrator:'; \
		if host cluster-orch-node.localhost > /dev/null 2>&1; then \
			echo '✅ DNS resolution working for cluster-orch-node.localhost'; \
		else \
			echo '❌ DNS resolution failed for cluster-orch-node.localhost'; \
			echo 'This may cause connection issues but not critical for this test'; \
		fi; \
	"; \
	if [ $$? -ne 0 ]; then \
		echo ""; \
		echo "❌ AGENT VALIDATION FAILED"; \
		echo "The root cause of test failures is that agents are not running properly."; \
		echo "Fix the agent services before proceeding with cluster creation tests."; \
		exit 1; \
	fi; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Stage 2.5 completed in $${duration}s (Agent Validation)"

.PHONY: run-tests-only
run-tests-only: ## Run only the tests without bootstrapping
	@echo "🧪 Starting Stage 3: Test Execution..."
	@start_time=$$(date +%s); \
	PATH=${ENV_PATH} SKIP_DELETE_CLUSTER=true mage test:ClusterOrchClusterApiSmokeTest; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Stage 3 completed in $${duration}s (Test Execution)"

.PHONY: test-staged
test-staged: ## Run test in stages for easier debugging
	@echo "🎯 Starting Staged Testing Workflow..."
	@overall_start=$$(date +%s); \
	$(MAKE) bootstrap-infra; \
	$(MAKE) deploy-cluster-agent; \
	$(MAKE) validate-agents; \
	$(MAKE) run-tests-only; \
	overall_end=$$(date +%s); \
	total_duration=$$((overall_end - overall_start)); \
	echo ""; \
	echo "🏁 Staged Testing Workflow Summary:"; \
	echo "   Total Time: $${total_duration}s"; \
	echo "   Infrastructure can be reused for subsequent runs"; \
	echo "   Use 'make cleanup-infra' when finished"

.PHONY: cleanup-infra
cleanup-infra: ## Clean up the kind cluster and test infrastructure
	@echo "🧹 Cleaning up test infrastructure..."
	@start_time=$$(date +%s); \
	PATH=${ENV_PATH} mage test:cleanup; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Cleanup completed in $${duration}s"

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


