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
	"path/filepath"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/envparse"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"

	"github.com/bborbe/agent-pi/pkg/factory"
	"github.com/bborbe/agent-pi/pkg/providercatalogue"
)

// defaultProviderKeyEnv mirrors main.go's fallback: the local CLI must thread
// the credential under the same name the Kafka entry point does.
const defaultProviderKeyEnv = "MINIMAX_API_KEY"

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
	ProviderAPIKey string `required:"false" arg:"provider-api-key" env:"PROVIDER_API_KEY" usage:"Provider credential, threaded into the pi subprocess under the name in PROVIDER_KEY_ENV" display:"length"`

	// ProviderBaseURL is the endpoint rendered into pi's provider catalogue. Empty
	// keeps pi's own default and writes no catalogue, matching the Kafka entry point.
	ProviderBaseURL string `required:"false" arg:"provider-base-url" env:"PROVIDER_BASE_URL" usage:"Provider endpoint rendered into the pi provider catalogue"`

	// ProviderKeyEnv is the name of the environment variable pi receives the
	// credential under — a name, never the value.
	ProviderKeyEnv string `required:"false" arg:"provider-key-env" env:"PROVIDER_KEY_ENV" usage:"Name of the environment variable pi receives the provider credential under" default:"MINIMAX_API_KEY"`

	// ProviderContextWindow is declared explicitly because pi defaults an omitted
	// value to 128000, which would compact a larger-window model early.
	ProviderContextWindow int `required:"false" arg:"provider-context-window" env:"PROVIDER_CONTEXT_WINDOW" usage:"Context window declared for the model in the pi provider catalogue" default:"128000"`

	// ProviderMaxTokens is the maximum output tokens declared for the model in
	// the catalogue.
	ProviderMaxTokens int `required:"false" arg:"provider-max-tokens" env:"PROVIDER_MAX_TOKENS" usage:"Maximum output tokens declared for the model in the pi provider catalogue" default:"32000"`

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
	if err := a.writeProviderCatalogue(ctx); err != nil {
		return err
	}

	taskContent, err := os.ReadFile(
		a.TaskFilePath,
	) // #nosec G304 -- filePath from trusted CLI input
	if err != nil {
		return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
	}

	deliverer := factory.CreateFileResultDeliverer(a.TaskFilePath)

	// run-task is the local single-task runner, so it is always the job shape. The
	// empty session id is what says so: no identity to resume, and therefore no
	// persistence either — a run leaves nothing behind.
	runner := factory.CreatePiRunner(a.AgentDir, a.AllowedTools, a.Model, a.piEnv(), "")
	agent := factory.CreateAgent(runner, envparse.KeyValuePairs(a.EnvContextRaw))

	result, err := agent.Run(ctx, a.Phase, string(taskContent), deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	return agentlib.PrintResult(ctx, result)
}

// piEnv is the environment this binary overrides on the pi subprocess.
//
// It mirrors main.go's helper rather than sharing one: the two are separate main
// packages, so an unexported symbol cannot cross, and a test here pins that the
// local CLI threads the credential under the same configured name.
func (a *application) piEnv() map[string]string {
	piEnv := map[string]string{}
	if a.ProviderAPIKey != "" {
		keyEnv := a.providerKeyEnv()
		piEnv[keyEnv] = a.ProviderAPIKey
		glog.V(2).Infof("provider credential threaded to pi as %s", keyEnv)
	}
	return piEnv
}

// providerKeyEnv resolves the variable name pi receives the credential under.
//
// The fallback is mandatory: a struct literal built in a test never passes
// through argument.Parse, so ProviderKeyEnv is empty there and an unresolved
// name would hand pi an empty map key.
func (a *application) providerKeyEnv() string {
	if a.ProviderKeyEnv != "" {
		return a.ProviderKeyEnv
	}
	return defaultProviderKeyEnv
}

// providerCatalogueSettings is the provider configuration rendered into pi's
// models.json.
func (a *application) providerCatalogueSettings() providercatalogue.Settings {
	return providercatalogue.Settings{
		ProviderBaseURL: a.ProviderBaseURL,
		ProviderKeyEnv:  a.providerKeyEnv(),
		Model:           a.Model,
		ContextWindow:   a.ProviderContextWindow,
		MaxTokens:       a.ProviderMaxTokens,
	}
}

// writeProviderCatalogue renders pi's provider catalogue into $HOME/.pi/agent,
// the directory pi resolves when PI_CODING_AGENT_DIR is not forwarded.
func (a *application) writeProviderCatalogue(ctx context.Context) error {
	home := os.Getenv("HOME")
	if home == "" {
		return errors.Errorf(
			ctx,
			"HOME must be set to locate the pi agent directory for models.json",
		)
	}
	agentDir := filepath.Join(home, ".pi", "agent")
	if err := providercatalogue.Write(ctx, a.providerCatalogueSettings(), agentDir); err != nil {
		return errors.Wrap(ctx, err, "write pi provider catalogue")
	}
	if a.ProviderBaseURL != "" {
		glog.V(2).Infof(
			"wrote pi provider catalogue endpoint=%s model=%s",
			a.ProviderBaseURL,
			a.Model,
		)
	}
	return nil
}
