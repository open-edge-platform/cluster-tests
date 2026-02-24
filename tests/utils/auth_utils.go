// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/open-edge-platform/cluster-tests/tests/auth"
)

// Constants for downstream cluster access
const (
	LocalGatewayAddress           = "http://localhost:8081"
	ConnectGatewayInternalAddress = "https://connect-gateway.kind.internal:443"
	TempKubeconfigPattern         = "kubeconfig-*.yaml"
	LocalKubeconfigPattern        = "kubeconfig-local-*.yaml"
	ConnectGatewayPort            = 8081
	PortForwardStartupDelay       = 2 * time.Second
)

// SetupTestAuthentication initializes JWT generation and returns auth context
func SetupTestAuthentication(subject string) (*auth.TestAuthContext, error) {
	// Use the simple SetupTestAuthentication from auth package
	return auth.SetupTestAuthentication(subject)
}

// AuthenticatedHTTPClient creates an HTTP client with JWT authentication
func AuthenticatedHTTPClient(authContext *auth.TestAuthContext) *http.Client {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Add JWT token to requests
	originalTransport := client.Transport
	if originalTransport == nil {
		originalTransport = http.DefaultTransport
	}

	client.Transport = &AuthTransport{
		Transport: originalTransport,
		Token:     authContext.Token,
	}

	return client
}

// AuthTransport adds JWT authentication to HTTP requests
type AuthTransport struct {
	Transport http.RoundTripper
	Token     string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	clonedReq := req.Clone(req.Context())
	clonedReq.Header.Set("Authorization", "Bearer "+t.Token)
	clonedReq.Header.Set("Content-Type", "application/json")
	clonedReq.Header.Set("Accept", "application/json")

	return t.Transport.RoundTrip(clonedReq)
}

// CallClusterManagerAPI makes an authenticated API call to cluster-manager
func CallClusterManagerAPI(authContext *auth.TestAuthContext, method, endpoint string, body interface{}) (*http.Response, error) {
	client := AuthenticatedHTTPClient(authContext)

	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, endpoint, bodyReader)
	} else {
		req, err = http.NewRequest(method, endpoint, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return client.Do(req)
}

// GetClusterManagerEndpoint returns the cluster-manager API endpoint
func GetClusterManagerEndpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%s", PortForwardLocalPort)
}

// GetClusterKubeconfigFromAPI retrieves kubeconfig from cluster-manager API
func GetClusterKubeconfigFromAPI(authContext *auth.TestAuthContext, namespace, clusterName string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/v2/clusters/%s/kubeconfigs", GetClusterManagerEndpoint(), clusterName)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add namespace header as used by cluster-manager
	req.Header.Set("Activeprojectid", namespace)

	client := AuthenticatedHTTPClient(authContext)
	return client.Do(req)
}

// TestClusterManagerAuthentication tests if cluster-manager API accepts JWT authentication
func TestClusterManagerAuthentication(authContext *auth.TestAuthContext) error {
	endpoint := fmt.Sprintf("%s/v2/healthz", GetClusterManagerEndpoint())

	resp, err := CallClusterManagerAPI(authContext, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to call cluster-manager healthz endpoint: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println(" JWT authentication successful")
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("JWT authentication failed: token invalid or expired")
	case http.StatusForbidden:
		return fmt.Errorf("JWT valid but insufficient RBAC permissions")
	default:
		return fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}
}

// GetClusterInfoWithAuth retrieves cluster information using authenticated API call
func GetClusterInfoWithAuth(authContext *auth.TestAuthContext, namespace, clusterName string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/v2/clusters/%s", GetClusterManagerEndpoint(), clusterName)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Activeprojectid", namespace)

	client := AuthenticatedHTTPClient(authContext)
	return client.Do(req)
}

// ImportClusterTemplateAuthenticated imports a cluster template using JWT authentication
func ImportClusterTemplateAuthenticated(authContext *auth.TestAuthContext, namespace string, templateType string) error {
	var data []byte
	var err error
	switch templateType {
	case TemplateTypeK3sBaseline:
		data, err = os.ReadFile(BaselineClusterTemplatePathK3s)
	default:
		return fmt.Errorf("unsupported template type: %s", templateType)
	}

	if err != nil {
		return err
	}

	client := AuthenticatedHTTPClient(authContext)

	req, err := http.NewRequest("POST", ClusterTemplateURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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

// CreateClusterAuthenticated creates a cluster using JWT authentication
func CreateClusterAuthenticated(authContext *auth.TestAuthContext, namespace, nodeGUID, templateName string) error {
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

	client := AuthenticatedHTTPClient(authContext)

	req, err := http.NewRequest("POST", ClusterCreateURL, &configBuffer)
	if err != nil {
		return err
	}

	req.Header.Set("Activeprojectid", namespace)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create cluster: %s", string(body))
	}

	return nil
}

// TestDownstreamClusterAccess tests accessing the downstream cluster using the provided kubeconfig
func TestDownstreamClusterAccess(kubeconfigContent string) error {
	// Write kubeconfig to a temporary file
	tmpFile, err := os.CreateTemp("", TempKubeconfigPattern)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(kubeconfigContent); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	tmpFile.Close()

	// Modify kubeconfig to use local port-forward for connect-gateway
	modifiedKubeconfig := strings.ReplaceAll(kubeconfigContent,
		ConnectGatewayInternalAddress,
		LocalGatewayAddress)

	tmpFileModified, err := os.CreateTemp("", LocalKubeconfigPattern)
	if err != nil {
		return fmt.Errorf("failed to create modified temp file: %w", err)
	}
	defer os.Remove(tmpFileModified.Name())

	if _, err := tmpFileModified.WriteString(modifiedKubeconfig); err != nil {
		return fmt.Errorf("failed to write modified kubeconfig: %w", err)
	}
	tmpFileModified.Close()

	// Set up port-forward to connect-gateway if not already running
	if !isPortForwardRunning(ConnectGatewayPort) {
		cmd := exec.Command("kubectl", "port-forward", "svc/cluster-connect-gateway", fmt.Sprintf("%d:8080", ConnectGatewayPort))
		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start port-forward to connect-gateway: %w", err)
		}
		// Give port-forward a moment to establish
		time.Sleep(PortForwardStartupDelay)
	}

	// Test accessing the downstream cluster - get nodes
	cmd := exec.Command("kubectl", "--kubeconfig", tmpFileModified.Name(), "get", "nodes", "-o", "wide")
	nodeOutput, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to access downstream cluster nodes: %w", err)
	}

	if len(nodeOutput) == 0 {
		return fmt.Errorf("no nodes found in downstream cluster")
	}

	// Test accessing the downstream cluster - get all pods
	cmd = exec.Command("kubectl", "--kubeconfig", tmpFileModified.Name(), "get", "pods", "-A", "-o", "wide")
	podOutput, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get pods from downstream cluster: %w", err)
	}

	// Display the complete downstream cluster information
	fmt.Printf("\n✅ DOWNSTREAM K3S CLUSTER ACCESS SUCCESSFUL!\n")
	fmt.Printf("==========================================\n")
	fmt.Printf("NODES:\n%s\n", string(nodeOutput))
	fmt.Printf("PODS (ALL NAMESPACES):\n%s\n", string(podOutput))
	fmt.Printf("==========================================\n")

	return nil
}

// isPortForwardRunning checks if a port-forward is already running on the specified port
func isPortForwardRunning(port int) bool {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port))
	err := cmd.Run()
	return err == nil
}
