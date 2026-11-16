---
status: committing
summary: Added configurable provider endpoint settings, rendered a pi models.json catalogue from a shared package used by both entry points, with tests and docs
execution_id: agent-pi-provider-endpoint-exec-006-provider-base-url
dark-factory-version: v0.198.17
created: "2026-11-16T18:30:00Z"
queued: "2026-11-16T17:59:05Z"
started: "2026-11-16T18:51:17Z"
completed: "2026-11-16T18:59:06Z"
---

# Point the pi CLI at a configurable provider endpoint

<summary>
- The agent can be pointed at a model endpoint that is not MiniMax, by configuration alone.
- New settings name the endpoint, the environment variable that carries its credential, and the model's context and output-token limits.
- When an endpoint is configured, the agent writes the pi CLI's provider catalogue before spawning it, so pi resolves the configured model instead of its built-in MiniMax default.
- An operator who configures nothing sees exactly today's behaviour: the MiniMax key name, no catalogue written, no default changed.
- The change is covered by tests that fail if the credential variable name is ignored or if a catalogue is written with no endpoint configured.
- The change is recorded in the changelog so the next release carries it.
- No deployment is changed by this prompt; pointing the live agents at the new endpoint is a separate values change.
</summary>

<objective>
Let `agent-pi` talk to any Anthropic-Messages-compatible endpoint — the fleet's in-cluster model router in particular — by taking the endpoint URL and the name of the environment variable that carries its credential, so the agent survives the MiniMax subscription's expiry on 2026-11-16 without a code change per provider. Today the credential is hardcoded into the pi subprocess as `MINIMAX_API_KEY` and pi has no way to learn a different base URL at all, so the agent can only ever reach MiniMax. This change is reasoned from the pinned pi package rather than inherited from a spec; there is no spec to link.

</objective>

<context>
Read `docs/dod.md` for the repo's definition of done and `README.md` for the documented env and route contract. (There is no tracked `CLAUDE.md` in this repo — the untracked one in the main checkout is not present in a worktree and is not part of the contract.)

Read these files in full before editing:

- `main.go` — the Kafka entry point. `application` is the config struct; every field's struct tag is how the executor's Config CRD env reaches it. `piEnv()` builds the environment the pi subprocess receives and today hardcodes the literal `MINIMAX_API_KEY`. `Run` branches into `runService` for a `type: service` agent and into a task runner otherwise, and both paths obtain their pi environment from `piEnv()`. `ProviderBaseURL` already exists but is used only as the address a service agent dials for its readiness probe — its doc comment claiming it is "passed through to the subprocess unchanged" is stale and must be corrected.
- `cmd/run-task/main.go` — the local CLI entry point. It duplicates the provider fields and the `MINIMAX_API_KEY` literal inline in `Run`; it must stay in step with `main.go`.
- `main_internal_test.go` — `package main`, the unexported-symbol tests. Its existing `piEnv` spec constructs `&application{ProviderAPIKey: "secret"}` directly, which is why a struct-tag default never applies there (see requirement 2).
- `main_test.go` — `package main_test`, the suite entry point (`RunSpecs`). It prints `Ran 7 of 7 Specs` today; that count is the baseline for verification.
- `cmd/run-task/main_test.go` — the sibling suite (`Run-Task Suite`) that will discover a `package main` test file in `cmd/run-task/`.
- `pkg/factory/factory.go` — `CreatePiRunner` and `CreatePiSessionFactory` pass the `env map[string]string` straight into `pilib.PiRunnerConfig.Env`, which the library appends to the pi subprocess environment.
- `README.md` — the `## Env Vars` table, the `PROVIDER_API_KEY` row (which claims the value is always threaded as `MINIMAX_API_KEY`), and the paragraph saying pi "does not have OAuth, it uses `MINIMAX_API_KEY`". All must describe the new settings.
- `CHANGELOG.md` — the highest released heading is `## v0.8.1` and there is no `## Unreleased` section today; the new entry goes under a new one.

Verified facts about the pi CLI, read from the **pinned 0.87.1 package itself** (`npm pack @earendil-works/pi-coding-agent@0.87.1`, then reading its `dist/` and `docs/`) — not from the newer `1.5.2` binary on a developer machine, whose provider surface differs:

- pi 0.87.1 has **no** environment variable for a provider base URL (`grep -rho '[A-Z][A-Z0-9_]*_BASE_URL' dist/` yields only `DEFAULT_CATALOG_BASE_URL`). Its only supported route to a non-built-in endpoint is `<agent-dir>/models.json`.
- `<agent-dir>` is `$PI_CODING_AGENT_DIR` when set and `~/.pi/agent` otherwise — **but the shared `bborbe/agent` library does not forward `PI_CODING_AGENT_DIR` to the pi subprocess** (`pi/pi-runner.go`'s allowlist carries `HOME, PATH, USER, TZ, ZONEINFO, TMPDIR, LANG, LC_ALL` plus provider key names and `PiRunnerConfig.Env`). `HOME` *is* forwarded. So the catalogue must be written to `$HOME/.pi/agent`, which is the directory pi actually resolves here; writing to `$PI_CODING_AGENT_DIR` would put the file somewhere pi never looks and every test would still pass.
- In `models.json`, `apiKey` supports `$NAME` / `${NAME}` environment interpolation, but `baseUrl` is taken literally (`resolve-config-value.js` is applied to keys and headers only). That is why the base URL must be rendered into the file at startup rather than referenced by name.
- A provider entry is `{"providers": {"<name>": {"baseUrl": "...", "api": "anthropic-messages", "apiKey": "$NAME", "models": [{"id": "..."}]}}}`. `api: "anthropic-messages"` with `baseUrl` + `apiKey: "$KEY"` is the documented custom-provider shape in 0.87.1 (`dist/core/extensions/types.d.ts`, `docs/custom-provider.md`). `--model <id>` matches an exact lowercase model id (`dist/core/model-resolver.js` `findExactModelReferenceMatch`), so no `--provider` flag is needed and the library needs no change.
- A catalogue entry that omits `contextWindow` gets **128000** (`provider-composer.js`: `contextWindow ?? 128000`), so a larger-window model compacts early with no error. Declare it.
- The exact shape was exercised end to end against a live Anthropic-Messages router before this prompt was written: a provider with `api: anthropic-messages`, `apiKey: "$TEST_ROUTER_KEY"`, one model id, and `pi --print --model <that id>` answered correctly.

The fleet's in-cluster router serves Anthropic Messages at `http://claude-code-router-<stage>.agent.svc.cluster.local:8788` and currently requires no inbound credential — but a non-empty value must still be supplied, because pi resolves the interpolated key from the environment.

</context>

<requirements>

1. **Add the provider settings to `main.go`'s `application` struct**, next to the existing `ProviderAPIKey` / `Model` fields, each with the repo's existing struct-tag shape (`required:"false"`, `arg:`, `env:`, `usage:`, and a `default:`):

   | Field | Env | Arg | Default | Purpose |
   |---|---|---|---|---|
   | `ProviderKeyEnv` | `PROVIDER_KEY_ENV` | `provider-key-env` | `MINIMAX_API_KEY` | Name of the environment variable pi receives the credential under |
   | `ProviderContextWindow` | `PROVIDER_CONTEXT_WINDOW` | `provider-context-window` | `128000` | Context window declared for the model in the catalogue |
   | `ProviderMaxTokens` | `PROVIDER_MAX_TOKENS` | `provider-max-tokens` | `32000` | Max output tokens declared for the model in the catalogue |

   The provider id and wire protocol are **not** settings: use the constants `"custom"` and `"anthropic-messages"` in the catalogue package. Neither has a second consumer, and a knob no caller varies is surface the next reader has to reason about for nothing.

   Keep `ProviderAPIKey`'s `display:"length"` (the credential must never be printed). Update its `usage` string so it no longer claims the value is always passed as `MINIMAX_API_KEY`. Also correct `ProviderBaseURL`'s doc comment and `usage`: it is not "passed through to the subprocess unchanged" — it is dialled for the readiness probe and, after this change, rendered into the catalogue file.

2. **Resolve the credential's variable name with a fallback, and use it in `piEnv()`.** Replace the hardcoded map key with `a.ProviderKeyEnv` when non-empty and a package-level `defaultProviderKeyEnv = "MINIMAX_API_KEY"` constant otherwise. The fallback is mandatory, not defensive: `main_internal_test.go` builds `&application{ProviderAPIKey: "secret"}` directly, so the struct tag's `default:` is never applied in tests (that is `argument.Parse`'s job), and a bare `a.ProviderKeyEnv` would produce the map key `""` and fail the existing spec. Keep the existing guard that leaves the map empty when `ProviderAPIKey` is empty.

3. **Extract the catalogue to a new package `pkg/providercatalogue`.** `main.go` and `cmd/run-task/main.go` are separate `main` packages, so unexported symbols cannot cross; one implementation with one test beats two that can drift. Do not put it in `pkg/factory/` (wiring only). The package exposes:

   - `Settings` — `ProviderBaseURL`, `ProviderKeyEnv`, `Model` (string) and `ContextWindow`, `MaxTokens` (int).
   - `Models(settings Settings) ([]byte, error)` — returns the catalogue bytes, or `nil` when `ProviderBaseURL` is empty. It renders exactly:

     ```json
     {
       "providers": {
         "custom": {
           "baseUrl": "<ProviderBaseURL>",
           "api": "anthropic-messages",
           "apiKey": "$<ProviderKeyEnv>",
           "models": [
             {"id": "<Model>", "reasoning": true, "input": ["text"], "contextWindow": <ContextWindow>, "maxTokens": <MaxTokens>}
           ]
         }
       }
     }
     ```

     `apiKey` carries the literal `$` followed by the variable *name* — pi interpolates it. The credential itself never enters the file.
   - `Write(settings Settings, agentDir string) error` — writes those bytes to `<agentDir>/models.json`, creating the directory if absent. When `Models` returns `nil` (no base URL) it writes nothing and does not touch an existing file.
   - Both functions carry a GoDoc comment in the style of `piEnv` in `main.go` — why it exists, not what the signature says.

4. **Call it from both entry points.** In `main.go` and in `cmd/run-task/main.go`, resolve the agent directory as `$HOME/.pi/agent` (`path/filepath`), and call `providercatalogue.Write` with the resolved settings before the pi runner is constructed — in `main.go` at the top of `Run`, before the service/task branch, so both shapes get it. When `$HOME` is empty, return a wrapped error naming the missing variable rather than skipping silently. Wrap every error with `github.com/bborbe/errors` (`errors.Wrap` / `errors.Wrapf`), never `fmt.Errorf`, and never a bare `return err`. Log one `glog.V(2)` line naming the endpoint and the model when the catalogue is written, and the credential's variable name (never its value) when it is threaded through. Do not change `cmd/run-task`'s required flags or its default `MODEL`.

5. **Add tests that fail without the change.**

   - `pkg/providercatalogue/providercatalogue_suite_test.go` + `providercatalogue_test.go` — a Ginkgo v2 / Gomega suite in `package providercatalogue_test`, mirroring `pkg/factory/factory_suite_test.go` (`time.Local = time.UTC`, `format.TruncatedDiff = false`, `RegisterFailHandler(Fail)`, `RunSpecs`). A package without its own `RunSpecs` has its specs silently not discovered and `make test` still exits 0.
   - `Models` returns `nil` and no error when `ProviderBaseURL` is empty.
   - `Models` with a base URL, key env name, model, context window and max tokens set returns bytes that **unmarshal** into the expected structure — assert through `encoding/json` on a typed or `map[string]any` value, not by substring-matching the raw bytes — and that `providers.custom.apiKey` equals `$<ProviderKeyEnv>` rather than the credential, and that `contextWindow` / `maxTokens` carry the configured integers.
   - `Write` writes `models.json` under the given directory with the same bytes `Models` returns, and writes **nothing** (leaving a pre-existing file untouched) when there is no base URL. Use `os.MkdirTemp` with a deferred `os.RemoveAll`.
   - In `main_internal_test.go` (`package main`): `piEnv()` puts the key under `ProviderKeyEnv` when it is set to something other than `MINIMAX_API_KEY` — this is the assertion that fails on today's hardcoded literal — and still uses `MINIMAX_API_KEY` when `ProviderKeyEnv` is empty (the state a struct literal produces; the tag default only applies through `argument.Parse`), pinning the fallback constant.
   - The `cmd/run-task` mirror is covered too: add `cmd/run-task/main_internal_test.go` (`package main` — the existing `Run-Task Suite` in `main_test.go` discovers it) asserting the credential variable name that the mirror passes to `CreatePiRunner`.

   These are boundary tests: the JSON is consumed by a third-party CLI whose parser is the contract, so a shape-only assertion is not enough.

6. **Update `README.md`.** Add `PROVIDER_KEY_ENV`, `PROVIDER_CONTEXT_WINDOW`, `PROVIDER_MAX_TOKENS` and `PROVIDER_BASE_URL` to the `## Env Vars` table; correct the `PROVIDER_API_KEY` row, which no longer means "threaded as `MINIMAX_API_KEY`" but is threaded under `PROVIDER_KEY_ENV`; and correct the sentence claiming pi "does not have OAuth, it uses `MINIMAX_API_KEY`" so it states that the credential's variable name is configurable and that a non-MiniMax endpoint is reached through a `models.json` the binary renders at startup. Keep the `## Pi subprocess env allowlist` section truthful: `PiRunnerConfig.Env` is what threads the credential, and the allowlist's `PROVIDER_API_KEY` entry is unrelated to this mechanism.

7. **Add a `## Unreleased` heading and one bullet under it** in `CHANGELOG.md`, above the highest released heading (`## v0.8.1`). Conventional prefix (`feat:`), and the bullet must contain the literal `PROVIDER_BASE_URL` so the verification grep is deterministic.

8. **Self-check before finishing.** Run every command in `<verification>` and quote its actual output. Then walk each numbered requirement above against the diff, one by one.

</requirements>

<constraints>

- Do NOT commit — dark-factory handles git.
- Do NOT change any existing default: `MODEL` stays `MiniMax-M2.7-highspeed`, and an unconfigured agent keeps sending the credential as `MINIMAX_API_KEY` with no catalogue file written.
- Do NOT write the credential value into `models.json` or into any log line. Only the variable *name* may be logged.
- Do NOT touch `pkg/factory/` or the `bborbe/agent` library — the library needs no change for this, and its vendored copy is not in scope.
- Do NOT add a dependency; `encoding/json`, `os` and `path/filepath` are standard library.
- Do NOT change `cmd/run-task`'s required flags, its `BRANCH` default, or its task-file behaviour.
- Do NOT edit any file under `agent/` — the guardrails directory is content, not code.
- Do NOT rename `## Unreleased` and do not create a version tag.
- Do NOT add a new required environment variable; every new setting has a working default.
- Existing tests must still pass.
- No daemon artifacts, editor files, or scratch output are committed.

</constraints>

<verification>

Run each command. All must pass; quote the actual output of each in the completion report.

1. Full test suite. Do **not** pipe this — a pipeline reports its last stage's status, so `| tail` would report success on a total failure:

   ```bash
   go test ./... -count=1
   ```

   Expected: exits 0 and prints one `ok`/`no test files` line per package, including `github.com/bborbe/agent-pi`, `.../pkg/factory` and `.../pkg/providercatalogue`.

2. Every suite actually ran, and the new specs are among them. Plain `-v` prints no spec names, so count specs; `-ginkgo.v` is the flag that prints the tree if you want names:

   ```bash
   go test ./... -count=1 -v 2>&1 | grep -oE 'Ran [0-9]+ of [0-9]+ Specs'
   ```

   Expected: one `Ran N of N Specs` line per suite — `main` above its pre-change baseline of 7, plus `Factory Suite`, `Prompts Suite`, `Run-Task Suite` and the new `Providercatalogue Suite`. A suite with no line means it was not discovered.

3. The credential name is no longer a hardcoded map literal. Read the lines; do not just check the exit code:

   ```bash
   grep -n 'MINIMAX_API_KEY' main.go cmd/run-task/main.go pkg/providercatalogue/*.go
   ```

   Expected: every match is either a `default:"MINIMAX_API_KEY"` struct tag or the named fallback constant — no `map[key] = value` assignment anywhere.

4. The empty-endpoint branch exists and is read before any marshalling:

   ```bash
   awk '/func Models/,/^}/' pkg/providercatalogue/providercatalogue.go
   ```

   Expected: the function body prints, and it returns `nil` when `ProviderBaseURL` is empty, before any `json.Marshal` call. The behavioural proof is spec 2 in requirement 5; this command only makes the branch visible.

5. `make precommit` exits 0 — let the exit status stand:

   ```bash
   make precommit
   ```

   Expected: exits 0 and ends with `ready to commit`.

6. The changelog entry sits under `## Unreleased`, not under a released heading:

   ```bash
   awk '/^## /{sec=$0} /PROVIDER_BASE_URL/{print "sits under: " sec; exit}' CHANGELOG.md
   ```

   Expected: prints `sits under: ## Unreleased`.

</verification>
