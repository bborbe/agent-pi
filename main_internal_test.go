// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is `package main` rather than `main_test` because the readiness
// check and its address parsing are unexported, and an external test cannot
// reach them. Ginkgo registers specs in one global suite per test binary, and
// the binary holds both packages, so the RunSpecs in main_test.go runs these.
package main

import (
	"net"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dialAddress", func() {
	DescribeTable("turns a provider base URL into a dialable host:port",
		func(raw, expected string) {
			actual, err := dialAddress(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry("https defaults to 443", "https://api.example.com", "api.example.com:443"),
		Entry("http defaults to 80", "http://api.example.com", "api.example.com:80"),
		Entry("an explicit port is kept", "http://api.example.com:8080", "api.example.com:8080"),
		Entry("a bare host defaults to 443", "api.example.com", "api.example.com:443"),
		Entry("a bare host:port is kept", "api.example.com:9999", "api.example.com:9999"),
	)
})

var _ = Describe("readinessHandler", func() {
	It("reports ready without checking when no provider URL is configured", func() {
		// The pi CLI's own default provider is not knowable from here, so the
		// check is skipped and said to be skipped rather than guessed at.
		app := &application{}
		recorder := httptest.NewRecorder()

		app.readinessHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/readiness", nil))

		Expect(recorder.Code).To(Equal(200))
		Expect(recorder.Body.String()).To(ContainSubstring("not checked"))
	})

	It("reports NOT ready when the provider is unreachable", func() {
		// This is the assertion SC5 rests on: the probe must go red because the
		// provider cannot be reached, not merely because a key is wrong.
		app := &application{ProviderBaseURL: "http://127.0.0.1:1"}
		recorder := httptest.NewRecorder()

		app.readinessHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/readiness", nil))

		Expect(recorder.Code).To(Equal(503))
		Expect(recorder.Body.String()).To(ContainSubstring("unreachable"))
	})

	It("reports ready when the provider accepts a connection", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = listener.Close() }()

		app := &application{ProviderBaseURL: "http://" + listener.Addr().String()}
		recorder := httptest.NewRecorder()

		app.readinessHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/readiness", nil))

		Expect(recorder.Code).To(Equal(200))
		Expect(recorder.Body.String()).To(ContainSubstring("reachable"))
	})

	It("reports NOT ready for an unparseable URL rather than crashing", func() {
		app := &application{ProviderBaseURL: "http://[::1"}
		recorder := httptest.NewRecorder()

		app.readinessHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/readiness", nil))

		Expect(recorder.Code).To(Equal(503))
	})

	It("does not hang when the provider is unroutable rather than refusing", func() {
		// A dial to a black-holed address must be bounded, or the readiness probe
		// times out on its own terms and the distinction is lost.
		app := &application{ProviderBaseURL: "http://10.255.255.1:9"}
		recorder := httptest.NewRecorder()
		done := make(chan struct{})

		go func() {
			defer close(done)
			app.readinessHandler().
				ServeHTTP(recorder, httptest.NewRequest("GET", "/readiness", nil))
		}()

		Eventually(done, 10*time.Second).Should(BeClosed())
		Expect(recorder.Code).To(Equal(503))
	})
})
