# Changelog

All notable changes to this project will be documented in this file.

## v0.5.0

- feat: address a service agent's `/prompt` with a per-request session id, so one pod can hold more than one conversation. The session id was the compiled-in constant `serviceSessionID`, so every caller shared one conversation; it is now resolved per request from an optional `X-Session-Id` header, validated against `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}` **before** the body is read and before any runner is built. A request that sends no header resolves to `serviceSessionID` and reaches the same conversation it always did, so existing callers are untouched. Runners are held one per session id in `sessionRunners`, built lazily on first use — pi's `--session-id` creates a session when it is missing, so an unseen id needs no registration step — and each session carries its own mutex, so two session ids run concurrently while two requests on one id still take turns. That serialization is what protects a session's transcript, a single store on the mounted volume. The id is never logged, and neither is the prompt, so the pod's log cannot be used to enumerate live conversations. The endpoint stays unauthenticated: a session id is an address, not a credential.
- chore: adopt dark-factory in this repo. Adds the pipeline trees (`specs/`, `prompts/`), a project `docs/dod.md` wired as `validationPrompt`, and gitignore entries for the daemon's lock and log. The daemon runs with `workflow: direct`, so it commits to the current branch — an unignored `.dark-factory.lock` and `.dark-factory.log` would otherwise have been swept into a prompt's commit.

## v0.4.2

- fix: give a service agent a session identity, so it actually remembers its conversation. `v0.4.0` wired the runner and `v0.4.1` made it able to read pi's answers, but the agent still forgot everything between prompts: `PersistSession` only omitted `--no-session`, which governs whether pi **writes** the transcript, and reading it back is a different flag. Observed live — turn 1 returned `ACK`, turn 2 returned *"No token was previously requested to be remembered"*, which is exactly what a fresh session says. `runService` now builds its runner with `serviceSessionID`, and `CreatePiRunner` takes the session id as **one** parameter rather than a persist flag plus an id, because persistence without an identity is the defect itself: it writes a transcript nothing ever reads. `--session-id` rather than `--continue` because it **creates the session when it is missing**, so a brand-new agent's first prompt behaves exactly like its thousandth. The task path passes `""` and stays ephemeral, leaving nothing behind on the shared volume.
- chore: update github.com/bborbe/agent to v0.90.4

## v0.4.1

- fix: pin the pi CLI to `0.87.1` and bump `github.com/bborbe/agent` to `v0.90.3` for the parser fix that pin depends on. pi was installed **unpinned**, and its `--mode json` output is a stream of `{"type": ...}` events that the runner parses **by name** — names that have already changed once: pi 0.87.x emits `message_end` where older builds emitted `agent_end`. Installed unpinned, an image rebuild silently adopts whatever vocabulary is current, and the runner then answers `no result found in pi CLI output` on runs that in fact succeeded: a symptom that reads as a model failure and is a parser mismatch. That is not hypothetical — it is exactly what the `v0.4.0` image did on 2026-09-26, and it stayed invisible because the fleet's working agents run older images (`pi-agent` is pinned at `v0.1.7`) while the freshly-built ones had never been prompted. `v0.90.3` teaches the parser the current vocabulary; **this pin is what stops the next rebuild breaking it again**, and the Dockerfile comment records what to check when it is deliberately bumped — a pin with no instructions is a pin someone removes.

## v0.4.0

- feat: serve a prompt intake on a service agent, so the long-running shape can actually be addressed — and build its runner, which is the part that was missing. The service branch returned **before** the runner was constructed, so `CreatePiRunner`'s `persistSession` argument was evaluated as `a.AgentType == factory.AgentTypeService` on a line a service agent can never reach: a **constant `false`** wearing the shape of a live decision. `v0.2.0`'s `PersistSession` flag, its `--no-session` branch and the specs on both sides were therefore correct and **unreachable** — no production path ever set it. A service agent started, served `/readiness`, reported `1/1 Ready` and could not hold a session, which is the one thing it exists to do. `runService` now builds its own runner with persistence **on** and serves `POST /prompt` alongside `/readiness` and `/metrics`: the request body is the prompt, the response body the runner's answer, and it is the **second** request that exercises continuity. **Only a live run could have found this, and even that would not have** — every other defect in this series was reachable and wrong, so running it surfaced the failure; this one was unreachable, so the pod looked entirely healthy. The intake exists because the alternative test — exec'ing `pi` inside the pod — would prove the volume persists while skipping the runner, re-testing the part unit specs already cover and skipping the part that was missing. The endpoint is unauthenticated and reachable only from inside the namespace (no Ingress, dev only); the body is bounded at 1 MiB so one caller cannot exhaust the pod's memory, and **the prompt is never logged** — only its length and a short digest — so the pod's log corroborates which turn ran without becoming a second copy of the conversation. The runner's error is logged rather than returned, since it can carry the command line it ran.

## v0.3.1

- fix: let a service agent start when it has no `TASK_ID`. `v0.3.0` made `TASK_CONTENT` conditional on the agent shape, but `TaskID` was still declared as `agentlib.TaskIdentifier`, whose own `Validate()` rejects an empty value — and the framework runs **type** validation on every field whether or not it is `required`. So a service agent, which has no task and therefore no identifier, still failed at startup: `field TaskID (type lib.TaskIdentifier) validation failed: identifier missing`. **Same class as the `TASK_CONTENT` fix, one field over** — the instance I saw was fixed and the adjacent one of identical shape was missed, which is the recurring shape of this whole deploy: a requirement satisfied in one place and re-asserted in another. The field is now a plain string, converted to `TaskIdentifier` at the point of use, where a non-empty identifier is actually needed. **Only running it could find this**: the failure is in the framework's argument parsing, which no spec in this repo exercises, so a green suite said nothing about it.

## v0.3.0

- feat: run as a service agent instead of exiting without a task. The binary declared `TASK_CONTENT` as `required:"true"`, so a long-running identity agent — which by definition has no task — crashlooped at startup with `parse app failed: validate required failed: Required field empty, define parameter task-content or define env TASK_CONTENT`. The requirement is now enforced in `Run` rather than by the struct tag, because the tag is static and cannot be conditional: a task-routed agent still fails fast on an empty `TASK_CONTENT`, while `AGENT_TYPE=service` stays alive and serves `/readiness` and `/metrics` through the same `service.Run` + `libhttp.NewServer` pair the framework's own example uses. The readiness check **dials** `PROVIDER_BASE_URL` rather than calling the provider — the question a probe asks is whether the provider is *reachable*, and a dial answers it without spending a request or needing a valid key. That is what makes it falsifiable: a liveness check would pass against an unreachable provider too, so pointing `PROVIDER_BASE_URL` at an unroutable address must turn the probe red or the probe proves nothing. With `PROVIDER_BASE_URL` unset the pi CLI's own default applies, which this binary cannot know, so the check is skipped and says so rather than guessing. Found on a live cluster: the service agent's pod had every layer below the binary correct — image, `AGENT_TYPE`, bound volume, scheduling — and still could not start.

## v0.2.0

- feat: keep pi's session storage for a service agent, driven by the executor's `AGENT_TYPE` env — a long-running identity agent needs continuity across prompts, while a task-routed agent must not persist: `pi-agent` mounts a shared volume at `/home/pi/.pi`, so a persisted session would let a later run resume an unrelated earlier task's conversation. `CreatePiRunner` takes the flag and passes it to `PiRunnerConfig.PersistSession`, which defaults false, so every existing agent behaves exactly as before. `AGENT_TYPE` is stamped by the executor from the Config's `spec.type` rather than hand-written into the CR; `cmd/run-task` is the local single-task runner and always passes false.
- chore: update github.com/bborbe/agent to v0.90.0

## v0.1.14

- chore: update github.com/bborbe/agent to v0.87.5, github.com/bborbe/kafka to v1.25.16

## v0.1.13

- chore: update github.com/bborbe/agent to v0.87.4, github.com/bborbe/errors to v1.6.1, github.com/bborbe/kafka to v1.25.15, github.com/bborbe/service to v1.10.13, github.com/bborbe/time to v1.27.14, github.com/bborbe/vault-cli to v0.126.3

## v0.1.12

- fix: `make build` refuses to stamp a version onto a tree that is not that version's tag (`check-version-tag`, escape hatch `ALLOW_UNTAGGED_BUILD=1`). `VERSION` defaults to the newest tag repo-wide, so an operator-run build from an untagged or older tree silently republishes under the newest tag. The guard compares `git describe --exact-match HEAD` against `$(VERSION)` and exits non-zero on mismatch.

## v0.1.11

- chore: update Go to 1.27.1 and github.com/bborbe/agent to v0.87.1, github.com/bborbe/kafka to v1.25.12, github.com/bborbe/service to v1.10.12, github.com/bborbe/vault-cli to v0.122.0

## v0.1.10

- fix: update `golang.org/x/crypto` to v0.56.0 — clears the vulnerability gate blocking this repo's CI
- fix: `Dockerfile` `ARG DOCKER_REGISTRY` default now points at `docker.prod.nuke.benjamin-borbe.de:443` instead of the decommissioned `docker.quant.benjamin-borbe.de:443`. Inert in CI (which passes `DOCKER_REGISTRY` explicitly) but a bare local `docker build` silently targeted a dead host

## v0.1.9

- chore: update github.com/bborbe/agent to v0.87.0, github.com/bborbe/cqrs to v0.6.10, github.com/bborbe/kafka to v1.25.11, github.com/bborbe/sentry to v1.10.1, github.com/bborbe/service to v1.10.11, github.com/bborbe/time to v1.27.12, github.com/bborbe/vault-cli to v0.121.3

## v0.1.8

- chore: update github.com/bborbe/agent to v0.85.0, github.com/bborbe/cqrs to v0.6.9, github.com/bborbe/errors to v1.6.0, github.com/bborbe/kafka to v1.25.10, github.com/bborbe/sentry to v1.10.0, github.com/bborbe/service to v1.10.10, github.com/bborbe/time to v1.27.11, github.com/bborbe/vault-cli to v0.118.4, github.com/onsi/gomega to v1.43.0

## v0.1.7

- chore: update Go to 1.27.0 and github.com/bborbe/agent to v0.83.1, github.com/bborbe/cqrs to v0.6.8, github.com/bborbe/errors to v1.5.21, github.com/bborbe/kafka to v1.25.9, github.com/bborbe/sentry to v1.9.27, github.com/bborbe/service to v1.10.9, github.com/bborbe/time to v1.27.10, github.com/bborbe/vault-cli to v0.116.2

## v0.1.6

- chore: Bump errcheck to v1.20.0 and golangci-lint to v2.13.1 for Go 1.27 support

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.1.5

- chore: update Go to 1.26.6 and update dependencies
## v0.1.4

- chore: update Go to 1.26.6 and dependencies (golang.org/x/mod to v0.40.0 to fix GO-2026-6179, GO-2026-6180)

## v0.1.3

- docs: add a License section to the README

## v0.1.2

- chore: bump Go 1.26.4 → 1.26.5, alpine 3.23 → 3.24, and bborbe deps (agent v0.72.0 → v0.77.1, vault-cli v0.68.0 → v0.101.1, cqrs, kafka, service, time, sentry, errors)
- security: exclude no-fix advisory GO-2026-5932 (golang.org/x/crypto/openpgp unmaintained) via VULNCHECK_IGNORE + .trivyignore + .osv-scanner.toml
- security: exclude containerd v1 no-fix advisories GO-2026-5064/5338/5622 (only v2 patched; CRI checkpoint-restore unreachable, indirect dep) via .osv-scanner.toml

## v0.1.1

- refactor: converge build to bborbe/kafka-topic-reader publish-only model — make buca publishes docker.io/bborbe/agent-pi:$(VERSION); deploy machinery removed.

## v0.1.0

- feat: adopt cqrs v0.6.0 / agent v0.72.0 explicit `base.TopicPrefix`; add optional `TopicPrefix` config (`env TOPIC_PREFIX`) for Kafka result topic naming — empty means unprefixed topics (Octopus per-stage clusters), non-empty preserves `develop`/`master` names (quant)
- chore: bump `github.com/bborbe/agent` v0.70.0 → v0.72.0, `github.com/bborbe/cqrs` v0.5.2 → v0.6.0
