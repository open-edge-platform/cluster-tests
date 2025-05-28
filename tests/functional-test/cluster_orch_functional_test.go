// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package functional_test

import (
	"fmt"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterOrchFunctionalTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch functional tests\n")
	RunSpecs(t, "cluster orch functional test suite")
}

var _ = Describe("Cluster Orch Functional tests", Ordered, Label(utils.ClusterOrchFunctionalTest), func() {
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

	It("TC-CO-INT-002: Should successfully import RKE2 Single Node cluster template", func() {
		By("Importing the cluster template")
		err := utils.ImportClusterTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.ClusterTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())
	})

	It("TC-CO-INT-003: Should verify that cluster create API should succeed", func() {
		// Record the start time before creating the cluster
		clusterCreateStartTime = time.Now()

		By("Creating the cluster")
		err := utils.CreateCluster(namespace, nodeGUID)
		Expect(err).NotTo(HaveOccurred())
	})

	It("TC-CO-INT-004: Should verify that the cluster is fully active", func() {
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
		// Record the end time after the cluster is fully active
		clusterCreateEndTime = time.Now()

		// Calculate and print the total time taken
		totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
		fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)
	})

	It("TC-CO-INT-005: Should verify that the cluster information can be queried	", func() {
		By("Getting the cluster information")
		resp, err := utils.GetClusterInfo(namespace, utils.ClusterName)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		// TODO: Verify the cluster details are correct
	})

	It("TC-CO-INT-006: Should verify that the cluster label can be queried", func() {
		fmt.Printf("TODO: Implement this test\n")
	})

	It("TC-CO-INT-007: Should verify that the cluster label can be updated", func() {
		fmt.Printf("TODO: Implement this test\n")
	})

	It("TC-CO-INT-008: Should verify that the connect gateway allow access to k8s api", func() {
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

		// Exec into one of the pods in the kube-system namespace on the edge node cluster
		By("Executing command in kube-scheduler-cluster-agent-0 pod")
		cmd = exec.Command("kubectl", "exec", "--kubeconfig", "kubeconfig.yaml", "-it", "-n", "kube-system", "kube-scheduler-cluster-agent-0", "--", "ls")
		output, err = cmd.Output()
		Expect(err).NotTo(HaveOccurred())
		By("Printing the output of the command")
		fmt.Printf("Output of `ls` command:\n%s\n", string(output))
	})
	It("TC-CO-INT-009: Should verify that a cluster template cannot be deleted if there is a cluster using it", func() {
		By("Trying to delete the cluster template")
		err := utils.DeleteTemplate(namespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("denied the request: clusterTemplate is in use"))
	})
	// TODO: Add more functional tests
})
