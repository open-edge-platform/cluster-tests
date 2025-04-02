// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package functional_test

import (
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
	clusterTemplateName          = "baseline-v0.0.1"
	clusterOrchFunctionalTest    = "cluster-orch-functional-test"
	portForwardAddress           = "0.0.0.0"
	portForwardService           = "svc/cluster-manager"
	portForwardGatewayService    = "svc/cluster-connect-gateway"
	portForwardLocalPort         = "8080"
	portForwardRemotePort        = "8080"
	portForwardGatewayLocalPort  = "8081"
	portForwardGatewayRemotePort = "8080"
	clusterTemplateURL           = "http://127.0.0.1:8080/v2/templates"
	clusterCreateURL             = "http://127.0.0.1:8080/v2/clusters"
	clusterConfigTemplatePath    = "../../configs/cluster-config.json"
	baselineClusterTemplatePath  = "../../configs/baseline-cluster-template.json"
)

var (
	skipDeleteCluster = os.Getenv("SKIP_DELETE_CLUSTER") == "true"
)

func TestClusterOrchFunctionalTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch functional tests\n")
	RunSpecs(t, "cluster orch functional test suite")
}

var _ = Describe("Cluster Orch Functional tests", Ordered, Label(clusterOrchFunctionalTest), func() {
	var (
		namespace              string
		nodeGUID               string
		portForwardCmd         *exec.Cmd
		gatewayPortForward     *exec.Cmd
		clusterCreateStartTime time.Time
		clusterCreateEndTime   time.Time
	)

	BeforeAll(func() {
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

		By("Port forwarding to the cluster gateway service")
		gatewayPortForward = exec.Command("kubectl", "port-forward", portForwardGatewayService, fmt.Sprintf("%s:%s", portForwardGatewayLocalPort, portForwardGatewayRemotePort), "--address", portForwardAddress)
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

	It("TC-CO-INT-002: Should successfully import RKE2 Single Node cluster template", func() {
		By("Importing the cluster template")
		err := importClusterTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return isClusterTemplateReady(namespace, clusterTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())
	})

	It("TC-CO-INT-003: Should verify that cluster create API should succeed", func() {
		// Record the start time before creating the cluster
		clusterCreateStartTime = time.Now()

		By("Creating the cluster")
		err := createCluster(namespace, nodeGUID)
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
			cmd := exec.Command("clusterctl", "describe", "cluster", clusterName, "-n", namespace)
			output, err := cmd.Output()
			if err != nil {
				return false
			}
			fmt.Printf("Cluster components status:\n%s\n", string(output))
			return checkAllComponentsReady(string(output))
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())
		// Record the end time after the cluster is fully active
		clusterCreateEndTime = time.Now()

		// Calculate and print the total time taken
		totalTime := clusterCreateEndTime.Sub(clusterCreateStartTime)
		fmt.Printf("\033[32mTotal time from cluster creation to fully active: %v 🚀 ✅\033[0m\n", totalTime)
	})

	It("TC-CO-INT-005: Should verify that the cluster information can be queried	", func() {
		By("Getting the cluster information")
		resp, err := getClusterInfo(namespace, clusterName)
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
		fmt.Println(clusterName)
		cmd := exec.Command("clusterctl", "get", "kubeconfig", clusterName, "--namespace", defaultNamespace) // ">", "kubeconfig.yaml")
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
	})
	// TODO: Add more functional tests
})

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

func getClusterInfo(namespace, clusterName string) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s", clusterCreateURL, clusterName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	return client.Do(req)
}
