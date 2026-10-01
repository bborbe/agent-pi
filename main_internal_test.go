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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("piEnv", func() {
	It("passes the provider key to the subprocess when it is configured", func() {
		app := &application{ProviderAPIKey: "secret"}
		Expect(app.piEnv()).To(Equal(map[string]string{"MINIMAX_API_KEY": "secret"}))
	})

	It("sets no override when no provider key is configured", func() {
		app := &application{}
		Expect(app.piEnv()).To(BeEmpty())
	})
})
