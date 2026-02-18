// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package template_api_test

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
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

	It("should validate the template import success", Label(utils.ClusterOrchTemplateApiSmokeTest, utils.ClusterOrchTemplateApiAllTest), func() {
		By("Importing the cluster template k3s baseline")
		err := utils.ImportClusterTemplate(namespace, utils.TemplateTypeK3sBaseline)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for the cluster template to be ready")
		Eventually(func() bool {
			return utils.IsClusterTemplateReady(namespace, utils.K3sTemplateName)
		}, 2*time.Minute, 2*time.Second).Should(BeTrue())
	})

	It("Should be able to retrieve a template", Label(utils.ClusterOrchTemplateApiSmokeTest, utils.ClusterOrchTemplateApiAllTest), func() {
		By("Retrieving the K3s template")
		template, err := utils.GetClusterTemplate(namespace, utils.K3sTemplateOnlyName, utils.K3sTemplateOnlyVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(template.Name + "-" + template.Version).To(Equal(utils.K3sTemplateName))
	})

	It("Should not find a default template when non has been set", Label(utils.ClusterOrchTemplateApiAllTest), func() {
		By("Getting Default template when none has been set")
		defaultTemplateInfo, err := utils.GetDefaultTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(defaultTemplateInfo).To(BeNil(), "Default template should be nil when none has been set")
	})

	It("Should be able to set a default template", Label(utils.ClusterOrchTemplateApiSmokeTest, utils.ClusterOrchTemplateApiAllTest), func() {

		By("Set the default template by providing only template name without version")
		err := utils.SetDefaultTemplate(namespace, utils.K3sTemplateOnlyName, "")
		Expect(err).NotTo(HaveOccurred())

		By("Getting Default template after setting it")
		defaultTemplateInfo, err := utils.GetDefaultTemplate(namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(*defaultTemplateInfo.Name).To(Equal(utils.K3sTemplateOnlyName), "Default template name should match the set template name")
		Expect(defaultTemplateInfo.Version).To(Equal(utils.K3sTemplateOnlyVersion), "Default template version should match the set template version")

		By("Set the default template by providing both template name and version")
		err = utils.SetDefaultTemplate(namespace, utils.K3sTemplateOnlyName, utils.K3sTemplateOnlyVersion)
		Expect(err).NotTo(HaveOccurred())

	})

	It("Should error out when setting a default template with an invalid name", Label(utils.ClusterOrchTemplateApiAllTest), func() {
		By("Setting default template to a non-existing template should error")
		err := utils.SetDefaultTemplate(namespace, "non-existing-template", "v1.0.0")
		Expect(err).To(HaveOccurred(), "Setting default template to a non-existing template should return an error")

	})

	It("Should return templates matching a filter", Label(utils.ClusterOrchTemplateApiAllTest), func() {
		By("Retrieving templates with a filter")
			templates, err := utils.GetClusterTemplatesWithFilter(namespace, "version="+utils.K3sTemplateOnlyVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(templates).ToNot(BeNil(), "Templates should not be nil")
		Expect(templates.TemplateInfoList).ToNot(BeNil())
		Expect(*templates.TemplateInfoList).To(HaveLen(1), "There should be one template matching the filter - k3s")
	})
})
