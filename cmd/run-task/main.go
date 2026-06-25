// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command run-task is the local-CLI entry point for agent-pi.
//
// Reads a markdown task file from disk, runs the agent against it, and
// writes the updated content back to the same file. Mirrors the Kafka
// entry point (../../main.go) but uses file I/O instead of Kafka/CQRS.
package main

import (
	"context"
	"os"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent/agent/pi/pkg/factory"
	agentlib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/envparse"
)

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

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Provider routing.
	ProviderAPIKey string `required:"false" arg:"provider-api-key" env:"PROVIDER_API_KEY" usage:"MiniMax API key passed to pi CLI as MINIMAX_API_KEY" display:"length"`

	// Model selection.
	Model string `required:"false" arg:"model" env:"MODEL" usage:"Model name" default:"MiniMax-M2.7-highspeed"`

	// Environment
	Branch base.Branch `required:"true" arg:"branch" env:"BRANCH" usage:"branch" default:"dev"`

	// Phase to run (defaults to execution; framework requires explicit phase)
	Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`

	// Task file for local development
	TaskFilePath string `required:"true" arg:"task-file" env:"TASK_FILE" usage:"Path to the markdown task file"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	taskContent, err := os.ReadFile(
		a.TaskFilePath,
	) // #nosec G304 -- filePath from trusted CLI input
	if err != nil {
		return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
	}

	deliverer := factory.CreateFileResultDeliverer(a.TaskFilePath)

	piEnv := map[string]string{}
	if a.ProviderAPIKey != "" {
		piEnv["MINIMAX_API_KEY"] = a.ProviderAPIKey
	}

	runner := factory.CreatePiRunner(a.AgentDir, a.AllowedTools, a.Model, piEnv)
	agent := factory.CreateAgent(runner, envparse.KeyValuePairs(a.EnvContextRaw))

	result, err := agent.Run(ctx, a.Phase, string(taskContent), deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	return agentlib.PrintResult(ctx, result)
}
