// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is `package main` because piEnv is unexported and an external test
// cannot reach it. Ginkgo registers specs in one global suite per test binary,
// and the binary holds both packages, so the RunSpecs in main_test.go runs these.
package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("piEnv", func() {
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
