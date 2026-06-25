# Agent Pi

Generic, domain-agnostic Pi CLI runner. Receives a task from the agent pipeline, spawns the `pi` CLI (from `@earendil-works/pi-coding-agent`) with configurable tools and instructions, and returns a structured JSON result.

New agents are created by swapping instructions (agent `AGENTS.md` guardrails) and `ALLOWED_TOOLS` — no Go code changes needed.

## How It Works

1. Agent pipeline ([[task/controller]] → Kafka → [[task/executor]]) spawns a K8s Job with the `agent-pi` image.
2. The Job receives `TASK_CONTENT`, `TASK_ID`, `BRANCH`, `ALLOWED_TOOLS`, `MODEL`, `PHASE`, etc. via env vars.
3. `main.go` assembles the prompt via `lib/pi` (embedded `workflow.md` + `output-format.md` + task content).
4. Runs `pi --print --mode json --no-session` with the allowed tools and selected model.
5. Parses the JSON result and publishes to Kafka via `lib/delivery.KafkaResultDeliverer` (when `TASK_ID` set), or falls back to `NoopResultDeliverer` for local runs.

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
