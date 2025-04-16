
# Tests for Cluster Orchestration Service

## Overview

This repo documents the test plan for Cluster Orchestration subsystem in Intel® Open Edge Platform. It also hosts the
integration test framework and test cases that is used test the cluster orchestration subsystem.

## Get started

### Prerequisites

This repo uses the following tools. Make sure you have them installed on your system:

- [mage](https://magefile.org/) build tool to build and run the tests.
- [asdf](https://asdf-vm.com/#/core-manage-asdf-vm) to manage the versions of the tools used in the tests.
- [docker](https://docs.docker.com/get-docker/) to run the tests in a containerized environment.

### Install other dependencies

To install the dependencies, run the following command:

```shell
mage asdfPlugins
```

Make sure to set `export PATH="$HOME/.asdf/shims:$PATH"` in your shell profile to ensure that the tools installed
by asdf are available in your PATH.

### Running the tests

To run the tests, run the following command:

```shell
make test
```

The above step will internally invoke the `bootstrap` make target to bootstrap the environment with the dependencies
configured in `.test-dependencies.yaml` file before running the tests.

Refer the `test-plan/test-plan.md` for the detailed test plan.

#### Configuring test dependencies

While there is a default configuration to bootstrap the test environment, it is also possible for you to configure the
dependencies.

Below is the format the `.test-dependencies.yaml` file. You can add the dependencies that you need to install for your tests.

```shell
# .test-dependencies.yaml
# This YAML file defines the dependencies for the test bootstrap step. It specifies build steps for various dependencies
# required for the test environment. The file contains the following fields:
#
# Fields:
# - kind-cluster-config: Specifies the configuration file for the kind cluster.
#
# - components: A list of components, each with its own configuration:
#   - name: The name of the component.
#   - skip-component: A flag to skip the component during the build process (true/false).
#   - skip-local-build: A flag to skip the local build of the component (true/false).
#   - pre-install-commands: Commands to run before installing the component.
#   - helm-repo: Details for the Helm repositories, including:
#       - url: The URL of the Helm repository.
#         release-name: The release name for the Helm chart.
#         package: The Helm chart package name.
#         namespace: The Kubernetes namespace for the Helm release.
#         version: The version of the Helm chart.
#         overrides: The Helm chart overrides.
#   - git-repo:
#       url: The Git URL of the component's repository.
#       version: The Git branch/tag/commit of the component to use.
#   - make-directory: The directory containing the Makefile.
#   - make-variables: Variables to pass to the `make` command.
#   - make-targets: `make` targets to build the component.
#   - post-install-commands: Commands to run after installing the component.
```

##### Overriding the default configuration

You can override the default configuration by setting the ADDITIONAL_CONFIG environment variable for the `test:bootstrap`
target like below.

```shell
ADDITIONAL_CONFIG='{"components":[{"name":"cluster-api-provider-intel", "skip-local-build": false, "git-repo": {"version":"my-dev-branch"}}]}' mage test:bootstrap
```

This example command will override the version of the `cluster-api-provider-intel` component to `my-dev-branch`.

**NOTE**: The ADDITIONAL_CONFIG should be a valid JSON string and should follow the format specified in the
`.test-dependencies.yaml` file.

## Contribute

We welcome contributions from the community! To contribute, please open a pull request to have your changes reviewed and merged into the main. To learn how to contribute to the project, see the [contributor's guide](https://docs.openedgeplatform.intel.com/edge-manage-docs/main/developer_guide/contributor_guide/index.html). We encourage you to add appropriate unit tests and e2e tests if your contribution introduces a new feature.

The project will accept contributions through Pull-Requests (PRs). PRs must be built successfully by the CI pipeline,
pass linters verifications and the unit tests.

## Community and Support

To learn more about the project, its community, and governance, visit the [Edge Orchestrator Community](https://github.com/intel).
For support, start with [Troubleshooting](https://github.com/intel) or [contact us](https://github.com/intel).

There are several convenience make targets to support developer activities, you can run `mage -l` to see the list of available targets.

## License

Cluster tests is licensed under [Apache 2.0 License](LICENSES/Apache-2.0.txt)
