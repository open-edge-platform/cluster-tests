// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/open-edge-platform/cluster-manager/v2/pkg/api"
)

const (
	DefaultNamespace = "53cd37b9-66b2-4cc8-b080-3722ed7af64a"
	DefaultNodeGUID  = "12345678-1234-1234-1234-123456789012"
	NamespaceEnvVar  = "NAMESPACE"
	NodeGUIDEnvVar   = "NODEGUID"
	ClusterName      = "demo-cluster"

	ClusterOrchClusterApiAllTest    = "cluster-orch-cluster-api-all-test"
	ClusterOrchClusterApiSmokeTest  = "cluster-orch-cluster-api-smoke-test"
	ClusterOrchTemplateApiSmokeTest = "cluster-orch-template-api-smoke-test"
	ClusterOrchTemplateApiAllTest   = "cluster-orch-template-api-all-test"

	PortForwardAddress           = "0.0.0.0"
	PortForwardService           = "svc/cluster-manager"
	PortForwardGatewayService    = "svc/cluster-connect-gateway"
	PortForwardLocalPort         = "8080"
	PortForwardRemotePort        = "8080"
	PortForwardGatewayLocalPort  = "8081"
	PortForwardGatewayRemotePort = "8080"

	Rke2TemplateOnlyName    = "baseline-rke2"
	Rke2TemplateOnlyVersion = "v0.0.1"

	K3sTemplateOnlyName    = "baseline-k3s"
	K3sTemplateOnlyVersion = "v0.0.1"

	Rke2TemplateName = "baseline-rke2-v0.0.1"
	K3sTemplateName  = "baseline-k3s-v0.0.1"

	ClusterTemplateURL = "http://127.0.0.1:8080/v2/templates"
	ClusterCreateURL   = "http://127.0.0.1:8080/v2/clusters"
	ClusterSummaryURL  = "http://127.0.0.1:8080/v2/clusters/summary"

	ClusterConfigTemplatePath = "../../configs/cluster-config.json"

	BaselineClusterTemplatePathRke2 = "../../configs/baseline-cluster-template-rke2.json"
	BaselineClusterTemplatePathK3s  = "../../configs/baseline-cluster-template-k3s.json"
)

const (
	TemplateTypeK3sBaseline  = "k3s-baseline"
	TemplateTypeRke2Baseline = "rke2-baseline"
	// Add more template types as needed
)

var (
	SkipDeleteCluster = os.Getenv("SKIP_DELETE_CLUSTER") == "true"
)

// GetEnv retrieves the value of the environment variable or returns the default value if not set.
func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// EnsureNamespaceExists ensures that the specified namespace exists in the cluster.
func EnsureNamespaceExists(namespace string) error {
	cmd := exec.Command("kubectl", "get", "namespace", namespace)
	err := cmd.Run()
	if err != nil {
		// Namespace does not exist, create it
		cmd = exec.Command("kubectl", "create", "namespace", namespace)
		return cmd.Run()
	}
	return nil
}

// ImportClusterTemplate imports a cluster template into the specified namespace.
func ImportClusterTemplate(namespace string, templateType string) error {
	var data []byte
	var err error
	switch templateType {
	case TemplateTypeK3sBaseline:
		data, err = os.ReadFile(BaselineClusterTemplatePathK3s)
	case TemplateTypeRke2Baseline:
		data, err = os.ReadFile(BaselineClusterTemplatePathRke2)
	default:
		return fmt.Errorf("unsupported template type: %s", templateType)
	}

	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", ClusterTemplateURL, bytes.NewBuffer(data))
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

func GetClusterTemplate(namespace, templateName, templateVersion string) (*api.TemplateInfo, error) {

	url := fmt.Sprintf("%s/%s/%s", ClusterTemplateURL, templateName, templateVersion)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get template: %s", string(body))
	}

	var templateInfo api.TemplateInfo
	if err = json.NewDecoder(resp.Body).Decode(&templateInfo); err != nil {
		return nil, fmt.Errorf("failed to decode template info: %v", err)
	}

	return &templateInfo, nil
}

func GetClusterTemplatesWithFilter(namespace, filter string) (*api.TemplateInfoList, error) {
	ClusterTemplateURLWithFilter := fmt.Sprintf("%s?filter=%s", ClusterTemplateURL, filter)
	req, err := http.NewRequest("GET", ClusterTemplateURLWithFilter, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get templates: %s", string(body))
	}
	var templateInfoList api.TemplateInfoList
	if err := json.NewDecoder(resp.Body).Decode(&templateInfoList); err != nil {
		return nil, fmt.Errorf("failed to decode template info list: %v", err)
	}
	return &templateInfoList, nil
}

func DeleteTemplate(namespace, templateName, templateVersion string) error {
	url := fmt.Sprintf("%s/%s/%s", ClusterTemplateURL, templateName, templateVersion)

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
		return fmt.Errorf("failed to delete template: %s", string(body))
	}

	return nil
}

func DeleteAllTemplate(namespace string) error {
	req, err := http.NewRequest("GET", ClusterTemplateURL, nil)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to get templates: %s", string(body))
	}
	var templateInfoList api.TemplateInfoList
	if err := json.NewDecoder(resp.Body).Decode(&templateInfoList); err != nil {
		return fmt.Errorf("failed to decode template info list: %v", err)
	}
	if templateInfoList.TemplateInfoList != nil && len(*templateInfoList.TemplateInfoList) != 0 {
		for _, templateInfo := range *templateInfoList.TemplateInfoList {
			fmt.Printf("Deleting template: %s \n", templateInfo.Name+"-"+templateInfo.Version)
			err := DeleteTemplate(namespace, templateInfo.Name, templateInfo.Version)
			if err != nil {
				return fmt.Errorf("failed to delete template %s: %v", templateInfo.Name+"-"+templateInfo.Version, err)
			}
		}
	}

	return nil
}

func GetDefaultTemplate(namespace string) (*api.DefaultTemplateInfo, error) {
	req, err := http.NewRequest("GET", ClusterTemplateURL+"?default=true", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get templates: %s", string(body))
	}
	var templateInfoList api.TemplateInfoList
	if err := json.NewDecoder(resp.Body).Decode(&templateInfoList); err != nil {
		return nil, fmt.Errorf("failed to decode template info list: %v", err)
	}
	return templateInfoList.DefaultTemplateInfo, nil
}

func SetDefaultTemplate(namespace, name, version string) error {
	url := fmt.Sprintf("%s/%s/default", ClusterTemplateURL, name)
	var err error
	var req *http.Request
	var data []byte
	var defaultTemplateInfo api.DefaultTemplateInfo

	if version != "" {
		defaultTemplateInfo.Version = version
	}

	data, err = json.Marshal(defaultTemplateInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal default template info: %v", err)
	}

	req, err = http.NewRequest("PUT", url, bytes.NewBuffer(data))
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set default template: %s, code: %v", string(body), resp.StatusCode)
	}

	return nil

}

// IsClusterTemplateReady checks if the cluster template is ready.
func IsClusterTemplateReady(namespace, templateName string) bool {
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

// CreateCluster creates a cluster using the provided configuration.
func CreateCluster(namespace, nodeGUID, templateName string) error {
	templateData, err := os.ReadFile(ClusterConfigTemplatePath)
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
		TemplateName: templateName,
		ClusterName:  ClusterName,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", ClusterCreateURL, &configBuffer)
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

// DeleteCluster deletes a cluster by name.
func DeleteCluster(namespace string) error {
	url := fmt.Sprintf("%s/%s", ClusterCreateURL, ClusterName)

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

func DeleteNode(namespace, nodeGUID, query string) error {
	url := fmt.Sprintf("%s/%s/nodes/%s", ClusterCreateURL, ClusterName, nodeGUID)
	if query != "" {
		url += "?" + query
	}

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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete node: code: %v", resp.StatusCode)
	}

	return nil
}

func GetClusterInfo(namespace, clusterName string) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s", ClusterCreateURL, clusterName)
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

func GetClusterInfoByNodeID(namespace, nodeGUID string) (*http.Response, error) {
	url := fmt.Sprintf("%s/%s/nodes/clusterdetail", ClusterCreateURL, nodeGUID)
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

func GetClusterSummary(namespace string) (*api.ClusterSummary, error) {

	req, err := http.NewRequest("GET", ClusterSummaryURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get cluster summary: %s", string(body))
	}

	var summary api.ClusterSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("failed to decode cluster summary: %v", err)
	}

	return &summary, nil
}

func UpdateClusterLabel(namespace, clusterName string, data map[string]string) error {
	url := fmt.Sprintf("%s/%s/labels", ClusterCreateURL, clusterName)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal label data: %v", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
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

	if resp.StatusCode != http.StatusOK {

		return fmt.Errorf("failed to get update cluster label, code: %v", resp.StatusCode)
	}
	return nil
}

// CheckAllComponentsReady verifies if all components in the cluster are ready.
func CheckAllComponentsReady(output string) bool {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Skip the header line
		if strings.Contains(line, "NAME") && strings.Contains(line, "READY") {
			continue
		}
		// Check if the line contains a "False" status in the "READY" column
		fields := strings.Fields(line)
		if (len(fields) > 1 && fields[1] == "False") || len(fields) == 1 {
			return false
		}
	}
	return true
}

// FetchMetrics fetches the metrics from the /metrics endpoint.
func FetchMetrics() (io.ReadCloser, error) {
	resp, err := http.Get("http://127.0.0.1:8081/metrics")
	if err != nil {
		return nil, fmt.Errorf("error fetching metrics: %v", err)
	}
	return resp.Body, nil
}

// ParseMetrics checks if the metric websocket_connections_total with status="succeeded" is 1.
func ParseMetrics(metrics io.Reader) (bool, error) {
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

func LogCommandOutput(command string, args []string) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
	}
	fmt.Printf("Command output:\n%s\n", string(output))
}
