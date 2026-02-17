// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package functional_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/cluster-tests/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterOrchRobustnessTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch robustness tests\n")
	RunSpecs(t, "cluster orch robustness test suite")
}

var _ = Describe("Cluster Orch Robustness tests", Ordered, Label(utils.ClusterOrchRobustnessTest), func() {
	var (
		namespace              string
		nodeGUID               string
		portForwardCmd         *exec.Cmd
		gatewayPortForward     *exec.Cmd
		clusterCreateStartTime time.Time
		clusterCreateEndTime   time.Time
	)

	BeforeAll(func() {
		namespace = utils.GetEnv(utils.NamespaceEnvVar, utils.DefaultNamespace)
		nodeGUID = utils.GetEnv(utils.NodeGUIDEnvVar, utils.DefaultNodeGUID)

		// create namespace for the project
		By("Ensuring the namespace exists")
		err := utils.EnsureNamespaceExists(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster manager service")
		portForwardCmd = exec.Command("kubectl", "port-forward", utils.PortForwardService, fmt.Sprintf("%s:%s", utils.PortForwardLocalPort, utils.PortForwardRemotePort), "--address", utils.PortForwardAddress)
		err = portForwardCmd.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second) // Give some time for port-forwarding to establish

		By("Port forwarding to the cluster gateway service")
		gatewayPortForward = exec.Command("kubectl", "port-forward", utils.PortForwardGatewayService, fmt.Sprintf("%s:%s", utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort), "--address", utils.PortForwardAddress)
		err = gatewayPortForward.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second) // Give some time for port-forwarding to establish

	})

	AfterAll(func() {
		defer func() {
			if portForwardCmd != nil && portForwardCmd.Process != nil {
				portForwardCmd.Process.Kill()
			}
		}()

		if !utils.SkipDeleteCluster {
			By("Deleting the cluster")
			err := utils.DeleteCluster(namespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying that the cluster is deleted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "-n", namespace, "get", "cluster", utils.ClusterName)
				err := cmd.Run()
				return err != nil
			}, 1*time.Minute, 5*time.Second).Should(BeTrue())
		}
	})

	It("Test prerequisite: Should successfully import K3s Single Node cluster template", func() {
		By("Importing the cluster template")
		err := utils.ImportClusterTemplate(namespace, utils.TemplateTypeK3sBaseline)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.K3sTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())
	})

	It("Test prerequisite: Should verify that cluster create API should succeed for k3s cluster", func() {
		By("Resetting cluster-agent state (fresh k3s datastore/token)")
		err := utils.ResetClusterAgent()
		Expect(err).NotTo(HaveOccurred())

		// Record the start time before creating the cluster
		clusterCreateStartTime = time.Now()

		By("Creating the cluster")
		err = utils.CreateCluster(namespace, nodeGUID, utils.K3sTemplateName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Test prerequisite: Should verify that the cluster is fully active", func() {
		By("Waiting for IntelMachine to exist")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "-n", namespace, "get", "intelmachine", "-o", "jsonpath={.items[*].metadata.name}")
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			count := 0
			if len(output) > 0 {
				count = len(strings.Fields(string(output)))
			}
			return count > 0
		}, 1*time.Minute, 5*time.Second).Should(BeTrue())

		By("Waiting for all components to be ready")
		Eventually(func() bool {
			cmd := exec.Command("clusterctl", "describe", "cluster", utils.ClusterName, "-n", namespace)
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			fmt.Printf("Cluster components status:\n%s\n", string(output))
			return utils.CheckAllComponentsReady(string(output))
		}, 5*time.Minute, 10*time.Second).Should(BeTrue())
		// Record the end time after the cluster is fully active
		clusterCreateEndTime = time.Now()

		// Calculate and print the total time taken
		totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
		fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)
	})

	It("Test prerequisite: Should verify that the cluster information can be queried	", func() {
		By("Getting the cluster information")
		resp, err := utils.GetClusterInfo(namespace, utils.ClusterName)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		// TODO: Verify the cluster details are correct
	})

	It("Test prerequisite: Should verify that the connect gateway allow access to k8s api", func() {
		// cmd := exec.Command("curl", "-X", "GET", fmt.Sprintf("127.0.0.1:%v/kubernetes/%v-%v/api/v1/namespaces/default/pods", portForwardGatewayLocalPort, namespace, clusterName))
		By("Getting kubeconfig")
		fmt.Println(utils.ClusterName)
		cmd := exec.Command("clusterctl", "get", "kubeconfig", utils.ClusterName, "--namespace", utils.DefaultNamespace) // ">", "kubeconfig.yaml")
		output, err := cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		kubeConfigName := "kubeconfig.yaml"
		err = os.WriteFile(kubeConfigName, output, 0644)
		Expect(err).NotTo(HaveOccurred())

		By("Setting in kubeconfig server to cluster connect gateway")
		cmd = exec.Command("sed", "-i", "s|http://[[:alnum:].-]*:8080/|http://127.0.0.1:8081/|", "kubeconfig.yaml")
		_, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		By("Getting list of pods")
		cmd = exec.Command("kubectl", "--kubeconfig", "kubeconfig.yaml", "get", "pods")
		_, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		// Exec into a pod in the kube-system namespace on the edge node cluster.
		// Note: in k3s, control-plane components like scheduler are not necessarily exposed as pods.
		By("Executing command in local-path-provisioner pod")
		cmd = exec.Command("kubectl", "get", "pods", "-n", "kube-system", "-l", "app=local-path-provisioner",
			"-o", "jsonpath={.items[0].metadata.name}", "--kubeconfig", "kubeconfig.yaml")
		podNameBytes, err := cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		podName := strings.TrimSpace(string(podNameBytes))
		Expect(podName).NotTo(BeEmpty(), "local-path-provisioner pod name should not be empty")

		cmd = exec.Command("kubectl", "exec", "--kubeconfig", "kubeconfig.yaml", "-it", "-n", "kube-system", podName, "--", "ls")
		output, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		By("Printing the output of the command")
		fmt.Printf("Output of `ls` command:\n%s\n", string(output))
	})

	It("Should verify that clusterConnect gateway probes the connection to cluster", func() {
		By("Checking the clusterConnect's LastProbeSuccessTimestamp is not zero")
		Eventually(func() bool {
			// get all clusterconnects - there should be only one, pick its name
			cmd := exec.Command("kubectl", "get", "clusterconnect", "-o", "jsonpath={.items[0].metadata.name}")
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			clusterConnectName := string(output)
			fmt.Printf("ClusterConnect Name: %s\n", clusterConnectName)

			cmd = exec.Command("kubectl", "get", "clusterconnect", clusterConnectName, "-o", "jsonpath={.status.connectionProbe.lastProbeSuccessTimestamp}")
			output, err = cmd.Output()
			if err != nil {
				return false
			}
			lastProbeSuccessTimestamp := string(output)
			if lastProbeSuccessTimestamp == "" {
				fmt.Println("LastProbeSuccessTimestamp is not set yet")
				return false
			}
			fmt.Printf("LastProbeSuccessTimestamp: %s\n", lastProbeSuccessTimestamp)
			return lastProbeSuccessTimestamp != ""
		}, 5*time.Minute, 10*time.Second).Should(BeTrue())
	})

	It("Should verify that a cluster shows connection lost status when connect agent stops working", func() {
		By("Breaking the connect agent by changing its image name in the pod manifest")
		// kubectl exec -n default cluster-agent-0 -- sed -i 's/connect-agent/connectx-agent/g' /var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml
		breakConnectAgentCommand := exec.Command("kubectl", "exec", "-n", "default", "cluster-agent-0", "--", "sed", "-i", "s/connect-agent/connectx-agent/g", "/var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml")
		err := breakConnectAgentCommand.Run()
		Expect(err).NotTo(HaveOccurred())
		connectionLostStartTime := time.Now()

		By("Waiting for intel infra provider to detect connection lost")
		Eventually(func() bool {
			cmd := exec.Command("clusterctl", "describe", "cluster", utils.ClusterName, "-n", namespace)
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			fmt.Printf("Cluster components status:\n%s\n", string(output))
			return utils.CheckLostConnection(string(output))
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())
		// Record the end time after the cluster is fully active
		connectionLostEndTime := time.Now()

		// Calculate and print the total time taken to detect connection lost
		totalTime := connectionLostEndTime.Sub(connectionLostStartTime)
		fmt.Printf("\033[32mTotal time from breaking connect-agent to detect connection lost: %v 🚨🛜\033[0m\n", totalTime)

		By("Getting the cluster information about lost connection")
		resp, err := utils.GetClusterInfo(namespace, utils.ClusterName)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		decoder := json.NewDecoder(resp.Body)
		var clusterInfo map[string]interface{}
		err = decoder.Decode(&clusterInfo)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		By("Verifying the providerStatus.message is 'connect agent is disconnected'")
		providerStatus, ok := clusterInfo["providerStatus"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "providerStatus field is missing or not a map")

		message, ok := providerStatus["message"].(string)
		Expect(ok).To(BeTrue(), "message field is missing or not a string")
		Expect(message).To(ContainSubstring("connect agent is disconnected"), "providerStatus.message does not contain 'connect agent is disconnected'")

	})

	It("Should verify that cluster mark infrastructure as ready when connect-agent is fixed", func() {
		By("Fixing the connect agent by changing its image name in the pod manifest to the right one")
		// kubectl exec -n default cluster-agent-0 -- sed -i 's/connectx-agent/connect-agent/g' /var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml
		fixConnectAgentCommand := exec.Command("kubectl", "exec", "-n", "default", "cluster-agent-0", "--", "sed", "-i", "s/connectx-agent/connect-agent/g", "/var/lib/rancher/k3s/agent/pod-manifests/connect-agent.yaml")
		err := fixConnectAgentCommand.Run()
		Expect(err).NotTo(HaveOccurred())
		connectionRecoveredStartTime := time.Now()

		By("Waiting for all components to be ready again")
		Eventually(func() bool {
			cmd := exec.Command("clusterctl", "describe", "cluster", utils.ClusterName, "-n", namespace)
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			fmt.Printf("Cluster components status:\n%s\n", string(output))
			return utils.CheckAllComponentsReady(string(output))
		}, 5*time.Minute, 10*time.Second).Should(BeTrue())

		connectionRecoveredEndTime := time.Now()

		// Calculate and print the total time taken to recover from connection lost
		totalTime := connectionRecoveredEndTime.Sub(connectionRecoveredStartTime)
		fmt.Printf("\033[32mTotal time from breaking connect-agent to recover from connection lost: %v 🚨🛜 ✅\033[0m\n", totalTime)

	})
})
