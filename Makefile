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

.PHONY: create-rke2
create-rke2: ## Create RKE2 cluster using Custom Resources
	kubectl apply -f configs/rke2/all.yaml

.PHONY: create-k3s
create-k3s: ## Create K3S cluster using Custom Resources
	curl --location '127.0.0.1:8080/v2/clusters' --header 'Activeprojectid: 53cd37b9-66b2-4cc8-b080-3722ed7af64a' --header 'Content-Type: application/json' --header 'Accept: application/json' --data '{"name":"demo-cluster","template":"baseline-k3s-v0.0.1","nodes":[{"id":"12345678-1234-1234-1234-123456789012","role":"all"}],"labels":{"default-extension":"baseline"}}'
	kubectl -n 53cd37b9-66b2-4cc8-b080-3722ed7af64a wait cl demo-cluster --for=condition=Ready --timeout=5m
	sleep 10 # improve later by replacing for wait, if needed
	kubectl exec -it cluster-agent-0 -- kubectl get po -A
	kubectl get po -A

.PHONY: load-k3s-template
load-k3s-template: ## Create K3S cluster template
	curl --location '127.0.0.1:8080/v2/templates' \
		--header 'Activeprojectid: 53cd37b9-66b2-4cc8-b080-3722ed7af64a' \
		--header 'Content-Type: application/json' \
		--header 'Accept: application/json' \
		--data @configs/baseline-cluster-template-k3s.json
	kubectl -n 53cd37b9-66b2-4cc8-b080-3722ed7af64a wait clustertemplate  baseline-k3s-v0.0.1 --for=condition=Ready --timeout=5m
	sleep 1

.PHONY: create-projectid
create-projectid: ## Create projectid
	kubectl create ns 53cd37b9-66b2-4cc8-b080-3722ed7af64a

.PHONY: connect-agent
connect-agent: ## (OBSOLETE) Copy the connect-agent manifest to the EN
	kubectl -n 53cd37b9-66b2-4cc8-b080-3722ed7af64a get cl demo-cluster -o yaml | yq '.spec.topology.variables[0].value.content' > /tmp/connect-agent.yaml
	#CCGIP=$(shell kubectl get svc cluster-connect-gateway -o yaml|yq '.spec.clusterIP') yq -i '.spec.hostAliases[0].ip = strenv(CCGIP)' /tmp/connect-agent.yaml
	#yq -i '.spec.hostAliases[0].hostnames[0] = "cluster-connect-gateway.default.svc"' /tmp/connect-agent.yaml
	kubectl cp /tmp/connect-agent.yaml cluster-agent-0:/var/lib/rancher/k3s/server/manifests/connect-agent.yaml

.PHONY: etcd-proxy
etcd-proxy: ## (OBSOLETE) Fix issues with etcd-proxy manifest
	kubectl cp configs/k3s/etcd-proxy.yaml cluster-agent-0:/var/lib/rancher/k3s/server/manifests/etcd-proxy.yaml

.PHONY: delete-k3s
delete-k3s: ## Delete K3S cluster
	kubectl -n 53cd37b9-66b2-4cc8-b080-3722ed7af64a delete cl demo-cluster

.PHONY: k3s-deploy-verify
k3s-deploy-verify: 
	@start=$$(date +%s); \
	$(MAKE) bootstrap; \
	$(MAKE) create-projectid; \
	$(MAKE) load-k3s-template; \
	$(MAKE) create-k3s; \
	$(MAKE) cluster-status; \
	end=$$(date +%s); \
	elapsed=$$((end - start)); \
	echo "Total time elapsed: $${elapsed} seconds"

.PHONY: cluster-status
cluster-status: ## Get the status of the cluster
	clusterctl -n 53cd37b9-66b2-4cc8-b080-3722ed7af64a describe cluster demo-cluster --show-conditions all

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


