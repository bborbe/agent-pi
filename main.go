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
	"os"
	"time"

	agentlib "github.com/bborbe/agent"
	delivery "github.com/bborbe/agent/delivery"
	"github.com/bborbe/agent/envparse"
	interactive "github.com/bborbe/agent/interactive"
	libmetrics "github.com/bborbe/agent/metrics"
	pilib "github.com/bborbe/agent/pi"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
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

// piEnv is the environment this binary overrides on every pi subprocess it spawns.
//
// It is one helper rather than a literal in each path because the task path and the
// service path must hand pi the same provider configuration: the task path builds its
// runner through factory.CreatePiRunner and the service path through
// factory.CreatePiSessionFactory, and a key present in one and missing in the other
// would make a service agent fail where a task agent works, or the reverse. An unset
// PROVIDER_API_KEY leaves the map empty so pi's own default provider configuration
// applies, rather than an empty key being handed to it.
func (a *application) piEnv() map[string]string {
	piEnv := map[string]string{}
	if a.ProviderAPIKey != "" {
		piEnv["MINIMAX_API_KEY"] = a.ProviderAPIKey
	}
	return piEnv
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
	return factory.CreatePiRunner(a.AgentDir, a.AllowedTools, a.Model, a.piEnv(), sessionID)
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
	// The session factory builds each session's runner on first use, so a session
	// exists exactly when a caller addresses it. Building a runner is construction
	// only — it performs no I/O — so deferring it to the first request costs nothing
	// and is what lets an unseen session id start a fresh conversation without a
	// registration step.
	sessions := factory.CreatePiSessionFactory(a.AgentDir, a.AllowedTools, a.Model, a.piEnv())
	svc := interactive.NewService(sessions, a.Listen, a.ProviderBaseURL, registry)
	return service.Run(
		ctx,
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		svc.Run,
	)
}
