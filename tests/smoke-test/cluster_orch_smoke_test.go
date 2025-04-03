// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package smoke_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	defaultNamespace             = "53cd37b9-66b2-4cc8-b080-3722ed7af64a"
	defaultNodeGUID              = "12345678-1234-1234-1234-123456789012"
	namespaceEnvVar              = "NAMESPACE"
	nodeGUIDEnvVar               = "NODEGUID"
	clusterName                  = "demo-cluster"
	clusterTemplateName          = "baseline-v2.0.1"
	clusterOrchSmoke             = "cluster-orch-smoke-test"
	portForwardAddress           = "0.0.0.0"
	portForwardService           = "svc/cluster-manager"
	portForwardGatewayService    = "svc/cluster-connect-gateway"
	portForwardGatewayLocalPort  = "8081"
	portForwardGatewayRemotePort = "8080"
	portForwardLocalPort         = "8080"
	portForwardRemotePort        = "8080"
	clusterTemplateURL           = "http://127.0.0.1:8080/v2/templates"
	clusterCreateURL             = "http://127.0.0.1:8080/v2/clusters"
	clusterConfigTemplatePath    = "../../configs/cluster-config.json"
	baselineClusterTemplatePath  = "../../configs/baseline-cluster-template.json"
)

var (
	skipDeleteCluster = os.Getenv("SKIP_DELETE_CLUSTER") == "true"
)

func TestClusterOrchSmokeTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch smoke tests\n")
	RunSpecs(t, "cluster orch smoke test suite")
}

var _ = Describe("TC-CO-INT-001: Single Node RKE2 Cluster Create and Delete using Cluster Manager APIs", Ordered, Label(clusterOrchSmoke), func() {
	var (
		gatewayPortForward     *exec.Cmd
		namespace              string
		nodeGUID               string
		portForwardCmd         *exec.Cmd
		clusterCreateStartTime time.Time
		clusterCreateEndTime   time.Time
	)

	BeforeEach(func() {
		namespace = getEnv(namespaceEnvVar, defaultNamespace)
		nodeGUID = getEnv(nodeGUIDEnvVar, defaultNodeGUID)

		// create namespace for the project
		By("Ensuring the namespace exists")
		err := ensureNamespaceExists(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster manager service")
		portForwardCmd = exec.Command("kubectl", "port-forward", portForwardService, fmt.Sprintf("%s:%s", portForwardLocalPort, portForwardRemotePort), "--address", portForwardAddress)
		err = portForwardCmd.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second) // Give some time for port-forwarding to establish

		By("Importing the cluster template")
		err = importClusterTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return isClusterTemplateReady(namespace, clusterTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())

		// Record the start time before creating the cluster
		clusterCreateStartTime = time.Now()

		By("Creating the cluster")
		err = createCluster(namespace, nodeGUID)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster gateway service")
		gatewayPortForward = exec.Command("kubectl", "port-forward", portForwardGatewayService, fmt.Sprintf("%s:%s", portForwardGatewayLocalPort, portForwardGatewayRemotePort), "--address", portForwardAddress)
		err = gatewayPortForward.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second) // Give some time for port-forwarding to establish

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

		if !skipDeleteCluster {
			By("Deleting the cluster")
			err := deleteCluster(namespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying that the cluster is deleted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "-n", namespace, "get", "cluster", clusterName)
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
			cmd := exec.Command("clusterctl", "describe", "cluster", clusterName, "-n", namespace)
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			fmt.Printf("Cluster components status:\n%s\n", string(output))
			return checkAllComponentsReady(string(output))
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())

		By("Checking that connect agent metric shows a successful connection")
		// Fetch metrics
		metrics, err := fetchMetrics()
		Expect(err).NotTo(HaveOccurred())
		defer metrics.Close()
		connectionSucceded, err := parseMetrics(metrics)
		Expect(err).NotTo(HaveOccurred())
		Eventually(connectionSucceded).Should(BeTrue())

		// Record the end time after the cluster is fully active
		clusterCreateEndTime = time.Now()

		// Calculate and print the total time taken
		totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
		fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			logCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--", "/var/lib/rancher/rke2/bin/kubectl", "--kubeconfig", "/etc/rancher/rke2/rke2.yaml", "get", "pods", "-A"})
			logCommandOutput("kubectl", []string{"exec", "cluster-agent-0", "--", "/var/lib/rancher/rke2/bin/kubectl", "--kubeconfig", "/etc/rancher/rke2/rke2.yaml", "describe", "pod", "-n", "kube-system", "connect-agent-cluster-agent-0"})
		}
	})
})

// fetchMetrics fetches the metrics from the /metrics endpoint.
func fetchMetrics() (io.ReadCloser, error) {
	resp, err := http.Get("http://127.0.0.1:8081/metrics")
	if err != nil {
		return nil, fmt.Errorf("error fetching metrics: %v", err)
	}
	return resp.Body, nil
}

// parseMetrics checks if the metric websocket_connections_total with status="succeeded" is 1.
func parseMetrics(metrics io.Reader) (bool, error) {
	scanner := bufio.NewScanner(metrics)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `websocket_connections_total{status="succeeded"}`) {
			fmt.Printf("\tfound metric: %s\n", line)
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[1] != "0" {
				return true, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading metrics: %v", err)
	}

	return false, nil
}

func logCommandOutput(command string, args []string) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
	}
	fmt.Printf("Command output:\n%s\n", string(output))
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func checkAllComponentsReady(output string) bool {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Skip the header line
		if strings.Contains(line, "NAME") && strings.Contains(line, "READY") {
			continue
		}
		// Check if the line contains a "False" status in the "READY" column
		fields := strings.Fields(line)
		// Account for below conditions in below check
		// 1. The second field, which is Ready status, is "False"
		// 2. The second field is not present, which means the component ready status is not available yet
		if (len(fields) > 1 && fields[1] == "False") || len(fields) == 1 {
			return false
		}
	}
	return true
}

func ensureNamespaceExists(namespace string) error {
	cmd := exec.Command("kubectl", "get", "namespace", namespace)
	err := cmd.Run()
	if err != nil {
		// Namespace does not exist, create it
		cmd = exec.Command("kubectl", "create", "namespace", namespace)
		return cmd.Run()
	}
	return nil
}

func importClusterTemplate(namespace string) error {
	data, err := os.ReadFile(baselineClusterTemplatePath)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", clusterTemplateURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to import cluster template: %s", string(body))
	}

	return nil
}

func isClusterTemplateReady(namespace, templateName string) bool {
	cmd := exec.Command("kubectl", "get", "clustertemplates.edge-orchestrator.intel.com", templateName, "-n", namespace, "-o", "yaml")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Use yq to parse the YAML and check the .status.ready field
	cmd = exec.Command("yq", "eval", ".status.ready", "-")
	cmd.Stdin = bytes.NewReader(output)
	readyOutput, err := cmd.Output()
	if err != nil {
		return false
	}

	// Check if the ready status is true
	return strings.TrimSpace(string(readyOutput)) == "true"
}

func createCluster(namespace, nodeGUID string) error {
	templateData, err := os.ReadFile(clusterConfigTemplatePath)
	if err != nil {
		return err
	}

	tmpl, err := template.New("clusterConfig").Parse(string(templateData))
	if err != nil {
		return err
	}

	var configBuffer bytes.Buffer
	err = tmpl.Execute(&configBuffer, struct {
		ClusterName  string
		TemplateName string
		NodeGUID     string
	}{
		NodeGUID:     nodeGUID,
		TemplateName: clusterTemplateName,
		ClusterName:  clusterName,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", clusterCreateURL, &configBuffer)
	if err != nil {
		return err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create cluster: %s", string(body))
	}

	return nil
}

func deleteCluster(namespace string) error {
	url := fmt.Sprintf("%s/%s", clusterCreateURL, clusterName)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete cluster: %s", string(body))
	}

	return nil
}
