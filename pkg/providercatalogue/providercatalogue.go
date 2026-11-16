// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package providercatalogue renders the pi CLI's provider catalogue.
//
// pi 0.87.1 has no environment variable for a provider base URL; the only route
// to a non-built-in endpoint is <agent-dir>/models.json. This package exists so
// the two entry points (main.go and cmd/run-task/main.go) share one renderer
// instead of each carrying a copy that can drift, and so the file the CLI parses
// is covered by tests that assert its shape rather than its substrings.
package providercatalogue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/bborbe/errors"
)

// providerID and providerAPI are constants rather than settings: no caller
// varies them, and a knob that never changes is surface the next reader has to
// reason about for nothing.
const (
	providerID  = "custom"
	providerAPI = "anthropic-messages"
)

// Settings is the provider configuration rendered into the catalogue.
//
// ProviderKeyEnv carries the *name* of the environment variable pi interpolates,
// never the credential itself: pi resolves "$NAME" from its own environment at
// request time, so writing the value into the file would put the secret on disk
// for no gain.
type Settings struct {
	ProviderBaseURL string
	ProviderKeyEnv  string
	Model           string
	ContextWindow   int
	MaxTokens       int
}

type catalogue struct {
	Providers map[string]provider `json:"providers"`
}

type provider struct {
	BaseURL string  `json:"baseUrl"`
	API     string  `json:"api"`
	APIKey  string  `json:"apiKey"`
	Models  []model `json:"models"`
}

type model struct {
	ID            string   `json:"id"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
}

// Models renders the catalogue bytes for the configured provider.
//
// It returns nil when no base URL is configured, which is the signal that the
// pi CLI's own default provider configuration applies and no catalogue should be
// written. contextWindow is declared explicitly because pi silently falls back
// to 128000 when it is omitted, so a larger-window model would compact early
// with no error.
func Models(settings Settings) ([]byte, error) {
	if settings.ProviderBaseURL == "" {
		return nil, nil
	}
	data, err := json.MarshalIndent(catalogue{
		Providers: map[string]provider{
			providerID: {
				BaseURL: settings.ProviderBaseURL,
				API:     providerAPI,
				APIKey:  "$" + settings.ProviderKeyEnv,
				Models: []model{
					{
						ID:            settings.Model,
						Reasoning:     true,
						Input:         []string{"text"},
						ContextWindow: settings.ContextWindow,
						MaxTokens:     settings.MaxTokens,
					},
				},
			},
		},
	}, "", "  ")
	if err != nil {
		return nil, errors.Wrap(context.Background(), err, "marshal provider catalogue")
	}
	return data, nil
}

// Write renders the catalogue into <agentDir>/models.json.
//
// It writes nothing when no base URL is configured, and in particular leaves an
// existing models.json untouched: an unconfigured agent must keep behaving
// exactly as it did before this package existed. The directory is created on
// demand because the pi agent directory may not exist yet on a fresh volume.
func Write(settings Settings, agentDir string) error {
	data, err := Models(settings)
	if err != nil {
		return errors.Wrap(context.Background(), err, "render provider catalogue")
	}
	if data == nil {
		return nil
	}
	if err := os.MkdirAll(agentDir, 0750); err != nil {
		return errors.Wrapf(context.Background(), err, "create pi agent dir %s", agentDir)
	}
	path := filepath.Join(agentDir, "models.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return errors.Wrapf(context.Background(), err, "write %s", path)
	}
	return nil
}
