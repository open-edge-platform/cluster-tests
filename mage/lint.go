// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package mage

import (
	"github.com/magefile/mage/sh"
)

func (Lint) golang() error {
	return sh.RunV("$HOME/.asdf/shims/golangci-lint", "run", "-v", "--timeout", "5m0s")
}

func (Lint) yaml() error {
	return sh.RunV("yamllint", "-c", ".yamllint", ".test-dependencies.yaml")
}

func (Lint) markdown() error {
	return sh.RunV(
		"markdownlint-cli2",
		"--config", ".markdownlint.yml",
		"./README.md", "./test-plan/**/*.md",
	)
}

func (Lint) fixMarkdown() error {
	return sh.RunV(
		"markdownlint-cli2", "--fix",
		"--config", ".markdownlint.yml",
		"./README.md", "./test-plan/**/*.md",
	)
}
