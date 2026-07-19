# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

- chore: bump Go 1.26.4 → 1.26.5, alpine 3.23 → 3.24, and bborbe deps (agent v0.72.0 → v0.77.1, vault-cli v0.68.0 → v0.101.1, cqrs, kafka, service, time, sentry, errors)
- security: exclude no-fix advisory GO-2026-5932 (golang.org/x/crypto/openpgp unmaintained) via VULNCHECK_IGNORE + .trivyignore + .osv-scanner.toml
- security: exclude containerd v1 no-fix advisories GO-2026-5064/5338/5622 (only v2 patched; CRI checkpoint-restore unreachable, indirect dep) via .osv-scanner.toml

## v0.1.1

- refactor: converge build to bborbe/kafka-topic-reader publish-only model — make buca publishes docker.io/bborbe/agent-pi:$(VERSION); deploy machinery removed.

## v0.1.0

- feat: adopt cqrs v0.6.0 / agent v0.72.0 explicit `base.TopicPrefix`; add optional `TopicPrefix` config (`env TOPIC_PREFIX`) for Kafka result topic naming — empty means unprefixed topics (Octopus per-stage clusters), non-empty preserves `develop`/`master` names (quant)
- chore: bump `github.com/bborbe/agent` v0.70.0 → v0.72.0, `github.com/bborbe/cqrs` v0.5.2 → v0.6.0
