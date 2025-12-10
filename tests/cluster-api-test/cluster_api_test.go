// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cluster_api_test_test

import (
	"encoding/json"
	"fmt"
	"io"
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

// Constants for commonly used values
const (
	TempKubeconfigPattern    = "/tmp/%s-kubeconfig.yaml"
	KubeconfigFileName       = "kubeconfig.yaml"
	LocalGatewayURL          = "http://127.0.0.1:8081/"
	ClusterReadinessTimeout  = 10 * time.Minute
	ClusterReadinessInterval = 10 * time.Second
	PodReadinessTimeout      = 5 * time.Minute
	PodReadinessInterval     = 10 * time.Second
	PortForwardTimeout       = 1 * time.Minute
	PortForwardInterval      = 5 * time.Second
	PortForwardDelay         = 5 * time.Second
)

// function to check if cluster components are ready
func checkClusterComponentsReady(namespace string) bool {
	cmd := exec.Command("clusterctl", "describe", "cluster", utils.ClusterName, "-n", namespace)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	fmt.Printf("Cluster components status:\n%s\n", string(output))
	return utils.CheckAllComponentsReady(string(output))
}

// function to wait for Intel machines to exist
func waitForIntelMachines(namespace string) {
	By("Waiting for IntelMachine to exist")
	Eventually(func() bool {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("kubectl -n %s get intelmachine -o yaml | yq '.items | length'", namespace))
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		return string(output) > "0"
	}, PortForwardTimeout, PortForwardInterval).Should(BeTrue())
}

// function to wait for cluster components to be ready
func waitForClusterComponentsReady(namespace string) {
	By("Waiting for all components to be ready")
	Eventually(func() bool {
		return checkClusterComponentsReady(namespace)
	}, ClusterReadinessTimeout, ClusterReadinessInterval).Should(BeTrue())
}

func TestClusterApiTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch api tests\n")
	RunSpecs(t, "cluster orch api test suite")
}

// setupPortForwarding sets up port forwarding for any service
func setupPortForwarding(serviceName, serviceIdentifier, localPort, remotePort string) (*exec.Cmd, error) {
	By(fmt.Sprintf("Port forwarding to the %s service", serviceName))
	portForwardCmd := exec.Command("kubectl", "port-forward", serviceIdentifier,
		fmt.Sprintf("%s:%s", localPort, remotePort), "--address", utils.PortForwardAddress)
	err := portForwardCmd.Start()
	if err != nil {
		return nil, err
	}
	time.Sleep(PortForwardDelay)
	return portForwardCmd, nil
}

// cleanupPortForwarding safely kills port forwarding processes
func cleanupPortForwarding(portForwardCmd, gatewayPortForward *exec.Cmd) {
	if portForwardCmd != nil && portForwardCmd.Process != nil {
		portForwardCmd.Process.Kill()
	}
	if gatewayPortForward != nil && gatewayPortForward.Process != nil {
		gatewayPortForward.Process.Kill()
	}
}

// performClusterOperation executes a cluster operation with conditional authentication
func performClusterOperation(operationType string, authDisabled bool, authContext *auth.TestAuthContext,
	namespace, nodeGUID, templateName string) error {

	if !authDisabled {
		fmt.Printf(" Using JWT authentication for cluster %s\n", operationType)
		switch operationType {
		case "import":
			By("Importing the cluster template")
			return utils.ImportClusterTemplateAuthenticated(authContext, namespace, templateName)
		case "create":
			By("Creating the k3s cluster")
			return utils.CreateClusterAuthenticated(authContext, namespace, nodeGUID, templateName)
		case "delete":
			By("Deleting the cluster")
			return utils.DeleteClusterAuthenticated(authContext, namespace)
		default:
			return fmt.Errorf("unknown operation type: %s", operationType)
		}
	} else {
		fmt.Printf(" Using non-authenticated cluster %s\n", operationType)
		switch operationType {
		case "import":
			By("Importing the cluster template")
			return utils.ImportClusterTemplate(namespace, templateName)
		case "create":
			By("Creating the k3s cluster")
			return utils.CreateCluster(namespace, nodeGUID, templateName)
		case "delete":
			By("Deleting the cluster")
			return utils.DeleteCluster(namespace)
		default:
			return fmt.Errorf("unknown operation type: %s", operationType)
		}
	}
}

// validateJWTWorkflow performs comprehensive JWT authentication validation
func validateJWTWorkflow(authContext *auth.TestAuthContext, namespace string) {
	By("Testing JWT-authenticated kubeconfig API endpoint (primary workflow validation)")
	Expect(authContext).NotTo(BeNil())

	By("Confirming JWT authentication usage for cluster operations")
	fmt.Printf(" JWT Token confirmed for cluster operations: %s...\n"+
		" JWT authentication confirmed for:\n"+
		"   - Cluster template import\n"+
		"   - Cluster creation\n"+
		"   - Cluster management APIs\n"+
		"   - Kubeconfig retrieval\n"+
		"   - Cluster deletion (in AfterEach)\n", authContext.Token[:20])

	By("Verifying JWT token structure and claims")
	// Token should be a JWT with header.payload.signature format
	parts := strings.Split(authContext.Token, ".")
	Expect(parts).To(HaveLen(3), "JWT should have 3 parts separated by dots")

	// Check auth context claims
	Expect(authContext.Subject).To(Equal("test-user"))
	Expect(authContext.Issuer).To(Equal("cluster-tests"))
	Expect(authContext.Audience).To(ContainElement("cluster-manager"))

	By("Testing cluster-manager API authentication")
	err := utils.TestClusterManagerAuthentication(authContext)
	if err != nil {
		fmt.Printf("  Authentication test result: %v\n", err)
		testConnectivity()
	} else {
		fmt.Println(" JWT authentication successful")
	}

	By("Testing kubeconfig retrieval via JWT workflow (no fallback)")
	testKubeconfigRetrieval(authContext, namespace)
}

// testConnectivity performs basic connectivity diagnostics
func testConnectivity() {
	By("Attempting basic connectivity test")
	endpoint := fmt.Sprintf("%s/v2/healthz", utils.GetClusterManagerEndpoint())
	resp, connErr := http.Get(endpoint)
	if connErr != nil {
		fmt.Printf("  Cluster-manager API not accessible: %v\n", connErr)
		return
	}
	if resp != nil {
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			fmt.Println(" Cluster-manager API is accessible without authentication")
		case http.StatusUnauthorized:
			fmt.Println(" Cluster-manager API requires authentication (expected)")
		default:
			fmt.Printf("  Unexpected response from cluster-manager: %d\n", resp.StatusCode)
		}
	}
}

// testKubeconfigRetrieval tests kubeconfig API endpoint (JWT workflow validation)
func testKubeconfigRetrieval(authContext *auth.TestAuthContext, namespace string) {
	resp, err := utils.GetClusterKubeconfigFromAPI(authContext, namespace, utils.ClusterName)
	Expect(err).NotTo(HaveOccurred(), "Kubeconfig API call should succeed for JWT workflow validation")

	Expect(resp).NotTo(BeNil(), "API response should not be nil")
	defer resp.Body.Close()
	handleKubeconfigResponse(resp, namespace)
}

// handleKubeconfigResponse processes the kubeconfig API response
func handleKubeconfigResponse(resp *http.Response, namespace string) {
	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println(" Successfully retrieved kubeconfig via cluster-manager API")
		processSuccessfulKubeconfigResponse(resp)
	case http.StatusNotFound:
		Fail(fmt.Sprintf("Cluster '%s' not found in namespace '%s' - JWT workflow validation failed", utils.ClusterName, namespace))
	case http.StatusUnauthorized:
		Fail("JWT authentication failed for kubeconfig endpoint")
	case http.StatusForbidden:
		Fail("JWT token lacks permissions for kubeconfig endpoint")
	default:
		Fail(fmt.Sprintf("Unexpected response from kubeconfig API: %d - JWT workflow validation failed", resp.StatusCode))
	}
}

// processSuccessfulKubeconfigResponse handles successful kubeconfig retrieval
func processSuccessfulKubeconfigResponse(resp *http.Response) {
	By("Validating the kubeconfig content")
	body, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())

	var kubeconfigResponse map[string]interface{}
	err = json.Unmarshal(body, &kubeconfigResponse)
	Expect(err).NotTo(HaveOccurred())

	kubeconfig, exists := kubeconfigResponse["kubeconfig"]
	Expect(exists).To(BeTrue(), "Response should contain kubeconfig field")
	Expect(kubeconfig).NotTo(BeEmpty(), "Kubeconfig should not be empty")

	By("Testing downstream cluster access with retrieved kubeconfig")
	err = utils.TestDownstreamClusterAccess(kubeconfig.(string))
	Expect(err).NotTo(HaveOccurred(), "Downstream cluster access should work with JWT-retrieved kubeconfig")

	fmt.Printf("COMPLETE JWT WORKFLOW SUCCESSFUL: Token → API → Kubeconfig → Downstream K3s Cluster Access\n")
}

// waitForClusterReady performs common cluster readiness validation
func waitForClusterReady(namespace string, clusterCreateStartTime time.Time) time.Time {
	waitForIntelMachines(namespace)
	waitForClusterComponentsReady(namespace)

	By("Checking that connect agent metric shows a successful connection")
	metrics, err := utils.FetchMetrics()
	Expect(err).NotTo(HaveOccurred())
	defer metrics.Close()
	connectionSucceeded, err := utils.ParseMetrics(metrics)
	Expect(err).NotTo(HaveOccurred())
	Eventually(connectionSucceeded).Should(BeTrue())

	clusterCreateEndTime := time.Now()
	totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
	fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)

	return clusterCreateEndTime
}

// validateKubeconfigAndClusterAccess performs kubeconfig validation and cluster access testing
func validateKubeconfigAndClusterAccess() {
	By("Getting kubeconfig")
	cmd := exec.Command("clusterctl", "get", "kubeconfig", utils.ClusterName, "--namespace", utils.DefaultNamespace)
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred())

	kubeConfigName := KubeconfigFileName
	err = os.WriteFile(kubeConfigName, output, 0644)
	Expect(err).NotTo(HaveOccurred())

	By("Setting in kubeconfig server to cluster connect gateway")
	cmd = exec.Command("sed", "-i", fmt.Sprintf("s|http://[[:alnum:].-]*:8080/|%s|", LocalGatewayURL), kubeConfigName)
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
	}, PodReadinessTimeout, PodReadinessInterval).Should(BeTrue(), "Not all pods are in Running or Completed state")

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
			authDisabled           bool
		)

		BeforeEach(func() {
			namespace = utils.GetEnv(utils.NamespaceEnvVar, utils.DefaultNamespace)
			nodeGUID = utils.GetEnv(utils.NodeGUIDEnvVar, utils.DefaultNodeGUID)

			// Check if authentication is disabled via environment variable
			authDisabled = os.Getenv("DISABLE_AUTH") == "true"

			if !authDisabled {
				By("Setting up JWT authentication")
				var err error
				authContext, err = utils.SetupTestAuthentication("test-user")
				Expect(err).NotTo(HaveOccurred())
				Expect(authContext).NotTo(BeNil())
				Expect(authContext.Token).NotTo(BeEmpty())
			} else {
				By("Authentication disabled - skipping JWT setup")
				fmt.Printf("  Authentication disabled (DISABLE_AUTH=true)\n")
			}

			By("Ensuring the namespace exists")
			var err error
			err = utils.EnsureNamespaceExists(namespace)
			Expect(err).NotTo(HaveOccurred())

			// Setup port forwarding using helper function
			portForwardCmd, err = setupPortForwarding("cluster manager", utils.PortForwardService,
				utils.PortForwardLocalPort, utils.PortForwardRemotePort)
			Expect(err).NotTo(HaveOccurred())

			// Import cluster template using helper function
			err = performClusterOperation("import", authDisabled, authContext, namespace, "", utils.TemplateTypeK3sBaseline)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the cluster template to be ready")
			Eventually(func() bool {
				return utils.IsClusterTemplateReady(namespace, utils.K3sTemplateName)
			}, 1*time.Minute, 2*time.Second).Should(BeTrue())

			clusterCreateStartTime = time.Now()

			// Create cluster using helper function
			err = performClusterOperation("create", authDisabled, authContext, namespace, nodeGUID, utils.K3sTemplateName)
			Expect(err).NotTo(HaveOccurred())

			// Setup gateway port forwarding using helper function
			gatewayPortForward, err = setupPortForwarding("cluster gateway", utils.PortForwardGatewayService,
				utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Cleanup port forwarding using helper function
			defer cleanupPortForwarding(portForwardCmd, gatewayPortForward)

			if !utils.SkipDeleteCluster {
				// Delete cluster using helper function
				var err error
				err = performClusterOperation("delete", authDisabled, authContext, namespace, "", "")
				Expect(err).NotTo(HaveOccurred())

				By("Verifying that the cluster is deleted")
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "-n", namespace, "get", "cluster", utils.ClusterName)
					err := cmd.Run()
					return err != nil
				}, PortForwardTimeout, PortForwardInterval).Should(BeTrue())
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
			}, 180*time.Second, 10*time.Second).Should(BeTrue())

			By("Checking that connect agent metric shows a successful connection")
			metrics, err := utils.FetchMetrics()
			Expect(err).NotTo(HaveOccurred())
			defer metrics.Close()
			connectionSucceeded, err := utils.ParseMetrics(metrics)
			Expect(err).NotTo(HaveOccurred())
			Eventually(connectionSucceeded).Should(BeTrue())

			var clusterCreateEndTime time.Time
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

			By("Getting OS release information from the local-path-provisioner pod")
			cmd = exec.Command("kubectl", "exec", "-it", podName, "-n", "kube-system", "--kubeconfig", kubeConfigName, "--", "cat", "/etc/os-release")
			output, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred(), "Failed to get OS release information from the pod")

			fmt.Printf("OS Release information:\n%s\n", string(output))

			By("Getting kernel version from the local-path-provisioner pod")
			cmd = exec.Command("kubectl", "exec", "-it", podName, "-n", "kube-system", "--kubeconfig", kubeConfigName, "--", "uname", "-r")
			output, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred(), "Failed to get kernel version from the pod")

			fmt.Printf("Kernel version: %s\n", strings.TrimSpace(string(output)))
		})

		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--",
					"/usr/local/bin/k3s", "kubectl", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml", "get", "pods", "-A"})
				utils.LogCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--",
					"/usr/local/bin/k3s", "kubectl", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml", "describe", "pod", "-n", "kube-system", "connect-agent-cluster-agent-0"})
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
			time.Sleep(PortForwardDelay) // Give some time for port-forwarding to establish

			By("Port forwarding to the cluster gateway service")
			gatewayPortForward = exec.Command("kubectl", "port-forward", utils.PortForwardGatewayService,
				fmt.Sprintf("%s:%s", utils.PortForwardGatewayLocalPort, utils.PortForwardGatewayRemotePort), "--address", utils.PortForwardAddress)
			err = gatewayPortForward.Start()
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(PortForwardDelay) // Give some time for port-forwarding to establish

		})

		AfterAll(func() {
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
				}, PortForwardTimeout, PortForwardInterval).Should(BeTrue())
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
			}, PortForwardTimeout, PortForwardInterval).Should(BeTrue())

			By("Waiting for all components to be ready")
			Eventually(func() bool {
				cmd := exec.Command("clusterctl", "describe", "cluster", utils.ClusterName, "-n", namespace)
				output, err := cmd.Output()
				if err != nil {
					return false
				}
				fmt.Printf("Cluster components status:\n%s\n", string(output))
				return utils.CheckAllComponentsReady(string(output))
			}, ClusterReadinessTimeout, ClusterReadinessInterval).Should(BeTrue())
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

			kubeConfigName := KubeconfigFileName
			err = os.WriteFile(kubeConfigName, output, 0644)
			Expect(err).NotTo(HaveOccurred())

			By("Setting in kubeconfig server to cluster connect gateway")
			cmd = exec.Command("sed", "-i", fmt.Sprintf("s|http://[[:alnum:].-]*:8080/|%s|", LocalGatewayURL), KubeconfigFileName)
			_, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred())

			By("Getting list of pods")
			cmd = exec.Command("kubectl", "--kubeconfig", KubeconfigFileName, "get", "pods")
			_, err = cmd.Output()
			Expect(err).NotTo(HaveOccurred())

			// Exec into one of the pods in the kube-system namespace on the edge node cluster
			By("Executing command in kube-scheduler-cluster-agent-0 pod")
			cmd = exec.Command("kubectl", "exec", "--kubeconfig", KubeconfigFileName, "-it", "-n",
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
