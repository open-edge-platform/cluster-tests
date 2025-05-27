// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package smoke_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"text/template"
	"time"

	"github.com/bitfield/script"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	clusterclassConfigTemplatePath = "../../configs/rke2-intel-clusterclass-example.yaml"
)

func TestClusterOrchK3SSmokeTest(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting cluster orch smoke tests\n")
	RunSpecs(t, "cluster orch smoke test suite")
}

var _ = Describe("TC-CO-INT-001: Single Node K3S Cluster Create and Delete using Custom Resources", Ordered, Label(clusterOrchSmoke), func() {
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
		err = createK3SCluster(namespace, nodeGUID)
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

func createK3SCluster(namespace, nodeGUID string) error {
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

	output, err := script.Echo(configBuffer.String()).Exec("kubectl apply -f -").String()
	if err != nil {
		return err
	}
	fmt.Println(output)

	return nil
}
