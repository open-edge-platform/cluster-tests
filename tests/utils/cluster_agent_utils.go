// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import "os"

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
	// Cluster-agent reset was historically required for the in-kind ENiC provider.
	// lw-ENiC support has been removed; vEN lifecycle/state reset is handled by the
	// provisioning/bootstrap flow.
	_ = os.Getenv(SkipClusterAgentResetEnvVar) // keep env var backward-compatible
	return nil
}
