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

// ClusterOrchSmoke Runs cluster orch smoke test
func (t Test) ClusterOrchSmoke() error {
	return t.clusterOrchSmoke()
}

// ClusterOrchFunctional Runs cluster orch functional test
func (t Test) ClusterOrchFunctional() error {
	return t.clusterOrchFunctional()
}

// ClusterOrchRobustness Runs cluster orch robustness test
func (t Test) ClusterOrchRobustness() error {
	return t.clusterOrchRobustness()
}

// ClusterOrchTemplateApiSmoke Runs template api test
func (t Test) ClusterOrchTemplateApiSmoke() error {
	return t.clusterOrchTemplateApiSmoke()
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
