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
	@echo "Starting Stage 2: Cluster Agent Deployment..."
	@start_time=$$(date +%s); \
	echo "Step 2.1: Deploying TLS proxy for gRPC termination..."; \
	kubectl apply -f tls-proxy-simple.yaml; \
	echo "Step 2.2: Waiting for TLS proxy to be ready..."; \
	kubectl wait --for=condition=Ready pod -l app=grpc-tls-proxy-simple --timeout=60s; \
	echo "Step 2.3: Loading cluster-agent container image..."; \
	PATH=${ENV_PATH} kind load docker-image 080137407410.dkr.ecr.us-west-2.amazonaws.com/edge-orch/infra/enic:0.8.5 --name kind; \
	echo "Step 2.4: Deploying cluster-agent..."; \
	kubectl apply -f cluster-agent-fixed.yaml; \
	echo "Step 2.5: Waiting for cluster-agent pod to be ready..."; \
	kubectl wait --for=condition=Ready pod/cluster-agent-0 --timeout=120s; \
	echo "Step 2.6: Adding TLS proxy certificate to cluster-agent trust store..."; \
	kubectl exec $$(kubectl get pods -l app=grpc-tls-proxy-simple -o jsonpath='{.items[0].metadata.name}') -- cat /etc/nginx/certs/tls.crt > /tmp/proxy-cert.crt; \
	kubectl cp /tmp/proxy-cert.crt cluster-agent-0:/usr/local/share/ca-certificates/grpc-proxy.crt; \
	kubectl exec cluster-agent-0 -- update-ca-certificates; \
	rm -f /tmp/proxy-cert.crt; \
	echo "Step 2.7: Verifying cluster-agent pod is running..."; \
	kubectl get pod cluster-agent-0; \
	end_time=$$(date +%s); \
	duration=$$((end_time - start_time)); \
	echo "✅ Stage 2 completed in $${duration}s (Cluster Agent Deployment with TLS Proxy)"

.PHONY: validate-tls-proxy
validate-tls-proxy: ## Validate that TLS proxy is working properly
	@echo "TLS Proxy Validation..."
	@echo "Checking TLS proxy pod status..."
	@kubectl wait --for=condition=Ready pod -l app=grpc-tls-proxy-simple --timeout=60s || (echo "TLS proxy pod not ready" && exit 1)
	@echo "Verifying TLS proxy service connectivity..."
	@kubectl exec cluster-agent-0 -- timeout 10 bash -c "openssl s_client -connect grpc-tls-proxy-simple:50021 -servername grpc-tls-proxy-simple < /dev/null" || (echo "TLS proxy connectivity failed" && exit 1)
	@echo "✅ TLS proxy validation completed successfully"

.PHONY: validate-agents
validate-agents: validate-tls-proxy ## Validate that agents are properly running before tests
	@echo "Agent Validation..."
	@kubectl wait --for=condition=Ready pod/cluster-agent-0 --timeout=60s || (echo "Pod not ready" && exit 1)
	@kubectl exec cluster-agent-0 -- bash -c " \
		required_agents=\"cluster-agent node-agent platform-update-agent platform-telemetry-agent\"; \
		max_retries=5; \
		retry_interval=10; \
		for attempt in \$$(seq 1 \$$max_retries); do \
			echo \"Attempt \$$attempt/\$$max_retries:\"; \
			active_count=0; \
			total_count=0; \
			for agent in \$$required_agents; do \
				total_count=\$$((total_count + 1)); \
				service_status=\$$(systemctl is-active \$$agent.service 2>/dev/null || echo 'inactive'); \
				if [ \"\$$service_status\" = \"active\" ]; then \
					echo \"   \$$agent.service: ACTIVE\"; \
					active_count=\$$((active_count + 1)); \
				else \
					echo \"   \$$agent.service: \$$service_status\"; \
				fi; \
			done; \
			if [ \$$active_count -eq \$$total_count ]; then \
				echo \"PASSED: All \$$active_count/\$$total_count services active\"; \
				exit 0; \
			else \
				echo \"FAILED: Only \$$active_count/\$$total_count services active\"; \
				if [ \$$attempt -lt \$$max_retries ]; then \
					echo \"Retrying in \$$retry_interval seconds...\"; \
					sleep \$$retry_interval; \
				fi; \
			fi; \
		done; \
		echo \"FINAL FAILURE: Validation failed after \$$max_retries attempts\"; \
		exit 1; \
	"

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


