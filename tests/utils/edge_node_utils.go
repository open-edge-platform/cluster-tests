// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// EdgeNodeProviderEnvVar selects which kind of edge node the tests operate against.
	//
	// Supported value:
	//   - "ven" (default): external VM reachable via SSH (see VEN_* env vars below)
	EdgeNodeProviderEnvVar = "EDGE_NODE_PROVIDER"

	EdgeNodeProviderVEN  = "ven"

	VENSSHHostEnvVar = "VEN_SSH_HOST"
	VENSSHUserEnvVar = "VEN_SSH_USER"
	VENSSHPortEnvVar = "VEN_SSH_PORT"
	VENSSHKeyEnvVar  = "VEN_SSH_KEY" // path to private key file
)

func GetEdgeNodeProvider() string {
	val := strings.TrimSpace(os.Getenv(EdgeNodeProviderEnvVar))
	if val == "" {
		return EdgeNodeProviderVEN
	}
	val = strings.ToLower(val)
	if val == EdgeNodeProviderVEN {
		return val
	}
	// Fall back to vEN as the only supported provider.
	return EdgeNodeProviderVEN
}

// ExecOnEdgeNode runs a shell command on the edge node.
//
// vEN: ssh into the VM and run the command.
func ExecOnEdgeNode(shellCommand string) ([]byte, error) {
	return execOnVEN(shellCommand)
}

func execOnVEN(shellCommand string) ([]byte, error) {
	host := strings.TrimSpace(os.Getenv(VENSSHHostEnvVar))
	user := strings.TrimSpace(os.Getenv(VENSSHUserEnvVar))
	port := strings.TrimSpace(os.Getenv(VENSSHPortEnvVar))
	key := strings.TrimSpace(os.Getenv(VENSSHKeyEnvVar))

	if host == "" {
		return nil, fmt.Errorf("%s must be set when %s=%s", VENSSHHostEnvVar, EdgeNodeProviderEnvVar, EdgeNodeProviderVEN)
	}
	if user == "" {
		user = "root"
	}
	if port == "" {
		port = "22"
	}
	if key == "" {
		return nil, fmt.Errorf("%s must be set to the SSH private key path when %s=%s", VENSSHKeyEnvVar, EdgeNodeProviderEnvVar, EdgeNodeProviderVEN)
	}

	// Disable host key checks to keep CI non-interactive.
	sshArgs := []string{
		"-i", key,
		"-p", port,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", user, host),
		"sh", "-lc", shellCommand,
	}
	cmd := exec.Command("ssh", sshArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trim := strings.TrimSpace(string(out))
		if trim == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, trim)
	}
	return out, nil
}
