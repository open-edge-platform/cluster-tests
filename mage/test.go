// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package mage

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-edge-platform/cluster-tests/tests/utils"

	"github.com/magefile/mage/sh"
	"gopkg.in/yaml.v3"
)

const (
	gitCommitHashRegex = `\b[0-9a-f]{5,40}\b` // Matches a git commit hash (min 5, max 40 characters)
)

type HelmRepo struct {
	URL         string `yaml:"url" json:"url"`
	ReleaseName string `yaml:"release-name" json:"release-name"`
	Package     string `yaml:"package" json:"package"`
	Namespace   string `yaml:"namespace" json:"namespace"`
	Version     string `yaml:"version" json:"version"`
	UseDevel    bool   `yaml:"use-devel" json:"use-devel"`
	Overrides   string `yaml:"overrides" json:"overrides"`
}

type GitRepo struct {
	URL     string `yaml:"url" json:"url"`
	Version string `yaml:"version" json:"version"`
}

type Component struct {
	Name                string     `yaml:"name" json:"name"`
	SkipComponent       bool       `yaml:"skip-component" json:"skip-component"`
	SkipLocalBuild      bool       `yaml:"skip-local-build" json:"skip-local-build"`
	HelmRepo            []HelmRepo `yaml:"helm-repo" json:"helm-repo"`
	GitRepo             GitRepo    `yaml:"git-repo" json:"git-repo"`
	PreInstallCommands  []string   `yaml:"pre-install-commands" json:"pre-install-commands"`
	MakeDirectory       string     `yaml:"make-directory" json:"make-directory"`
	MakeVariables       []string   `yaml:"make-variables" json:"make-variables"`
	MakeTargets         []string   `yaml:"make-targets" json:"make-targets"`
	PostInstallCommands []string   `yaml:"post-install-commands" json:"post-install-commands"`
}

type Config struct {
	KindClusterConfig string      `yaml:"kind-cluster-config" json:"kind-cluster-config"`
	Components        []Component `yaml:"components" json:"components"`
}

func (Test) bootstrap() error {
	defaultConfig, err := parseConfig(".test-dependencies.yaml")
	if err != nil {
		return err
	}

	// In vEN mode, the edge node is provisioned externally (e.g., via libvirt on the runner),
	// so we must not deploy the in-kind ENiC cluster-agent component.
	if strings.EqualFold(os.Getenv(utils.EdgeNodeProviderEnvVar), utils.EdgeNodeProviderVEN) {
		mergeConfigs(defaultConfig, &Config{
			Components: []Component{{
				Name:          "cluster-agent",
				SkipComponent: true,
			}},
		})
	}

	additionalConfigStr := os.Getenv("ADDITIONAL_CONFIG")
	fmt.Printf("Additional config: %s\n", additionalConfigStr)
	if additionalConfigStr != "" {
		var additionalConfig Config
		if err := json.Unmarshal([]byte(additionalConfigStr), &additionalConfig); err != nil {
			return err
		}
		fmt.Printf("Additional config after unmarshal: %+v\n", additionalConfig)

		mergeConfigs(defaultConfig, &additionalConfig)
	}

	if err := createKindCluster(defaultConfig.KindClusterConfig); err != nil {
		return err
	}

	for _, component := range defaultConfig.Components {
		if err := processComponent(component); err != nil {
			return err
		}
	}

	if err := maybeBootstrapVEN(); err != nil {
		return err
	}

	return nil
}

// maybeBootstrapVEN is a hook for VEN-style edge node provisioning/onboarding.
//
// Why it exists: `make <target>` runs `mage test:bootstrap` and then invokes the ginkgo
// suite in a separate process. Environment variables set within this bootstrap process
// won't persist, so VEN setup must write env exports to a file that Make can source.
//
// Behavior:
//   - If EDGE_NODE_PROVIDER != "ven": removes any stale .ven.env and exits.
//   - If EDGE_NODE_PROVIDER == "ven":
//   - If VEN_BOOTSTRAP_CMD is set, run it and require it to create .ven.env
//   - Else, require NODEGUID (or VEN_NODEGUID) to be set and write .ven.env
func maybeBootstrapVEN() error {
	const (
		venEnvFile      = ".ven.env"
		venBootstrapCmd = "VEN_BOOTSTRAP_CMD"
		venNodeGUID     = "VEN_NODEGUID"
	)

	if !strings.EqualFold(os.Getenv(utils.EdgeNodeProviderEnvVar), utils.EdgeNodeProviderVEN) {
		// If we are not in vEN mode, ensure we don't accidentally source a stale NODEGUID.
		_ = os.Remove(venEnvFile)
		return nil
	}

	if cmd := strings.TrimSpace(os.Getenv(venBootstrapCmd)); cmd != "" {
		if err := runCommand(cmd); err != nil {
			return fmt.Errorf("%s failed: %w", venBootstrapCmd, err)
		}
		// The command is expected to create .ven.env.
		if _, err := os.Stat(venEnvFile); err != nil {
			return fmt.Errorf("%s did not create %s: %w", venBootstrapCmd, venEnvFile, err)
		}
		return nil
	}

	// If a prior step already created .ven.env (e.g., local dev bootstrap),
	// allow reusing it as the source of truth.
	if b, err := os.ReadFile(venEnvFile); err == nil {
		content := string(b)
		if strings.Contains(content, "export "+utils.NodeGUIDEnvVar+"=") {
			return nil
		}
	}

	// Minimal path: user/CI provides NODEGUID (or VEN_NODEGUID) and SSH settings.
	nodeGUID := strings.TrimSpace(os.Getenv(utils.NodeGUIDEnvVar))
	if nodeGUID == "" {
		nodeGUID = strings.TrimSpace(os.Getenv(venNodeGUID))
	}
	if nodeGUID == "" {
		return fmt.Errorf("%s=%s requires %s (or %s) to be set, or set %s to provision/onboard the vEN and create .ven.env",
			utils.EdgeNodeProviderEnvVar, utils.EdgeNodeProviderVEN, utils.NodeGUIDEnvVar, venNodeGUID, venBootstrapCmd)
	}

	// Write exports for Makefile to source.
	// Keep it intentionally simple; it's safe to source and easy to inspect in CI artifacts.
	lines := []string{
		"# Generated by mage test:bootstrap (vEN mode)",
		"export " + utils.NodeGUIDEnvVar + "=\"" + nodeGUID + "\"",
	}
	// Also carry through SSH params if provided.
	for _, k := range []string{utils.VENSSHHostEnvVar, utils.VENSSHUserEnvVar, utils.VENSSHPortEnvVar, utils.VENSSHKeyEnvVar} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			lines = append(lines, "export "+k+"=\""+v+"\"")
		}
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(venEnvFile, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", venEnvFile, err)
	}

	return nil
}

func (Test) cleanup() error {
	cmd := "kind delete cluster"
	return runCommand(cmd)
}

// nolint: unused
func (Test) createCluster() error {
	return nil
}

// Test Runs cluster orch smoke test by creating locations, configuring host, creating a cluster and then finally cleanup
func (Test) clusterOrchClusterApiSmokeTest() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		fmt.Sprintf("--label-filter=%s", utils.ClusterOrchClusterApiSmokeTest),
		"./tests/cluster-api-test",
	)
}

// Test Runs cluster orch template api test
func (Test) clusterOrchTemplateApiSmokeTest() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		fmt.Sprintf("--label-filter=%s", utils.ClusterOrchTemplateApiSmokeTest),
		"./tests/template-api-test",
	)
}

// Test Runs cluster orch template api all tests
func (Test) clusterOrchTemplateApiAllTest() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		fmt.Sprintf("--label-filter=%s", utils.ClusterOrchTemplateApiAllTest),
		"./tests/template-api-test",
	)
}

// Test Runs cluster orch cluster api all tests
func (Test) clusterOrchClusterApiAllTest() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		fmt.Sprintf("--label-filter=%s", utils.ClusterOrchClusterApiAllTest),
		"./tests/cluster-api-test",
	)
}

// Test Runs cluster orch roubstness test
func (Test) clusterOrchRobustness() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		fmt.Sprintf("--label-filter=%s", utils.ClusterOrchRobustnessTest),
		"./tests/robustness-test",
	)
}

/////// Helper functions ///////

func mergeConfigs(defaultConfig, additionalConfig *Config) {
	if additionalConfig.KindClusterConfig != "" {
		defaultConfig.KindClusterConfig = additionalConfig.KindClusterConfig
	}

	for _, additionalComponent := range additionalConfig.Components {
		found := false
		for i, defaultComponent := range defaultConfig.Components {
			if defaultComponent.Name == additionalComponent.Name {
				fmt.Printf("Overriding config for component: %s, overriding config: %+v\n", defaultComponent.Name, additionalComponent)
				defaultConfig.Components[i] = mergeComponent(defaultComponent, additionalComponent)
				found = true
				break
			}
		}
		if !found {
			defaultConfig.Components = append(defaultConfig.Components, additionalComponent)
		}
	}
}

func mergeComponent(defaultComponent, additionalComponent Component) Component {
	defaultComponent.SkipComponent = additionalComponent.SkipComponent
	defaultComponent.SkipLocalBuild = additionalComponent.SkipLocalBuild

	if len(additionalComponent.HelmRepo) > 0 {
		defaultComponent.HelmRepo = append(defaultComponent.HelmRepo, additionalComponent.HelmRepo...)
	}
	if additionalComponent.GitRepo.URL != "" {
		defaultComponent.GitRepo.URL = additionalComponent.GitRepo.URL
	}
	if additionalComponent.GitRepo.Version != "" {
		defaultComponent.GitRepo.Version = additionalComponent.GitRepo.Version
	}
	if len(additionalComponent.PreInstallCommands) > 0 {
		defaultComponent.PreInstallCommands = additionalComponent.PreInstallCommands
	}
	if additionalComponent.MakeDirectory != "" {
		defaultComponent.MakeDirectory = additionalComponent.MakeDirectory
	}
	if len(additionalComponent.MakeVariables) > 0 {
		defaultComponent.MakeVariables = additionalComponent.MakeVariables
	}
	if len(additionalComponent.MakeTargets) > 0 {
		defaultComponent.MakeTargets = additionalComponent.MakeTargets
	}
	if len(additionalComponent.PostInstallCommands) > 0 {
		defaultComponent.PostInstallCommands = additionalComponent.PostInstallCommands
	}
	return defaultComponent
}

func parseConfig(file string) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func runCommand(cmd string) error {
	fmt.Println("Running command:", cmd)
	command := exec.Command("bash", "-c", cmd)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func createKindCluster(configFile string) error {
	cmd := fmt.Sprintf("kind create cluster --config %s", configFile)
	return runCommand(cmd)
}

func processComponent(component Component) error {
	if component.SkipComponent {
		fmt.Printf("Skipping component: %s\n", component.Name)
		return nil
	}

	workspaceDir := filepath.Join("_workspace", component.Name)

	if err := os.RemoveAll(workspaceDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workspaceDir, os.ModePerm); err != nil {
		return err
	}

	for _, cmd := range component.PreInstallCommands {
		cmd = fmt.Sprintf("cd %s && %s", workspaceDir, cmd)
		if err := runCommand(cmd); err != nil {
			return err
		}
	}

	if component.SkipLocalBuild {
		for _, helm := range component.HelmRepo {
			chart := fmt.Sprintf("%s/%s", helm.URL, helm.Package)
			cmd := fmt.Sprintf("helm install %s %s --namespace %s", helm.ReleaseName, chart, helm.Namespace)
			if helm.Version != "" {
				cmd = fmt.Sprintf("%s --version %s", cmd, helm.Version)
			}
			if helm.UseDevel {
				cmd = fmt.Sprintf("%s --devel", cmd)
			}
			if helm.Overrides != "" {
				cmd = fmt.Sprintf("%s %s", cmd, helm.Overrides)
			}
			if err := runCommand(cmd); err != nil {
				return err
			}
		}
	} else {
		// Check if the version is a commit hash
		commitHashRegex := regexp.MustCompile(gitCommitHashRegex)
		version := component.GitRepo.Version
		var cloneCmd string
		if commitHashRegex.MatchString(version) {
			cloneCmd = fmt.Sprintf("git clone %s %s && cd %s && git checkout %s", component.GitRepo.URL, workspaceDir, workspaceDir, version)
		} else {
			cloneCmd = fmt.Sprintf("git clone --branch %s %s %s", version, component.GitRepo.URL, workspaceDir)
		}
		if err := runCommand(cloneCmd); err != nil {
			return err
		}

		for _, target := range component.MakeTargets {
			makeDir := filepath.Join(workspaceDir, component.MakeDirectory)
			makeCmd := fmt.Sprintf("cd %s && make %s", makeDir, target)
			if len(component.MakeVariables) > 0 {
				makeCmd = fmt.Sprintf("cd %s && %s make %s", makeDir, strings.Join(component.MakeVariables, " "), target)
			}
			if err := runCommand(makeCmd); err != nil {
				return err
			}
		}
	}

	for _, cmd := range component.PostInstallCommands {
		cmd = fmt.Sprintf("cd %s && %s", workspaceDir, cmd)
		if err := runCommand(cmd); err != nil {
			return err
		}
	}

	return nil
}
