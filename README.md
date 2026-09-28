# Agent Pi

Generic, domain-agnostic Pi CLI runner. Receives a task from the agent pipeline, spawns the `pi` CLI (from `@earendil-works/pi-coding-agent`) with configurable tools and instructions, and returns a structured JSON result.

New agents are created by swapping instructions (agent `AGENTS.md` guardrails) and `ALLOWED_TOOLS` — no Go code changes needed.

## How It Works

1. Agent pipeline ([[task/controller]] → Kafka → [[task/executor]]) spawns a K8s Job with the `agent-pi` image.
2. The Job receives `TASK_CONTENT`, `TASK_ID`, `BRANCH`, `ALLOWED_TOOLS`, `MODEL`, `PHASE`, etc. via env vars.
3. `main.go` assembles the prompt via `lib/pi` (embedded `workflow.md` + `output-format.md` + task content).
4. Runs `pi --print --mode json` with the allowed tools and selected model — adding `--no-session` unless the agent is a service agent, whose sessions must persist (see below).
5. Parses the JSON result and publishes to Kafka via `lib/delivery.KafkaResultDeliverer` (when `TASK_ID` set), or falls back to `NoopResultDeliverer` for local runs.

## Service Agents

A Config with `spec.type: service` is a long-running identity rather than a task runner: the executor stamps `AGENT_TYPE=service`, and the binary then stays alive instead of running one task and exiting. It serves three endpoints on `LISTEN` (default `:9090`):

| Endpoint | Method | Purpose |
|---|---|---|
| `/readiness` | GET | 200 when the provider is reachable, 503 when it is not (or unparseable); skipped, and said to be skipped, when `PROVIDER_BASE_URL` is unset |
| `/metrics` | GET | Prometheus registry |
| `/prompt` | POST | Runs one prompt through the Pi runner and returns its answer as plain text |

`/prompt` is what makes the shape addressable, and it is the seam a chat transport would use. Its body is the prompt and its response is the runner's result; the handler itself persists nothing. Session continuity belongs to the runner, via `PiRunnerConfig.PersistSession`, which is on for a service agent and off for a task-routed one — a task-routed agent's runs are unrelated tasks sharing one volume, so resuming one task's conversation inside another would be a defect rather than a feature.

### Choosing a session

`X-Session-Id` is an optional request header on `/prompt` naming the conversation the prompt belongs to. A request that omits it is served from the default session, exactly as it was before the header existed, so an existing caller needs no change. A request that sends it is served from its own conversation, created on first use.

The accepted format is `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}` — one to 64 characters, the first of which is not `-`. Anything else, including a present-but-empty value, is answered `400` before the agent process is invoked. The format is a security boundary rather than a style preference: the id reaches the `pi` CLI as a command-line argument, where a leading `-` would be read as a flag.

Requests on different session ids run at the same time. Requests on one session id are serialized — one turn at a time — because a session's transcript is a single store on the mounted volume, and two interleaved runs there would corrupt the continuity the session exists to keep.

A session id is an address, not a credential: the header does not isolate a caller from anyone else who can reach the endpoint, because the endpoint is unauthenticated. The service cannot list, rename, or delete sessions.

The endpoint is unauthenticated and reachable only from inside the namespace (there is no Ingress). The request body is bounded at 1 MiB, and neither the prompt nor the session id is ever logged — only the prompt's length and a short digest.

## Env Vars

| Var | Required | Default | Purpose |
|---|---|---|---|
| `TASK_CONTENT` | yes | — | Raw task markdown |
| `BRANCH` | yes | — | `dev`/`prod` — used as Kafka topic prefix |
| `TASK_ID` | no | — | Required when publishing results via Kafka |
| `MODEL` | no | `MiniMax-M2.7-highspeed` | Model name passed to `pi --model` |
| `ALLOWED_TOOLS` | no | — | Comma-separated pi tool allowlist (e.g. `Read,Grep,Bash`) |
| `AGENT_DIR` | no | `agent` | Directory containing `AGENTS.md` guardrails (used as pi cwd) |
| `PROVIDER_API_KEY` | no | — | MiniMax API key, threaded into the pi subprocess as `MINIMAX_API_KEY` |
| `ENV_CONTEXT` | no | — | Comma-separated `KEY=VAL` pairs injected into the prompt |
| `PHASE` | no | `execution` | Agent phase: `planning` \| `execution` \| `ai_review` |
| `KAFKA_BROKERS` | no | — | Required when `TASK_ID` is set |
| `SENTRY_DSN` | no | — | Error reporting |
| `PUSHGATEWAY_URL` | no | `http://pushgateway:9090` | Prometheus PushGateway URL |
| `TASK_TYPE` | no | `unknown` | Task type label for metric grouping |

Pi's own config (auth state, sessions) lives in `$HOME/.pi`. The K8s manifest mounts a PVC at `/home/pi/.pi`; locally pi resolves it from your home dir. There is no `CLAUDE_CONFIG_DIR` equivalent — pi does not have OAuth, it uses `MINIMAX_API_KEY`.

## Creating a New Agent

To add a domain-specific agent that reuses this binary:

1. Create a task file in the OpenClaw vault with `assignee: pi-agent` (or a new assignee routed to this image via a Config CRD).
2. Mount a PVC or Secret containing the domain-specific `AGENTS.md` guardrails and any API credentials.
3. Set `ALLOWED_TOOLS` on the Config CRD to the minimum tools the agent needs.
4. Set `ENV_CONTEXT` to inject domain context (e.g. API URLs) into the prompt without modifying the binary.

### Config CRD env pattern

The `Config` CRD's `spec.env` map becomes pod env vars, which `main.go` consumes via struct tags. Example from `k8s/agent-pi.yaml`:

```yaml
spec:
  env:
    ALLOWED_TOOLS: Read,Bash,Grep,Glob,Write,Edit
    MODEL: MiniMax-M2.7-highspeed
```

Tune `ALLOWED_TOOLS` per task shape (minimum viable set):

| Task shape | Minimum tools |
|---|---|
| Web research | `WebSearch,WebFetch,Read,Grep` |
| Vault I/O via scripts | `Bash(scripts/vault-read.sh:*),Bash(scripts/vault-write.sh:*),Bash(scripts/vault-list.sh:*),Grep` |
| API query via script | `Bash(scripts/trading-api-read.sh:*),Grep` |
| Code edit | `Read,Write,Edit,Grep,Glob,Bash(go:*),Bash(make:*)` |

Prefer constrained `Bash(path:*)` forms over bare `Bash` to minimize shell attack surface.

### Pi subprocess env allowlist

`lib/pi/pi-runner.go` strips pod env down to a safe allowlist (`HOME,PATH,USER,TZ,…` plus known provider API keys: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `MINIMAX_API_KEY`, `GEMINI_API_KEY`, `PROVIDER_API_KEY`, …) before spawning `pi`. Custom env vars beyond that list **must** be threaded explicitly via `PiRunnerConfig.Env map[string]string` in `main.go`. Don't expect arbitrary pod env to reach pi by default.

## Local Quick Test

```bash
cd ~/Documents/workspaces/agent/agent/pi
go run . \
  --task-content "$(cat /path/to/task.md)" \
  --model MiniMax-M2.7-highspeed \
  --allowed-tools "Read,Write,Edit,Bash,Grep,Glob" \
  --agent-dir agent \
  --branch dev
```

Skips K8s, task controller, task executor, git writeback. Useful for iterating on prompts.

For a file-based local run (reads the task from disk, writes the result back), see `cmd/run-task/`.

## Links

Admin endpoints:
- Dev: <https://dev.quant.benjamin-borbe.de/admin/agent-pi/setloglevel/3>
- Prod: <https://prod.quant.benjamin-borbe.de/admin/agent-pi/setloglevel/3>

## Related

- `pkg/prompts/` — embedded prompts (`workflow.md`, `output-format.md`)
- `agent/AGENTS.md` — default agent guardrails
- `lib/pi/` — shared prompt assembly + pi CLI invocation
- `lib/delivery/` — shared Kafka result publishing
- `task/controller/` — Obsidian→Kafka event source
- `task/executor/` — Kafka→K8s Job spawner

## License

This project is licensed under the BSD-style license. See the [LICENSE](LICENSE) file for details.
