// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-pi binary.
package factory

import (
	agentlib "github.com/bborbe/agent"
	delivery "github.com/bborbe/agent/delivery"
	healthcheck "github.com/bborbe/agent/healthcheck"
	pilib "github.com/bborbe/agent/pi"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"

	"github.com/bborbe/agent-pi/pkg/prompts"
)

// ServiceName is the canonical service name for the agent-pi binary.
const ServiceName = "agent-pi"

// AgentTypeService is the AGENT_TYPE value the executor stamps for a Config whose
// spec.type is service. The executor owns this value — it comes from
// AgentTypeService in agent-task-executor/k8s/apis/agent.benjamin-borbe.de/v1 —
// and this side only reads it. The coupling is silent when it breaks: a mismatch
// means sessions simply stop persisting, with nothing logged.
const AgentTypeService = "service"

// CreatePiRunner constructs a Pi Runner pre-configured with tools, model, and env.
//
// sessionID is the agent's session identity: empty for a task-routed agent, set for
// a long-running identity agent. It is deliberately the *single* knob for both halves
// of continuity rather than a separate persist flag — persistence without an identity
// writes a transcript that nothing ever reads, which is exactly the defect that made
// a service agent save every conversation and remember none of them. Passing an id
// turns persistence on and pins the run to it; passing "" leaves the agent ephemeral,
// so a task-routed run still leaves nothing behind on the shared volume.
//
// See PiRunnerConfig.SessionID for why this is --session-id rather than --continue.
func CreatePiRunner(
	agentDir string,
	allowedTools string,
	model string,
	env map[string]string,
	sessionID string,
) pilib.Runner {
	return pilib.NewRunner(pilib.PiRunnerConfig{
		AgentDir:       agentDir,
		AllowedTools:   allowedTools,
		Model:          model,
		Env:            env,
		PersistSession: sessionID != "",
		SessionID:      sessionID,
	})
}

// CreateFileResultDeliverer creates a ResultDeliverer that writes the agent's output back to a markdown file.
func CreateFileResultDeliverer(filePath string) agentlib.ResultDeliverer {
	return delivery.NewFileResultDeliverer(
		delivery.NewPassthroughContentGenerator(),
		filePath,
	)
}

// CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task updates to Kafka.
func CreateKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	topicPrefix base.TopicPrefix,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return delivery.NewKafkaResultDeliverer(
		syncProducer,
		topicPrefix,
		taskID,
		originalContent,
		delivery.NewPassthroughContentGenerator(),
		currentDateTime,
	)
}

// CreateAgent assembles the 3-phase pi agent from a pre-constructed Runner.
func CreateAgent(
	runner pilib.Runner,
	envContext map[string]string,
) *agentlib.Agent {
	step := pilib.NewStep(pilib.StepConfig{
		Name:          "pi-task",
		Runner:        runner,
		Instructions:  prompts.BuildInstructions(),
		EnvContext:    envContext,
		OutputSection: "## Result",
		NextPhase:     "done",
	})
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("execution", step),
		agentlib.NewPhase("ai_review", step),
	)
}

// CreateAgentProvider wires the per-task-type dispatch table from a
// pre-constructed Runner so callers control Runner lifecycle.
func CreateAgentProvider(
	runner pilib.Runner,
	envContext map[string]string,
) agentlib.AgentProvider {
	domainAgent := CreateAgent(runner, envContext)
	livenessAgent := healthcheck.NewAgent(healthcheck.NewPiStep(runner))
	return agentlib.NewAgentProvider(ServiceName, map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeLLM:         domainAgent,
		agentlib.TaskTypeHealthcheck: livenessAgent,
		agentlib.TaskTypeOAuthProbe:  livenessAgent,
	})
}
