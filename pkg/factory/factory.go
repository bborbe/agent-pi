// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-pi binary.
package factory

import (
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"

	"github.com/bborbe/agent/agent/pi/pkg/prompts"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
	healthcheck "github.com/bborbe/agent/lib/healthcheck"
	pilib "github.com/bborbe/agent/lib/pi"
)

// ServiceName is the canonical service name for the agent-pi binary.
const ServiceName = "agent-pi"

// CreatePiRunner constructs a Pi Runner pre-configured with tools, model, and env.
func CreatePiRunner(
	agentDir string,
	allowedTools string,
	model string,
	env map[string]string,
) pilib.Runner {
	return pilib.NewRunner(pilib.PiRunnerConfig{
		AgentDir:     agentDir,
		AllowedTools: allowedTools,
		Model:        model,
		Env:          env,
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
	branch base.Branch,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return delivery.NewKafkaResultDeliverer(
		syncProducer,
		branch,
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
