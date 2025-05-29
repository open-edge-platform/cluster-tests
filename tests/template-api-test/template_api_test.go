// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package template_api_test

import (
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
	"os/exec"
	"testing"
	"time"
)

func TestTemplateApiTests(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting template api tests\n")
	RunSpecs(t, "template api test suite")
}

var _ = Describe("Template API Tests", Ordered, func() {
	var (
		namespace      string
		portForwardCmd *exec.Cmd
	)
	BeforeAll(func() {
		namespace = utils.GetEnv(utils.NamespaceEnvVar, utils.DefaultNamespace)

		By("Ensuring the namespace exists")
		err := utils.EnsureNamespaceExists(namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Port forwarding to the cluster manager service")
		portForwardCmd = exec.Command("kubectl", "port-forward", utils.PortForwardService, fmt.Sprintf("%s:%s", utils.PortForwardLocalPort, utils.PortForwardRemotePort), "--address", utils.PortForwardAddress)
		err = portForwardCmd.Start()
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second)

		By("Deleting all templates in the namespace")
		err = utils.DeleteAllTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		defer func() {
			if portForwardCmd != nil && portForwardCmd.Process != nil {
				portForwardCmd.Process.Kill()
			}
		}()

		By("Deleting all templates in the namespace")
		err := utils.DeleteAllTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should validate the template import success", Label(utils.ClusterOrchTemplateApiSmokeTest), func() {
		By("Importing the cluster template rke2 baseline")
		err := utils.ImportClusterTemplate(namespace, utils.TemplateTypeRke2Baseline)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.Rke2TemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())

		By("Importing the cluster template k3s baseline")
		err = utils.ImportClusterTemplate(namespace, utils.TemplateTypeK3sBaseline)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.K3sTemplateName)
		}, 1*time.Minute, 2*time.Second).Should(BeTrue())
	})
})
