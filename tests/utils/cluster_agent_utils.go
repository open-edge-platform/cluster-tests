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
	// SkipClusterAgentResetEnvVar disables the cluster-agent reset preflight.
	//
	// The cluster-agent hosts the single-node K3s control-plane. Re-running
	// cluster create/delete on the same agent instance can leave persisted K3s
	// state behind. If the k3s token/config changes across runs, k3s may fail
	// to start with:
	//   "bootstrap data already found and encrypted with different token"
	//	which causes Cluster API to stay in WaitingForKthreesServer.
	SkipClusterAgentResetEnvVar = "SKIP_CLUSTER_AGENT_RESET"
)

// ResetClusterAgent recreates the cluster-agent pod and its PVC (statefulset ordinal 0).
//
// This is a test-only hygiene step to ensure the embedded k3s datastore/token
// starts clean for each run.
func ResetClusterAgent() error {
	// Only the ENiC provider has an in-kind `cluster-agent` StatefulSet that we can reset.
	// For vEN, the edge node lifecycle/state reset is handled by the provisioning flow.
	if GetEdgeNodeProvider() != EdgeNodeProviderENiC {
		return nil
	}

	// Behavior controlled by SKIP_CLUSTER_AGENT_RESET:
	//   - "true"  -> never reset (historical behavior)
	//   - "false" -> always reset
	//   - unset    -> auto: reset only if prior k3s bootstrap state is detected
	val := os.Getenv(SkipClusterAgentResetEnvVar)
	if val == "true" {
		return nil
	}

	// Identify the namespace where the `cluster-agent` StatefulSet lives.
	// We keep this discovery-based because some environments deploy it outside `default`.
	nsCmd := exec.Command(
		"kubectl", "get", "statefulset", "-A",
		"-o", "jsonpath={range .items[?(@.metadata.name==\"cluster-agent\")]}{.metadata.namespace}{\"\\n\"}{end}",
	)
	nsOut, err := nsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to locate cluster-agent statefulset: %w", err)
	}
	namespace := strings.TrimSpace(string(nsOut))
	if namespace == "" {
		return fmt.Errorf("cluster-agent statefulset not found")
	}
	// If multiple matches exist (unexpected), use the first non-empty line.
	if strings.Contains(namespace, "\n") {
		for _, line := range strings.Split(namespace, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				namespace = line
				break
			}
		}
	}

	// Compute the PVC name from the StatefulSet volumeClaimTemplates.
	// PVCs follow: <claimTemplateName>-<statefulsetName>-<ordinal>
	claimCmd := exec.Command("kubectl", "-n", namespace, "get", "statefulset", "cluster-agent",
		"-o", "jsonpath={.spec.volumeClaimTemplates[0].metadata.name}")
	claimOut, err := claimCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read cluster-agent volumeClaimTemplates: %w", err)
	}
	claimTemplate := strings.TrimSpace(string(claimOut))
	if claimTemplate == "" {
		// Fallback to the known default used by the ENiC cluster-agent chart.
		claimTemplate = "rancher-volume"
	}
	pvcName := fmt.Sprintf("%s-%s-0", claimTemplate, "cluster-agent")

	if val == "" {
		need, err := shouldResetClusterAgent(namespace)
		if err != nil {
			return err
		}
		if !need {
			return nil
		}
	}

	// Recreate the pod + PVC for a fully fresh /var/lib/rancher state.
	//
	// IMPORTANT: We must prevent the StatefulSet from immediately recreating the pod
	// while the PVC is being deleted; otherwise the PVC can get stuck Terminating due
	// to pvc-protection and the new pod will keep using the old volume.
	//
	// This is the only reliable way we've found to avoid k3s crash-looping with:
	//   "bootstrap data already found and encrypted with different token"
	// under tight test timeouts.

	// 1) Scale down to 0
	if err := runKubectl("-n", namespace, "scale", "statefulset/cluster-agent", "--replicas=0"); err != nil {
		return fmt.Errorf("failed to scale down cluster-agent statefulset: %w", err)
	}
	// Wait for the pod to be deleted (best-effort)
	_ = runKubectl("-n", namespace, "delete", "pod", "cluster-agent-0", "--ignore-not-found")
	_ = runKubectl("-n", namespace, "wait", "--for=delete", "pod/cluster-agent-0", "--timeout=2m")

	// 2) Delete PVC and wait for it to be fully removed
	_ = runKubectl("-n", namespace, "delete", "pvc", pvcName, "--ignore-not-found")
	if err := runKubectl("-n", namespace, "wait", "--for=delete", "pvc/"+pvcName, "--timeout=3m"); err != nil {
		// Provide context (but keep going to try to restore replicas).
		_ = runKubectl("-n", namespace, "describe", "pvc", pvcName)
		return fmt.Errorf("PVC %s was not deleted in time: %w", pvcName, err)
	}

	// 3) Scale back up to 1 and wait for readiness
	if err := runKubectl("-n", namespace, "scale", "statefulset/cluster-agent", "--replicas=1"); err != nil {
		return fmt.Errorf("failed to scale up cluster-agent statefulset: %w", err)
	}
	if err := runKubectl("-n", namespace, "wait", "--for=condition=Ready", "pod/cluster-agent-0", "--timeout=4m"); err != nil {
		return fmt.Errorf("cluster-agent pod did not become Ready: %w", err)
	}

	return nil
}

func shouldResetClusterAgent(namespace string) (bool, error) {
	if GetEdgeNodeProvider() != EdgeNodeProviderENiC {
		return false, nil
	}

	// We only reset if we detect that CAPK/KThrees has previously written a k3s config.
	// On repeated runs, reusing this persisted state can cause k3s to crash-loop with:
	//   "bootstrap data already found and encrypted with different token"
	cmd := exec.Command(
		"kubectl", "-n", namespace,
		"exec", "cluster-agent-0", "--",
		"sh", "-lc", "test -f /etc/rancher/k3s/config.yaml",
	)
	if err := cmd.Run(); err == nil {
		return true, nil
	}
	// If the file is missing (or exec fails), default to not resetting.
	return false, nil
}

func runKubectl(args ...string) error {
	cmd := exec.Command("kubectl", args...)
	// Keep output for diagnostics on failure.
	if out, err := cmd.CombinedOutput(); err != nil {
		trim := strings.TrimSpace(string(out))
		if trim == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, trim)
	}
	return nil
}
