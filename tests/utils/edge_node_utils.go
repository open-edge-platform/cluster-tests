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
	// Supported values:
	//   - "enic" (default): in-kind privileged pod (cluster-agent-0)
	//   - "ven": external VM reachable via SSH (see VEN_* env vars below)
	EdgeNodeProviderEnvVar = "EDGE_NODE_PROVIDER"

	EdgeNodeProviderENiC = "enic"
	EdgeNodeProviderVEN  = "ven"

	VENSSHHostEnvVar = "VEN_SSH_HOST"
	VENSSHUserEnvVar = "VEN_SSH_USER"
	VENSSHPortEnvVar = "VEN_SSH_PORT"
	VENSSHKeyEnvVar  = "VEN_SSH_KEY" // path to private key file
)

func GetEdgeNodeProvider() string {
	val := strings.TrimSpace(os.Getenv(EdgeNodeProviderEnvVar))
	if val == "" {
		return EdgeNodeProviderENiC
	}
	val = strings.ToLower(val)
	switch val {
	case EdgeNodeProviderENiC, EdgeNodeProviderVEN:
		return val
	default:
		// Fall back to ENiC to preserve historical behavior.
		return EdgeNodeProviderENiC
	}
}

// ExecOnEdgeNode runs a shell command on the edge node.
//
// ENiC: kubectl exec into cluster-agent-0
// vEN:  ssh into the VM and run the command
func ExecOnEdgeNode(shellCommand string) ([]byte, error) {
	provider := GetEdgeNodeProvider()
	switch provider {
	case EdgeNodeProviderVEN:
		return execOnVEN(shellCommand)
	case EdgeNodeProviderENiC:
		fallthrough
	default:
		return execOnENiC(shellCommand)
	}
}

func execOnENiC(shellCommand string) ([]byte, error) {
	ns, err := findClusterAgentNamespace()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("kubectl", "-n", ns, "exec", "cluster-agent-0", "--", "sh", "-lc", shellCommand)
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

func findClusterAgentNamespace() (string, error) {
	// Identify the namespace where the `cluster-agent` StatefulSet lives.
	cmd := exec.Command(
		"kubectl", "get", "statefulset", "-A",
		"-o", "jsonpath={range .items[?(@.metadata.name==\"cluster-agent\")]}{.metadata.namespace}{\"\\n\"}{end}",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to locate cluster-agent statefulset: %w", err)
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return "", fmt.Errorf("cluster-agent statefulset not found")
	}
	// If multiple matches exist, use the first non-empty line.
	if strings.Contains(val, "\n") {
		for _, line := range strings.Split(val, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				val = line
				break
			}
		}
	}
	return val, nil
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
