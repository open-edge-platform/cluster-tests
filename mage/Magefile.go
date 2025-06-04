// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package mage

import (
	"fmt"
	"regexp"

	"github.com/bitfield/script"
	"github.com/magefile/mage/mg"
)

// AsdfPlugins Install ASDF plugins.
func AsdfPlugins() error {
	// Install remaining tools
	if _, err := script.File(".tool-versions").Column(1).MatchRegexp(regexp.MustCompile(`^[^\#]`)).
		ExecForEach("asdf plugin add {{.}}").Stdout(); err != nil {
		return err
	}
	if _, err := script.Exec("asdf install").Stdout(); err != nil {
		return err
	}
	if _, err := script.Exec("asdf current").Stdout(); err != nil {
		return err
	}

	if _, err := script.Exec("asdf reshim").Stdout(); err != nil {
		return err
	}

	fmt.Printf("asdf plugins updated 🔌\n")
	fmt.Printf("make sure to add $HOME/.asdf/shims to your PATH\n")
	return nil
}

////// Test specific targets

type Test mg.Namespace

// Cleanup Cleans up the test environment.
func (t Test) Cleanup() error {
	return t.cleanup()
}

// Bootstrap Bootstraps the test environment.
func (t Test) Bootstrap() error {
	_ = t.cleanup()
	return t.bootstrap()
}

// ClusterOrchClusterApiSmokeTest Runs cluster orch cluster api smoke test
func (t Test) ClusterOrchClusterApiSmokeTest() error {
	return t.clusterOrchClusterApiSmokeTest()
}

// ClusterOrchClusterApiAllTest Runs cluster orch cluster api all tests
func (t Test) ClusterOrchClusterApiAllTest() error {
	return t.clusterOrchClusterApiAllTest()
}

// ClusterOrchTemplateApiSmokeTest Runs template api smoke test
func (t Test) ClusterOrchTemplateApiSmokeTest() error {
	return t.clusterOrchTemplateApiSmokeTest()
}

// ClusterOrchTemplateApiAllTest Runs template api all tests
func (t Test) ClusterOrchTemplateApiAllTest() error {
	return t.clusterOrchTemplateApiAllTest()
}
  
// ClusterOrchRobustness Runs cluster orch robustness test
func (t Test) ClusterOrchRobustness() error {
	return t.clusterOrchRobustness()
}

////// Lint specific targets

type Lint mg.Namespace

// Golang Lint Golang files.
func (l Lint) Golang() error {
	return l.golang()
}

// Yaml Lint Yaml files.
func (l Lint) Yaml() error {
	return l.yaml()
}

// Markdown Lint Markdown files.
func (l Lint) Markdown() error {
	return l.markdown()
}

// FixMarkdown Fix lint issues in markdown files.
func (l Lint) FixMarkdown() error {
	return l.fixMarkdown()
}
