// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package smoke_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
)

func TestClusterOrchSmokeTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch smoke tests\n")
	RunSpecs(t, "cluster orch smoke test suite")
}

var _ = Describe("TC-CO-INT-001: Single Node RKE2 Cluster Create and Delete using Cluster Manager APIs", Ordered, Label(utils.ClusterOrchSmokeTest), func() {
	var (
		gatewayPortForward     *exec.Cmd
		namespace              string
		nodeGUID               string
		portForwardCmd         *exec.Cmd
		clusterCreateStartTime time.Time
		clusterCreateEndTime   time.Time
	)

	BeforeEach(func() {
		namespace = utils.GetEnv(utils.NamespaceEnvVar, utils.DefaultNamespace)
		nodeGUID = utils.GetEnv(utils.NodeGUIDEnvVar, utils.DefaultNodeGUID)

		By("Ensuring the namespace exists")
		err := utils.EnsureNamespaceExists(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster manager service")
		portForwardCmd = exec.Command("kubectl", "port-forward", utils.PortForwardService, fmt.Sprintf("%s:%s", utils.PortForwardLocalPort, utils.PortForwardRemotePort), "--address", utils.PortForwardAddress)
		err = portForwardCmd.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second)

		By("Importing the cluster template")
		err = utils.ImportClusterTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.ClusterTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())

		clusterCreateStartTime = time.Now()

		By("Creating the cluster")
		err = utils.CreateCluster(namespace, nodeGUID)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster gateway service")
		gatewayPortForward = exec.Command("kubectl", "port-forward", utils.PortForwardGatewayService, fmt.Sprintf("%s:%s", utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort), "--address", utils.PortForwardAddress)
		err = gatewayPortForward.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second)
	})

	AfterEach(func() {
		defer func() {
			if portForwardCmd != nil && portForwardCmd.Process != nil {
				portForwardCmd.Process.Kill()
			}
			if gatewayPortForward != nil && gatewayPortForward.Process != nil {
				gatewayPortForward.Process.Kill()
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

	It("should verify that the cluster is fully active", func() {
		By("Waiting for IntelMachine to exist")
		Eventually(func() bool {
			cmd := exec.Command("sh", "-c", fmt.Sprintf("kubectl -n %s get intelmachine -o yaml | yq '.items | length'", namespace))
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			return string(output) > "0"
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
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())

		By("Checking that connect agent metric shows a successful connection")
		metrics, err := utils.FetchMetrics()
		Expect(err).NotTo(HaveOccurred())
		defer metrics.Close()
		connectionSucceeded, err := utils.ParseMetrics(metrics)
		Expect(err).NotTo(HaveOccurred())
		Eventually(connectionSucceeded).Should(BeTrue())

		clusterCreateEndTime = time.Now()
		totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
		fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)

		By("Getting kubeconfig")
		cmd := exec.Command("clusterctl", "get", "kubeconfig", utils.ClusterName, "--namespace", utils.DefaultNamespace)
		output, err := cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		kubeConfigName := "kubeconfig.yaml"
		err = os.WriteFile(kubeConfigName, output, 0644)
		Expect(err).NotTo(HaveOccurred())

		By("Setting in kubeconfig server to cluster connect gateway")
		cmd = exec.Command("sed", "-i", "s|http://[[:alnum:].-]*:8080/|http://127.0.0.1:8081/|", kubeConfigName)
		_, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		By("Getting list of pods")
		cmd = exec.Command("kubectl", "--kubeconfig", kubeConfigName, "get", "pods")
		_, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		By("Dumping kubectl client and server version")
		cmd = exec.Command("kubectl", "version", "--kubeconfig", kubeConfigName)
		output, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		fmt.Printf("kubectl client and server version:\n%s\n", string(output))

		By("Executing command in kube-scheduler-cluster-agent-0 pod")
		cmd = exec.Command("kubectl", "exec", "--kubeconfig", kubeConfigName, "-it", "-n", "kube-system", "kube-scheduler-cluster-agent-0", "--", "ls")
		output, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		fmt.Printf("Output of `ls` command:\n%s\n", string(output))
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--", "/var/lib/rancher/rke2/bin/kubectl", "--kubeconfig", "/etc/rancher/rke2/rke2.yaml", "get", "pods", "-A"})
			utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--", "/var/lib/rancher/rke2/bin/kubectl", "--kubeconfig", "/etc/rancher/rke2/rke2.yaml", "describe", "pod", "-n", "kube-system", "connect-agent-cluster-agent-0"})
		}
	})
})
