// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cluster_api_test_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-edge-platform/cluster-tests/tests/auth"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
)

func TestClusterApiTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch api tests\n")
	RunSpecs(t, "cluster orch api test suite")
}

var _ = Describe("Single Node K3S Cluster Create and Delete using Cluster Manager APIs with baseline template",
	Ordered, Label(utils.ClusterOrchClusterApiSmokeTest, utils.ClusterOrchClusterApiAllTest), func() {
		var (
			authContext            *auth.TestAuthContext
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

			By("Setting up JWT authentication")
			var err error
			authContext, err = utils.SetupTestAuthentication("test-user")
			Expect(err).NotTo(HaveOccurred())
			Expect(authContext).NotTo(BeNil())
			Expect(authContext.Token).NotTo(BeEmpty())

			By("Ensuring the namespace exists")
			err = utils.EnsureNamespaceExists(namespace)
			Expect(err).NotTo(HaveOccurred())

			By("Port forwarding to the cluster manager service")
			portForwardCmd = exec.Command("kubectl", "port-forward", utils.PortForwardService,
				fmt.Sprintf("%s:%s", utils.PortForwardLocalPort, utils.PortForwardRemotePort), "--address", utils.PortForwardAddress)
			err = portForwardCmd.Start()
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			By("Importing the cluster template with JWT authentication")
			err = utils.ImportClusterTemplateAuthenticated(authContext, namespace, utils.TemplateTypeK3sBaseline)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the cluster template to be ready")
			Eventually(func() bool {
				return utils.IsClusterTemplateReady(namespace, utils.K3sTemplateName)
			}, 1*time.Minute, 2*time.Second).Should(BeTrue())

			clusterCreateStartTime = time.Now()

			By("Creating the k3s cluster")
			err = utils.CreateClusterAuthenticated(authContext, namespace, nodeGUID, utils.K3sTemplateName)
			Expect(err).NotTo(HaveOccurred())

			By("Port forwarding to the cluster gateway service")
			gatewayPortForward = exec.Command("kubectl", "port-forward", utils.PortForwardGatewayService,
				fmt.Sprintf("%s:%s", utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort), "--address", utils.PortForwardAddress)
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

			// Wait for all pods to be running
			By("Waiting for all pods to be running")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "--kubeconfig", kubeConfigName, "get", "pods", "-A", "-o", "jsonpath={.items[*].status.phase}")
				output, err := cmd.Output()
				if err != nil {
					return false
				}
				podStatuses := strings.Fields(string(output))
				for _, status := range podStatuses {
					if status != "Running" && status != "Completed" && status != "Succeeded" {
						return false
					}
				}
				return true
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "Not all pods are in Running or Completed state")

			By("Getting the local-path-provisioner pod name")
			cmd = exec.Command("kubectl", "get", "pods", "-n", "kube-system", "-l", "app=local-path-provisioner",
				"-o", "jsonpath={.items[0].metadata.name}", "--kubeconfig", kubeConfigName)
			output, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred(), "Failed to get the local-path-provisioner pod name")
			fmt.Printf("Local-path-provisioner pod name: %s\n", string(output))

			podName := strings.TrimSpace(string(output))
			Expect(podName).NotTo(BeEmpty(), "Pod name should not be empty")

			By("Executing the `ls` command in the local-path-provisioner pod")
			cmd = exec.Command("kubectl", "exec", "-it", podName, "-n", "kube-system", "--kubeconfig", kubeConfigName, "--", "ls")
			output, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred(), "Failed to execute the `ls` command in the pod")

			fmt.Printf("Output of `ls` command:\n%s\n", string(output))

		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--",
					"/usr/local/bin/kubectl", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml", "get", "pods", "-A"})
				utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--",
					"/usr/local/bin/kubectl", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml", "describe", "pod", "-n", "kube-system", "connect-agent-cluster-agent-0"})
			}
		})
	})

var _ = Describe("Single Node RKE2 Cluster Create and Delete using Cluster Manager APIs with baseline template",
	Ordered, Label(utils.ClusterOrchClusterApiAllTest), func() {
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
			portForwardCmd = exec.Command("kubectl", "port-forward", utils.PortForwardService,
				fmt.Sprintf("%s:%s", utils.PortForwardLocalPort, utils.PortForwardRemotePort), "--address", utils.PortForwardAddress)
			err = portForwardCmd.Start()
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second) // Give some time for port-forwarding to establish

			By("Port forwarding to the cluster gateway service")
			gatewayPortForward = exec.Command("kubectl", "port-forward", utils.PortForwardGatewayService,
				fmt.Sprintf("%s:%s", utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort), "--address", utils.PortForwardAddress)
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

		It("Should successfully import RKE2 Single Node cluster template", func() {
			By("Importing the cluster template")
			err := utils.ImportClusterTemplate(namespace, utils.TemplateTypeRke2Baseline)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the cluster template to be ready")
			Eventually(func() bool {
				return utils.IsClusterTemplateReady(namespace, utils.Rke2TemplateName)
			}, 1*time.Minute, 2*time.Second).Should(BeTrue())
		})

		It("Should verify that cluster create API should succeed for rke2 cluster", func() {
			// Record the start time before creating the cluster
			clusterCreateStartTime = time.Now()

			By("Creating the cluster")
			err := utils.CreateCluster(namespace, nodeGUID, utils.Rke2TemplateName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should verify that the cluster is fully active", func() {
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

		It("Should verify that the cluster information can be queried	", func() {
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

		It("Should verify that the connect gateway allow access to k8s api", func() {
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
			cmd = exec.Command("kubectl", "exec", "--kubeconfig", "kubeconfig.yaml", "-it", "-n",
				"kube-system", "kube-scheduler-cluster-agent-0", "--", "ls")
			output, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred())
			By("Printing the output of the command")
			fmt.Printf("Output of `ls` command:\n%s\n", string(output))
		})
		It("Should verify that a cluster template cannot be deleted if there is a cluster using it", func() {
			By("Trying to delete the cluster template")
			err := utils.DeleteTemplate(namespace, utils.Rke2TemplateOnlyName, utils.Rke2TemplateOnlyVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("clusterTemplate is in use"))
		})
		// TODO: Add more functional tests
	})
