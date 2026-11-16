// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package providercatalogue_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-pi/pkg/providercatalogue"
)

// catalogueDocument is the shape pi's parser reads. Asserting through it, rather
// than by substring-matching the raw bytes, is what makes the spec a contract
// test: the JSON is consumed by a third-party CLI whose parser is the contract.
type catalogueDocument struct {
	Providers map[string]struct {
		BaseURL string `json:"baseUrl"`
		API     string `json:"api"`
		APIKey  string `json:"apiKey"`
		Models  []struct {
			ID            string   `json:"id"`
			Reasoning     bool     `json:"reasoning"`
			Input         []string `json:"input"`
			ContextWindow int      `json:"contextWindow"`
			MaxTokens     int      `json:"maxTokens"`
		} `json:"models"`
	} `json:"providers"`
}

var configured = providercatalogue.Settings{
	ProviderBaseURL: "http://router.example:8788",
	ProviderKeyEnv:  "ROUTER_KEY",
	Model:           "claude-sonnet-5",
	ContextWindow:   200000,
	MaxTokens:       64000,
}

var _ = Describe("Models", func() {
	It("returns nothing when no base URL is configured", func() {
		data, err := providercatalogue.Models(context.Background(), providercatalogue.Settings{Model: "ignored"})
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(BeNil())
	})

	It("renders the provider, model limits and the credential variable name", func() {
		data, err := providercatalogue.Models(context.Background(), configured)
		Expect(err).NotTo(HaveOccurred())

		var document catalogueDocument
		Expect(json.Unmarshal(data, &document)).To(Succeed())
		Expect(document.Providers).To(HaveLen(1))

		provider, ok := document.Providers["custom"]
		Expect(ok).To(BeTrue())
		Expect(provider.BaseURL).To(Equal(configured.ProviderBaseURL))
		Expect(provider.API).To(Equal("anthropic-messages"))
		Expect(provider.APIKey).To(Equal("$ROUTER_KEY"))
		Expect(provider.Models).To(HaveLen(1))
		Expect(provider.Models[0].ID).To(Equal(configured.Model))
		Expect(provider.Models[0].Reasoning).To(BeTrue())
		Expect(provider.Models[0].Input).To(Equal([]string{"text"}))
		Expect(provider.Models[0].ContextWindow).To(Equal(configured.ContextWindow))
		Expect(provider.Models[0].MaxTokens).To(Equal(configured.MaxTokens))
	})
})

var _ = Describe("Write", func() {
	var agentDir string

	BeforeEach(func() {
		var err error
		agentDir, err = os.MkdirTemp("", "providercatalogue")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(agentDir)).To(Succeed())
		})
	})

	It("writes models.json with the bytes Models returns", func() {
		Expect(providercatalogue.Write(context.Background(), configured, agentDir)).To(Succeed())

		written, err := os.ReadFile(filepath.Join(agentDir, "models.json"))
		Expect(err).NotTo(HaveOccurred())
		expected, err := providercatalogue.Models(context.Background(), configured)
		Expect(err).NotTo(HaveOccurred())
		Expect(written).To(Equal(expected))
	})

	It("creates the agent directory when it does not exist", func() {
		nested := filepath.Join(agentDir, "missing", "agent")
		Expect(providercatalogue.Write(context.Background(), configured, nested)).To(Succeed())

		_, err := os.Stat(filepath.Join(nested, "models.json"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when the agent directory cannot be created", func() {
		blocked := filepath.Join(agentDir, "blocked")
		Expect(os.WriteFile(blocked, []byte("not a directory"), 0600)).To(Succeed())

		err := providercatalogue.Write(context.Background(), configured, filepath.Join(blocked, "agent"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("create pi agent dir"))
	})

	It("fails when models.json cannot be written", func() {
		Expect(os.Mkdir(filepath.Join(agentDir, "models.json"), 0750)).To(Succeed())

		err := providercatalogue.Write(context.Background(), configured, agentDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("write"))
	})

	It("writes nothing and leaves an existing catalogue untouched without a base URL", func() {
		path := filepath.Join(agentDir, "models.json")
		Expect(os.WriteFile(path, []byte("pre-existing"), 0600)).To(Succeed())

		Expect(providercatalogue.Write(context.Background(), providercatalogue.Settings{}, agentDir)).To(Succeed())

		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("pre-existing"))
	})
})
