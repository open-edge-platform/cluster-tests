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
func (Test) clusterOrchSmoke() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		"--label-filter=cluster-orch-smoke-test",
		"./tests/smoke-test",
	)
}

// Test Runs cluster orch functional test
func (Test) clusterOrchFunctional() error {
	return sh.RunV(
		"ginkgo",
		"-v",
		"-r",
		"--fail-fast",
		"--race",
		"--label-filter=cluster-orch-functional-test",
		"./tests/functional-test",
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
			cmd := fmt.Sprintf("helm install %s %s --namespace %s --devel", helm.ReleaseName, chart, helm.Namespace)
			if helm.Version != "" {
				cmd = fmt.Sprintf("%s --version %s", cmd, helm.Version)
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
