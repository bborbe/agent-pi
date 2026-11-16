// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is `package main` rather than `main_test` because piEnv is unexported
// and an external test cannot reach it. Ginkgo registers specs in one global suite
// per test binary, and the binary holds both packages, so the RunSpecs in
// main_test.go runs these.
//
// The HTTP surface this file used to cover — readiness, prompt intake and their
// address parsing — now lives in the shared service at
// github.com/bborbe/agent/interactive, which owns and tests the frozen :9090
// contract. Its unit specs moved with the code; a copy here would be a second
// implementation of the same assertions, and a test that imports net/http would
// re-introduce exactly the HTTP handling this repository just deleted.
package main

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("writeProviderCatalogue", func() {
	It("fails rather than skipping silently when HOME is unset", func() {
		previous, hadPrevious := os.LookupEnv("HOME")
		Expect(os.Setenv("HOME", "")).To(Succeed())
		DeferCleanup(func() {
			if hadPrevious {
				Expect(os.Setenv("HOME", previous)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOME")).To(Succeed())
		})

		err := (&application{}).writeProviderCatalogue(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("HOME"))
	})
})

var _ = Describe("piEnv", func() {
	It("passes the provider key to the subprocess when it is configured", func() {
		app := &application{ProviderAPIKey: "secret"}
		Expect(app.piEnv()).To(Equal(map[string]string{"MINIMAX_API_KEY": "secret"}))
	})

	It("passes the provider key under the configured variable name", func() {
		app := &application{ProviderAPIKey: "secret", ProviderKeyEnv: "ROUTER_KEY"}
		Expect(app.piEnv()).To(Equal(map[string]string{"ROUTER_KEY": "secret"}))
	})

	It("falls back to MINIMAX_API_KEY when no variable name is configured", func() {
		app := &application{ProviderAPIKey: "secret"}
		Expect(app.piEnv()).To(Equal(map[string]string{"MINIMAX_API_KEY": "secret"}))
	})

	It("sets no override when no provider key is configured", func() {
		app := &application{}
		Expect(app.piEnv()).To(BeEmpty())
	})
})

// serviceConfig is a fully configured service agent; each spec below blanks exactly
// one value so the failure it asserts is attributable to that value alone.
func serviceConfig() *application {
	return &application{
		InteractiveAuthToken: "secret",
		A2APublicURL:         "https://agent-pi.example/a2a",
		A2AAgentName:         "pi-agent",
	}
}

var _ = Describe("validateServiceConfig", func() {
	It("accepts a service configuration with every value set", func() {
		Expect(serviceConfig().validateServiceConfig(context.Background())).To(Succeed())
	})

	It("rejects a configuration with no auth token", func() {
		app := serviceConfig()
		app.InteractiveAuthToken = ""
		Expect(app.validateServiceConfig(context.Background())).NotTo(Succeed())
	})

	It("rejects a configuration with no public A2A URL", func() {
		app := serviceConfig()
		app.A2APublicURL = ""
		Expect(app.validateServiceConfig(context.Background())).NotTo(Succeed())
	})

	It("rejects a configuration with no A2A agent name", func() {
		app := serviceConfig()
		app.A2AAgentName = ""
		Expect(app.validateServiceConfig(context.Background())).NotTo(Succeed())
	})
})

var _ = Describe("cardConfig", func() {
	It("maps the agent name to the card name and the public URL to the card URL", func() {
		card := serviceConfig().cardConfig()
		Expect(card.Name).To(Equal("pi-agent"))
		Expect(card.PublicURL).To(Equal("https://agent-pi.example/a2a"))
	})
})
