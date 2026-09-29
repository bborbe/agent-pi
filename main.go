// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command agent-pi is the canonical AI-heavy agent: one Pi
// invocation per phase, all logic in the prompt + allowed tools.
//
// This binary is the Kafka entry point — spawned as a K8s Job by
// task/executor with TASK_CONTENT + TASK_ID + PHASE + KAFKA_BROKERS env.
// For local CLI mode (file-based), see cmd/run-task/main.go.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	agentlib "github.com/bborbe/agent"
	delivery "github.com/bborbe/agent/delivery"
	"github.com/bborbe/agent/envparse"
	libmetrics "github.com/bborbe/agent/metrics"
	pilib "github.com/bborbe/agent/pi"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/bborbe/agent-pi/pkg/factory"
)

const agentName = "pi-agent"

// maxPromptBytes bounds the body a single /prompt request may carry. The endpoint is
// unauthenticated and reachable by anything in the namespace, so an unbounded read
// would let one caller exhaust the pod's memory — and a service agent that dies is
// precisely the failure this workload shape exists to avoid.
const maxPromptBytes = 1 << 20

// serviceSessionID is the session identity a service agent's runner is pinned to.
//
// A constant is enough because each service agent has its own pod and its own volume,
// so the id only has to be stable within one agent. And because pi's `--session-id`
// *creates the session when it is missing*, the first prompt and the thousandth take
// the same code path — which is why it is used here rather than `--continue`, whose
// first run would be resuming nothing.
const serviceSessionID = "identity"

// sessionHeader is the request header a caller uses to address a session. It is
// part of the contract this change establishes: callers depend on the exact
// spelling, so it is a constant rather than a literal repeated in the handler.
const sessionHeader = "X-Session-Id"

// sessionIDPattern is the allowed session id format: one to 64 characters, the
// first of which is not '-'. This is the security boundary, not a style
// preference — the id reaches the agent CLI as a command-line argument, so a
// leading '-' would be parsed as a flag, and '.' or '/' would escape a session
// directory if the id were ever used to build a storage path.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// Pi CLI configuration
	AgentDir string `required:"false" arg:"agent-dir" env:"AGENT_DIR" usage:"Agent directory with .pi/ config" default:"agent"`

	// Allowed tools (comma-separated)
	AllowedTools string `required:"false" arg:"allowed-tools" env:"ALLOWED_TOOLS" usage:"Comma-separated list of allowed tools"`

	// Task content from agent pipeline.
	//
	// Deliberately not `required:"true"`: the tag is static and cannot be
	// conditional, but a service agent has no task — it is a long-running
	// identity addressed directly, which is exactly why the CRD exempts
	// `type: service` from the taskType requirement. The requirement is enforced
	// in Run instead, where the agent shape is known: a task-routed agent still
	// fails fast when it is empty.
	TaskContent string `required:"false" arg:"task-content" env:"TASK_CONTENT" usage:"Raw task markdown from vault; required unless AGENT_TYPE=service"`

	// Listen is the address a service agent binds for readiness and metrics.
	// A task-routed agent runs one task and exits, so it never serves.
	Listen string `required:"false" arg:"listen" env:"LISTEN" usage:"Address for readiness/metrics (service agents only)" default:":9090"`

	// ProviderBaseURL is the provider endpoint the pi CLI talks to; it is passed
	// through to the subprocess unchanged. A service agent also *dials* it for its
	// readiness check, so pointing it at an unroutable address makes the pod report
	// NotReady — which is what proves the probe tests provider reachability rather
	// than mere process liveness. Empty means "the pi CLI's own default", and the
	// check is skipped rather than guessed.
	ProviderBaseURL string `required:"false" arg:"provider-base-url" env:"PROVIDER_BASE_URL" usage:"Provider endpoint; a service agent dials it for readiness"`

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Provider routing.
	ProviderAPIKey string `required:"false" arg:"provider-api-key" env:"PROVIDER_API_KEY" usage:"MiniMax API key passed to pi CLI as MINIMAX_API_KEY" display:"length"`

	// Model selection.
	Model string `required:"false" arg:"model" env:"MODEL" usage:"Model name" default:"MiniMax-M2.7-highspeed"`

	// Agent shape, stamped by the executor from the Config's spec.type. "service"
	// is a long-running identity agent that keeps session continuity; empty (or
	// anything else) is a task-routed job agent that must not. See factory.AgentTypeService.
	AgentType string `required:"false" arg:"agent-type" env:"AGENT_TYPE" usage:"Agent shape: 'service' for a long-running identity agent; empty for a task-routed one"`

	// Branch for Kafka result delivery.
	Branch base.Branch `required:"false" arg:"branch" env:"BRANCH" usage:"branch"`

	// TopicPrefix selects the Kafka topic prefix for result delivery.
	TopicPrefix base.TopicPrefix `required:"false" arg:"topic-prefix" env:"TOPIC_PREFIX" usage:"Explicit Kafka topic prefix; empty means unprefixed topics"`

	// Phase to run (framework requires explicit phase).
	Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`

	// Kafka delivery (optional — only active when TASK_ID is set).
	KafkaBrokers libkafka.Brokers `required:"false" arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma separated list of Kafka brokers"`
	// Plain string rather than agentlib.TaskIdentifier: that type's own Validate()
	// rejects an empty value, and the framework runs *type* validation on every
	// field whether or not it is `required` — so a service agent, which has no task
	// and therefore no identifier, could not start even once TASK_CONTENT stopped
	// being mandatory. Same class as TASK_CONTENT, one field over. The value is
	// converted at the point of use, where a non-empty identifier is actually needed.
	TaskID string `required:"false" arg:"task-id"       env:"TASK_ID"       usage:"Agent task identifier for publishing results back to task controller"`

	PushgatewayURL string `required:"false" arg:"pushgateway-url" env:"PUSHGATEWAY_URL" usage:"Prometheus PushGateway URL"          default:"http://pushgateway:9090"`
	TaskType       string `required:"false" arg:"task-type"       env:"TASK_TYPE"       usage:"Task type label for metric grouping" default:"unknown"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	registry := prometheus.NewRegistry()
	jobMetrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
	pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
		Grouping("agent", agentName).
		Grouping("task_type", a.TaskType).
		Collector(registry)
	defer func() {
		if err := pusher.PushContext(ctx); err != nil {
			glog.Warningf("prometheus push failed: %v", err)
			return
		}
		glog.V(2).Infof("prometheus push completed")
	}()
	start := libtime.NewCurrentDateTime().Now().Time()

	glog.V(2).Infof("agent-pi started phase=%s", a.Phase)

	deliverer, closeDeliverer, err := a.createDeliverer(ctx)
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return err
	}
	defer closeDeliverer()

	// A service agent is a long-running identity, not a task runner: it has no
	// TASK_CONTENT, and its job is to stay alive and answer when addressed. That is
	// why the CRD exempts `type: service` from the taskType requirement, and why
	// this branch comes before the *task* runner is built — nothing below it is
	// needed to serve a task.
	//
	// It must not be read as "a service agent needs no runner": runService builds
	// one of its own, with persistence on. Taking this branch as licence to skip the
	// runner entirely is what left a service agent unable to hold a session while
	// still reporting Ready.
	if a.AgentType == factory.AgentTypeService {
		return a.runService(ctx, registry)
	}
	if a.TaskContent == "" {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Errorf(
			ctx,
			"TASK_CONTENT is required for a task-routed agent; it is optional only when AGENT_TYPE=%s",
			factory.AgentTypeService,
		)
	}

	// Persistence is off here, and it is now a literal rather than
	// `a.AgentType == factory.AgentTypeService`: the service branch above returns
	// before this line, so that comparison could only ever be false — it read as a
	// live decision while being a constant. The service path builds its own runner
	// with persistence on, in runService.
	runner := a.createRunner("")
	provider := factory.CreateAgentProvider(runner, envparse.KeyValuePairs(a.EnvContextRaw))
	agent, err := provider.Get(ctx, agentlib.TaskType(a.TaskType))
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "select agent for task_type")
	}

	result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "agent run failed")
	}
	jobMetrics.RecordRun(result.Status)
	jobMetrics.RecordDuration(time.Since(start))
	return agentlib.PrintResult(ctx, result)
}

// sessionIDFromRequest resolves the conversation a prompt belongs to from the
// request's session header.
//
// An absent header is an existing caller and resolves to serviceSessionID, the
// one conversation this service served before the header existed, so nothing
// already talking to it is orphaned. A present header is the caller's own id and
// is accepted only if it matches sessionIDPattern — the id reaches the agent CLI
// as a command-line argument, so it is validated here, before any body is read
// and before any runner exists, rather than trusted downstream.
//
// Absent and present-but-empty are deliberately different outcomes, and
// http.Header.Values is what tells them apart: Get collapses both to "". An
// empty value is a present header that does not match, so it is an error.
//
// The error deliberately does not carry the id. The id is caller-supplied, and a
// rejected id in a log line would let the log enumerate what callers asked for.
func sessionIDFromRequest(r *http.Request) (string, error) {
	values := r.Header.Values(sessionHeader)
	if len(values) == 0 {
		return serviceSessionID, nil
	}
	if !sessionIDPattern.MatchString(values[0]) {
		return "", errors.Errorf(r.Context(), "session id does not match the allowed format")
	}
	return values[0], nil
}

// runnerFactory builds the runner for one session id. It is the seam the prompt
// handler depends on instead of calling factory.CreatePiRunner directly, so a
// spec can observe which session id a request resolved to.
type runnerFactory func(sessionID string) pilib.Runner

// sessionRunner is one session's conversation: the runner that owns it and the
// lock that serializes turns within it. Two sessions hold two sessionRunners and
// never contend; two requests on one session share its lock and take turns.
type sessionRunner struct {
	mu     sync.Mutex
	runner pilib.Runner
}

// sessionRunners holds one runner per session id, built on first use. Building
// lazily is what lets an unseen session id start a fresh conversation: pi's
// --session-id creates the session when it is missing, so no registration step
// exists anywhere in this design.
type sessionRunners struct {
	factory runnerFactory
	mu      sync.Mutex
	byID    map[string]*sessionRunner
}

func newSessionRunners(factory runnerFactory) *sessionRunners {
	return &sessionRunners{
		factory: factory,
		byID:    map[string]*sessionRunner{},
	}
}

// get returns the sessionRunner for id, building and caching it on first use.
//
// s.mu guards the map and nothing else: it is released before the caller takes
// the session's own lock and runs a prompt, so two different sessions never
// contend on this store and one session's slow turn cannot delay another's
// lookup.
func (s *sessionRunners) get(id string) *sessionRunner {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byID[id]
	if !ok {
		session = &sessionRunner{runner: s.factory(id)}
		s.byID[id] = session
	}
	return session
}

// createRunner builds the Pi runner this binary runs prompts with.
//
// sessionID is empty for a task-routed agent and set for a service one. It is one
// parameter rather than a persist flag plus an id because the two are meaningless
// apart: persistence without an identity writes a transcript nothing reads, which is
// exactly what a service agent did before this — it saved every conversation and
// remembered none of them, answering a follow-up question with "No token was
// previously requested to be remembered".
func (a *application) createRunner(sessionID string) pilib.Runner {
	piEnv := map[string]string{}
	if a.ProviderAPIKey != "" {
		piEnv["MINIMAX_API_KEY"] = a.ProviderAPIKey
	}
	return factory.CreatePiRunner(a.AgentDir, a.AllowedTools, a.Model, piEnv, sessionID)
}

// createDeliverer builds the result deliverer: a no-op unless a TASK_ID is set, in
// which case results are published back to the task controller over Kafka. The
// returned func closes the producer and is always safe to call, so the caller can
// defer it unconditionally rather than branching on whether one was opened.
func (a *application) createDeliverer(
	ctx context.Context,
) (agentlib.ResultDeliverer, func(), error) {
	if a.TaskID == "" {
		return delivery.NewNoopResultDeliverer(), func() {}, nil
	}
	if len(a.KafkaBrokers) == 0 {
		return nil, func() {}, errors.Errorf(ctx, "KAFKA_BROKERS must be set when TASK_ID is set")
	}
	syncProducer, err := libkafka.NewSyncProducerWithName(
		ctx,
		a.KafkaBrokers,
		factory.ServiceName,
	)
	if err != nil {
		return nil, func() {}, errors.Wrap(ctx, err, "create sync producer")
	}
	closer := func() {
		if err := syncProducer.Close(); err != nil {
			glog.Warningf("close sync producer failed: %v", err)
		}
	}
	return factory.CreateKafkaResultDeliverer(
		syncProducer,
		a.TopicPrefix,
		agentlib.TaskIdentifier(a.TaskID),
		a.TaskContent,
		libtime.NewCurrentDateTime(),
	), closer, nil
}

// runService is the long-running half of this binary. A service agent has no task
// to run, so instead of executing one and exiting it stays alive and answers when
// addressed — the shape the Config's `type: service` selects, and the reason a
// StatefulSet rather than a Job is the right workload for it.
func (a *application) runService(
	ctx context.Context,
	registry *prometheus.Registry,
) error {
	glog.V(2).Infof(
		"agent-pi service mode: serving readiness, metrics and prompt intake on %s",
		a.Listen,
	)
	// The store is built here and the runners inside it are built on first use, so
	// a session exists exactly when a caller addresses it. Building a runner is
	// construction only — it performs no I/O — so deferring it to the first request
	// costs nothing and is what lets an unseen session id start a fresh
	// conversation without a registration step.
	sessions := newSessionRunners(a.createRunner)
	return service.Run(
		ctx,
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		a.createHTTPServer(registry, sessions),
	)
}

// createHTTPServer serves readiness, metrics and prompt intake for a service agent.
func (a *application) createHTTPServer(
	registry *prometheus.Registry,
	sessions *sessionRunners,
) run.Func {
	return func(ctx context.Context) error {
		router := http.NewServeMux()
		router.Handle("/readiness", a.readinessHandler())
		router.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		router.Handle("/prompt", a.promptHandler(sessions))
		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(a.Listen, router).Run(ctx)
	}
}

// promptHandler runs one prompt through the same Pi runner the task path uses and
// returns its answer as plain text.
//
// It exists so a service agent can actually be addressed. Without an intake the
// service starts, serves readiness and looks healthy while never building a runner
// at all — which makes "holds a session across two prompts" unprovable against the
// shipped artifact, and unprovable is indistinguishable from absent. Delivering the
// prompts by exec'ing `pi` inside the pod instead would prove the volume persists
// while skipping the runner: it would re-test the part already covered by unit specs
// and skip the part that was missing.
//
// The request body is the prompt, the response body the runner's result. The handler
// itself persists nothing — session continuity belongs to the runner, via
// PersistSession, and it is the *second* request that exercises it.
//
// The conversation is chosen per request: an optional session header names it, and a
// request without one is served from the default session exactly as before. Two
// different ids hold two conversations and run at the same time; two requests on one
// id take turns, because a session's transcript is a single store on the mounted
// volume and two runs interleaved there would corrupt the continuity the session
// exists to keep. The id is validated before the body is read and before a runner is
// built, so a malformed one costs nothing and cannot reach the agent process.
//
// Unauthenticated by design, and reachable only from inside the namespace: this
// service has no Ingress and runs in dev. The prompt is never logged — only its
// length and a short digest — and neither is the session id, so the pod's log
// corroborates which turn ran without becoming a second copy of the conversation or
// a list of the conversations that exist.
func (a *application) promptHandler(sessions *sessionRunners) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		sessionID, err := sessionIDFromRequest(r)
		if err != nil {
			http.Error(w, "invalid session id", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxPromptBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("read prompt: %v", err), http.StatusBadRequest)
			return
		}
		prompt := strings.TrimSpace(string(body))
		if prompt == "" {
			http.Error(w, "empty prompt", http.StatusBadRequest)
			return
		}
		digest := sha256.Sum256([]byte(prompt))
		glog.V(2).Infof(
			"prompt intake: bytes=%d sha256=%s",
			len(prompt),
			hex.EncodeToString(digest[:8]),
		)
		session := sessions.get(sessionID)
		session.mu.Lock()
		defer session.mu.Unlock()
		result, err := session.runner.Run(r.Context(), prompt)
		if err != nil {
			glog.Warningf("prompt intake failed: %v", err)
			http.Error(w, "prompt failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, result.GetResult())
	})
}

// readinessHandler reports whether this agent can reach its provider.
//
// It *dials* rather than calling the API: the question a readiness probe asks is
// "can this agent reach its provider at all", and a dial answers it without
// spending a request or needing a valid key. That is also what makes the check
// falsifiable — pointing PROVIDER_BASE_URL at an unroutable address must turn the
// probe red, or it is only testing that the process is alive, which liveness
// already covers.
//
// With PROVIDER_BASE_URL unset the provider is the pi CLI's own default, which
// this binary does not know, so the check is skipped and reported as such rather
// than guessed at.
func (a *application) readinessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.ProviderBaseURL == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "OK (PROVIDER_BASE_URL unset — provider reachability not checked)")
			return
		}
		addr, err := dialAddress(a.ProviderBaseURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("provider unreachable at %s: %v", addr, err),
				http.StatusServiceUnavailable,
			)
			return
		}
		_ = conn.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "OK (provider reachable at %s)", addr)
	})
}

// dialAddress turns a provider base URL into a host:port to dial, defaulting the
// port from the scheme so `https://host` and `host` both work.
func dialAddress(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.Wrapf(context.Background(), err, "parse PROVIDER_BASE_URL %q", raw)
	}
	host := parsed.Host
	if host == "" {
		// No scheme — treat the whole value as host[:port].
		host = raw
	}
	if !strings.Contains(host, ":") {
		if parsed.Scheme == "http" {
			host += ":80"
		} else {
			host += ":443"
		}
	}
	return host, nil
}
