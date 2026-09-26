// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is `package main` rather than `main_test` because the readiness
// check, the prompt intake and their address parsing are unexported, and an
// external test cannot reach them. Ginkgo registers specs in one global suite per
// test binary, and the binary holds both packages, so the RunSpecs in main_test.go
// runs these.
package main

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	pilib "github.com/bborbe/agent/pi"
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

// fakeRunner stands in for pilib.Runner. These specs care about what the handler
// does *with* a runner, not about pi, so the runner is reduced to the two things
// the handler can observe: the prompt it was handed, and what it returned.
type fakeRunner struct {
	prompt string
	result *pilib.Result
	err    error
	calls  int
}

func (f *fakeRunner) Run(_ context.Context, prompt string) (*pilib.Result, error) {
	f.calls++
	f.prompt = prompt
	return f.result, f.err
}

// overlapRunner blocks inside Run until released, so a spec can ask whether a
// second prompt got in while the first was still running.
type overlapRunner struct {
	mu       sync.Mutex
	inFlight int
	max      int
	release  chan struct{}
}

func (r *overlapRunner) Run(_ context.Context, _ string) (*pilib.Result, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.max {
		r.max = r.inFlight
	}
	r.mu.Unlock()

	<-r.release

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return &pilib.Result{Result: "ok"}, nil
}

func (r *overlapRunner) entered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inFlight
}

func (r *overlapRunner) maxConcurrent() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.max
}

var _ = Describe("promptHandler", func() {
	It("returns the runner's answer for a POSTed prompt", func() {
		runner := &fakeRunner{result: &pilib.Result{Result: "the answer"}}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(
			recorder,
			httptest.NewRequest("POST", "/prompt", strings.NewReader("a question")),
		)

		Expect(recorder.Code).To(Equal(200))
		Expect(recorder.Body.String()).To(Equal("the answer"))
		Expect(runner.prompt).To(Equal("a question"))
	})

	It("trims surrounding whitespace before handing the prompt to the runner", func() {
		runner := &fakeRunner{result: &pilib.Result{Result: "ok"}}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(
			recorder,
			httptest.NewRequest("POST", "/prompt", strings.NewReader("\n  a question \n")),
		)

		Expect(recorder.Code).To(Equal(200))
		Expect(runner.prompt).To(Equal("a question"))
	})

	It("rejects a non-POST method without running the runner", func() {
		runner := &fakeRunner{result: &pilib.Result{Result: "unused"}}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(recorder, httptest.NewRequest("GET", "/prompt", nil))

		Expect(recorder.Code).To(Equal(405))
		Expect(runner.calls).To(Equal(0))
	})

	It("rejects an empty prompt without running the runner", func() {
		runner := &fakeRunner{result: &pilib.Result{Result: "unused"}}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(
			recorder,
			httptest.NewRequest("POST", "/prompt", strings.NewReader("   \n")),
		)

		Expect(recorder.Code).To(Equal(400))
		Expect(runner.calls).To(Equal(0))
	})

	It("reports a failure without returning the runner's error to the caller", func() {
		// The runner's error can carry the command line it ran, so it is logged and
		// not returned: the caller gets a status, not a transcript.
		runner := &fakeRunner{err: errors.New("boom: /usr/local/bin/pi --model secret")}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(
			recorder,
			httptest.NewRequest("POST", "/prompt", strings.NewReader("a question")),
		)

		Expect(recorder.Code).To(Equal(500))
		Expect(recorder.Body.String()).NotTo(ContainSubstring("secret"))
	})

	It("bounds the prompt it reads rather than buffering an unbounded body", func() {
		// The endpoint is unauthenticated, so an unbounded read would let one caller
		// exhaust the pod's memory — and a service agent that dies is the failure
		// this workload shape exists to avoid.
		runner := &fakeRunner{result: &pilib.Result{Result: "ok"}}
		app := &application{}
		recorder := httptest.NewRecorder()

		app.promptHandler(runner).ServeHTTP(
			recorder,
			httptest.NewRequest(
				"POST",
				"/prompt",
				strings.NewReader(strings.Repeat("x", maxPromptBytes+1024)),
			),
		)

		Expect(recorder.Code).To(Equal(200))
		Expect(len(runner.prompt)).To(Equal(maxPromptBytes))
	})

	It("serializes concurrent prompts rather than interleaving two runs on one session", func() {
		// One identity holds one conversation. The runner writes a single session
		// store on the mounted volume, so two runs in flight together would corrupt
		// it — and the damage would surface later, as a resumed conversation that is
		// subtly someone else's.
		runner := &overlapRunner{release: make(chan struct{})}
		handler := (&application{}).promptHandler(runner)

		first := make(chan struct{})
		go func() {
			defer close(first)
			handler.ServeHTTP(
				httptest.NewRecorder(),
				httptest.NewRequest("POST", "/prompt", strings.NewReader("one")),
			)
		}()
		Eventually(runner.entered).Should(Equal(1))

		second := make(chan struct{})
		go func() {
			defer close(second)
			handler.ServeHTTP(
				httptest.NewRecorder(),
				httptest.NewRequest("POST", "/prompt", strings.NewReader("two")),
			)
		}()

		// Without the lock the second request would be inside the runner within
		// microseconds, so holding at one for this window is the assertion.
		Consistently(runner.entered, 200*time.Millisecond).Should(Equal(1))

		close(runner.release)
		<-first
		<-second
		Expect(runner.maxConcurrent()).To(Equal(1))
	})
})
