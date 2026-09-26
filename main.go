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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	agentlib "github.com/bborbe/agent"
	delivery "github.com/bborbe/agent/delivery"
	"github.com/bborbe/agent/envparse"
	libmetrics "github.com/bborbe/agent/metrics"
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
	// this branch comes before the runner is built — nothing below it is needed to
	// stay up.
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

	piEnv := map[string]string{}
	if a.ProviderAPIKey != "" {
		piEnv["MINIMAX_API_KEY"] = a.ProviderAPIKey
	}

	runner := factory.CreatePiRunner(
		a.AgentDir,
		a.AllowedTools,
		a.Model,
		piEnv,
		a.AgentType == factory.AgentTypeService,
	)
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
// to run, so instead of executing one and exiting it stays alive and serves
// readiness — the shape the Config's `type: service` selects, and the reason a
// StatefulSet rather than a Job is the right workload for it.
func (a *application) runService(
	ctx context.Context,
	registry *prometheus.Registry,
) error {
	glog.V(2).Infof("agent-pi service mode: no task to run; serving readiness on %s", a.Listen)
	return service.Run(
		ctx,
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		a.createHTTPServer(registry),
	)
}

// createHTTPServer serves readiness and metrics for a service agent.
func (a *application) createHTTPServer(registry *prometheus.Registry) run.Func {
	return func(ctx context.Context) error {
		router := http.NewServeMux()
		router.Handle("/readiness", a.readinessHandler())
		router.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(a.Listen, router).Run(ctx)
	}
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
