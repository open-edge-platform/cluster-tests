// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"os"

	"github.com/open-edge-platform/cluster-tests/tests/auth"
)

// Constants for OIDC configuration
const (
	DefaultOIDCConfigFile = "oidc-mock-config-dynamic.yaml"
)

// generateOIDCConfigFile generates OIDC mock configuration and writes it to a file
func generateOIDCConfigFile(outputFile string) error {
	if outputFile == "" {
		outputFile = DefaultOIDCConfigFile
	}

	config, err := auth.GenerateOIDCMockConfig()
	if err != nil {
		return fmt.Errorf("failed to generate OIDC config: %w", err)
	}

	err = os.WriteFile(outputFile, []byte(config), 0600)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("Dynamic OIDC mock configuration generated: %s\n", outputFile)
	fmt.Printf("Configuration uses runtime-generated RSA keys for GitGuardian compliance\n")
	fmt.Printf("JWKS generated using dynamic key system\n")
	fmt.Println("Ready to deploy with: kubectl apply -f", outputFile)

	return nil
}
