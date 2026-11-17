---
status: completed
summary: 'Added an Authorization: Bearer $PROVIDER_KEY_ENV header to the rendered pi models.json catalogue, with a spec proving it, README and CHANGELOG updates; make precommit exits 0.'
execution_id: agent-pi-auth-header-exec-007-provider-auth-header
dark-factory-version: v0.198.17
created: "2026-11-17T03:58:00Z"
queued: "2026-11-17T02:49:03Z"
started: "2026-11-17T02:49:05Z"
completed: "2026-11-17T02:53:38Z"
---

# Send the provider credential as a bearer header

<summary>
- The pi CLI now presents its provider credential as an `Authorization: Bearer` header as well as the api-key header it already sends.
- This is what lets the fleet's in-cluster router authenticate the request: its middleware strips `x-api-key` and only forwards a bearer.
- An operator who configures no endpoint still gets no catalogue and no header, so nothing changes for a MiniMax-configured agent.
- The catalogue the binary renders is the only artifact that changes; no new setting is introduced.
- The change is covered by a test that fails if the header is dropped from the rendered catalogue.
- The changelog records it.
</summary>

<objective>
Make the pi CLI present its credential in the header form the fleet's model router forwards, so an agent pointed at the in-cluster router authenticates instead of being rejected upstream with 401. The binary already renders a `models.json`; this adds one header to that file.

</objective>

<context>
Read `docs/dod.md` and `README.md`.

The failure this fixes, measured on nuke dev on 2026-11-17: every pi request through `claude-code-router-dev` returned `status=401` from `api.seibert.ai` in ~90 ms while every Claude-agent request on the same router, same model and same minutes returned 200 — 10 rejected against 1302 accepted. A probe of the pi subprocess against a header-recording server shows why: pi sends `x-api-key: <credential>` and **no `Authorization` header at all**. The router's `session`/auth middleware deletes `x-api-key`, and its auth-swap transport only supplies a bearer when the provider has its own token configured; where it does not, the upstream receives no credential and answers 401. Claude agents are unaffected because Claude Code sends `Authorization: Bearer <token>` itself.

Files to read before editing:

- `pkg/providercatalogue/providercatalogue.go` — the renderer. `provider` struct holds `BaseURL`, `API`, `APIKey`, `Models`. `Models(ctx, settings)` builds it; `Write(ctx, settings, agentDir)` renders it to `$HOME/.pi/agent/models.json`.
- `pkg/providercatalogue/providercatalogue_test.go` — the existing specs, including the typed `catalogueDocument` used to unmarshal and assert.
- `main.go` and `cmd/run-task/main.go` — the callers. Neither needs a change: the header is derived from the same `ProviderKeyEnv` the catalogue already uses.

Verified in the pinned pi 0.87.1 package: `dist/core/model-config.d.ts` declares `ProviderConfigSchema.headers` as `Record<string, string>`, and `dist/core/resolve-config-value.js` (`resolveHeadersOrThrow`) applies the same `$NAME` / `${NAME}` interpolation `apiKey` gets — including an embedded literal, so `"Bearer $NAME"` resolves to `Bearer <value>`. `authHeader` is not a `models.json` field, so pi does not overwrite a configured `Authorization` with one of its own. The header therefore carries the variable *name* and pi resolves the value from its own environment at request time; the credential still never enters the file.

</context>

<requirements>

1. **Add a provider-level `headers` map to the rendered catalogue.** In `pkg/providercatalogue/providercatalogue.go`, the `provider` struct gains:

   ```go
   Headers map[string]string `json:"headers"`
   ```

   and `Models` populates it with exactly one entry: `Authorization` → `"Bearer $" + settings.ProviderKeyEnv`.

   The `$` is the literal interpolation marker, not a shell expansion — pi resolves `$PROVIDER_KEY_ENV` from its environment. Do not put the credential value in the file.

2. **Keep everything else identical.** `apiKey` stays `$<ProviderKeyEnv>` (pi still uses it for providers that read the api-key header), `baseUrl`, `api`, `models`, `contextWindow` and `maxTokens` are unchanged, and the empty-`ProviderBaseURL` path still returns `nil` and writes nothing.

3. **Extend `pkg/providercatalogue/providercatalogue_test.go`.** Add `Headers map[string]string \`json:"headers"\`` to the typed `catalogueDocument` provider struct, and add a **new** `It` ("sends the credential as a bearer header") asserting that `providers.custom.headers.Authorization` equals `"Bearer $ROUTER_KEY"` for the `configured` fixture, and that the map has exactly one entry. The assertion must fail if the header is dropped, if a second header appears, or if the `Bearer ` prefix or the `$` marker is wrong. Leave the existing render spec untouched.

4. **Update `README.md`** so it says the rendered catalogue carries both an `apiKey` reference and an `Authorization: Bearer $<PROVIDER_KEY_ENV>` header, and why (the router strips `x-api-key` and forwards a bearer). Edit the paragraph beginning `Pi's own config (auth state, sessions) lives in $HOME/.pi` and the `PROVIDER_BASE_URL` row of the Env Vars table.

5. **Add a `## Unreleased` entry** to `CHANGELOG.md` above the highest released heading, conventional prefix (`fix:`), naming the bearer header.

6. **Self-check.** Run every command in `<verification>` and quote its output; then walk each requirement against the diff.

</requirements>

<constraints>

- Do NOT commit — dark-factory handles git.
- Do NOT change any default, any setting, or any caller signature.
- Do NOT write the credential value into `models.json` or into a log line — only the variable name appears in the file.
- Do NOT add a dependency.
- Do NOT touch `pkg/factory/`, `pkg/prompts/`, or the `bborbe/agent` library.
- Existing tests must still pass.
- No daemon artifacts, editor files, or scratch output committed.

</constraints>

<verification>

1. The suite passes, unpiped so the exit status is the check:

   ```bash
   go test ./... -count=1
   ```

   Expected: exits 0, five `ok` lines and one `?   …/mocks [no test files]`.

2. Every suite still runs:

   ```bash
   go test ./... -count=1 -v 2>&1 | grep -E 'Running Suite:|^Ran [0-9]+ of [0-9]+ Specs'
   ```

   Expected: five `Running Suite:` lines — `Main Suite`, `Factory Suite`, `Prompts Suite`, `Run-Task Suite`, `Providercatalogue Suite` — each followed by its `Ran N of N Specs` line, and the Providercatalogue one reads `Ran 8 of 8 Specs` (it is 7 today).

3. The rendered catalogue carries the header — the renderer's assignment, and the spec that proves it:

   ```bash
   grep -n 'Headers:' pkg/providercatalogue/providercatalogue.go
   go test ./pkg/providercatalogue/ -count=1 -v -ginkgo.v 2>&1 | grep -E 'bearer header'
   ```

   Expected: one `Headers:` line, and one passing spec line naming the bearer header.

4. `make precommit` exits 0:

   ```bash
   make precommit
   ```

5. The changelog entry sits under `## Unreleased`:

   ```bash
   awk '/^## /{sec=$0} /[Bb]earer/{print "sits under: " sec; exit}' CHANGELOG.md | grep -qx 'sits under: ## Unreleased'
   ```

   Expected: exits 0 with no output. (`CHANGELOG.md` already contains "Bearer" under `## v0.6.6`; the new entry sits above it, so the first match must be the new one.)

</verification>
