---
status: completed
summary: Bumped github.com/bborbe/agent to v0.99.3 and wired the shared interactive A2A surface into agent-pi's service mode (Agent Card + JSON-RPC endpoint, bearer-gated routes, three new required config values), with extracted testable guards, README and changelog updates.
execution_id: agent-pi-a2a-exec-004-expose-a2a-surface
dark-factory-version: v0.196.0
created: "2026-10-10T12:36:05Z"
queued: "2026-10-10T12:36:05Z"
started: "2026-10-10T14:02:52Z"
completed: "2026-10-10T14:09:05Z"
---

# Expose an A2A surface on agent-pi

<summary>
- The long-running service mode now advertises itself to other agents over the A2A protocol: a public Agent Card and a JSON-RPC endpoint beside the prompt intake it already serves.
- A managing session can hold a multi-turn conversation with this agent, addressed by one conversation id it chooses up front.
- The surface comes from the shared library this binary already builds on, so this repository gains wiring and configuration, not a second copy of the HTTP layer.
- Every gated route — the prompt intake and the new A2A endpoint — now requires a bearer credential, while the readiness probe, the metrics endpoint and the Agent Card itself stay public.
- A service that cannot authenticate, or that cannot advertise a real network endpoint, refuses to start rather than serving unauthenticated or advertising a dead address.
- The task-running mode, its flags, its environment variables and its Kafka publishing are unchanged.
- The new routes and the new configuration are documented in the README, and the change is recorded in the changelog under an unreleased heading.
- No version number is invented.
</summary>

<objective>
Give agent-pi's service mode an A2A surface — an Agent Card at the well-known path and a JSON-RPC `SendMessage` endpoint — by bumping `github.com/bborbe/agent` to a release that carries the shared `interactive` implementation of it and wiring the three configuration values it needs. The reason is that agent-pi is currently reachable only by a caller who already knows its address and speaks its bespoke prompt route; an A2A surface is what lets a managing session discover it, authenticate to it, and hold one conversation with it, which is the whole point of the change. The library already implements the protocol, so the correct change here is a dependency bump plus wiring, never a re-implementation.
</objective>

<context>
Read these before writing any code:

- `CLAUDE.md` — this repository's conventions, including "Do NOT commit — dark-factory handles git", the error-wrapping rule and the `make precommit` scope.
- `docs/dod.md` — this repository's definition of done, and the dark-factory validation prompt for this run. Its rules on coverage, GoDoc comments, error wrapping, README and changelog hygiene all apply. Two of its rules are load-bearing here: a new required environment variable is a breaking change to every existing deployment and must be called out, and public HTTP behaviour that changes must be documented in the README in the same commit.
- `main.go` — the whole change to this file is three struct fields, one extracted guard, one rewired constructor call and one log line. Read the `application` struct and its field tags, `Run`, `piEnv`, and `runService`.
- `main_internal_test.go` — `package main`, and the file that covers unexported symbols. Read the `piEnv` spec for the shape to follow.
- `main_test.go` — holds the single `TestSuite` / `RunSpecs` for this binary. Do NOT add a second `RunSpecs`: Ginkgo registers specs in one global suite per test binary, and this binary holds both `package main` and `package main_test`.
- `README.md` — its "Service Agents" section documents the HTTP contract. The contract changes in this change, so this file must change with it.
- `CHANGELOG.md` — the `## Unreleased` section and the required conventional bullet prefixes.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — the `## Unreleased` section shape and the required conventional bullet prefixes.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suite shape for this repository.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments say why a thing exists, not what its signature already says.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — this change adds a method that constructs errors, so the wrapping rules apply directly.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — this change adds a glog line, so the verbosity and never-log-a-credential rules apply directly.

### Why this prompt carries no `spec:` link

The sibling prompts in `prompts/completed/` carry one because they were generated from a spec. This one was not, deliberately: the canonical flow decision ([`docs/choosing-a-flow.md`](https://github.com/bborbe/dark-factory/blob/master/docs/choosing-a-flow.md)) lists "bump a dependency that requires accompanying code edits" verbatim as a **prompt** case, and that is the whole of this change — the surface itself already ships in the library. A spec would add a durable document whose "why" is already recorded in the vault task and goal that commissioned it. Do not create one as part of this prompt.

### The landed library API you must call

The library source is NOT in this repository (there is no `vendor/` directory). The signatures below were read from the released tag `v0.99.3` on the host and are quoted verbatim. After you bump the dependency (requirement 1), the same source is readable in the module cache at `$(go env GOMODCACHE)/github.com/bborbe/agent@v0.99.3/` — if what you read there differs from what is quoted below, the source wins; follow it.

```go
// github.com/bborbe/agent/interactive  (service.go)
func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
	auth Auth,
	card CardConfig,
	sessionIdleTimeout time.Duration,
	maxSessions int,
) Service
```

```go
// github.com/bborbe/agent/interactive  (auth.go)
// Auth is what every gated route requires. The zero Auth refuses every gated
// request; NewAuthToken requires the given bearer credential.
func NewAuthToken(token string) Auth
```

```go
// github.com/bborbe/agent/interactive  (a2a.go)
// CardConfig is what the A2A Agent Card advertises about this service.
type CardConfig struct {
	// Name names the deployed agent in the Agent Card. Empty means the card keeps
	// the library default, "interactive".
	Name string
	// PublicURL is the externally reachable A2A endpoint the card advertises
	// verbatim. It is never derived from the listen address or a request header.
	PublicURL string
}
```

```go
// github.com/bborbe/agent/interactive  (session-cache.go)
const DefaultSessionIdleTimeout = 15 * time.Minute
const DefaultMaxSessions = 8
```

Three facts about this API decide the shape of the change, and each one was verified against the released tag rather than assumed:

- **The A2A routes are mounted unconditionally.** `service.Handler()` registers `/prompt`, `/.well-known/agent-card.json` and `/a2a` every time, with no option to serve one and not the others. There is no "A2A off" configuration, so the surface cannot be added as an opt-in that leaves the old behaviour intact.
- **The zero `Auth` refuses every gated route**, `/prompt` included — not just `/a2a`. A service built with it answers 401 everywhere. That is why the credential cannot be optional: leaving it unset does not preserve today's unauthenticated `/prompt`, it breaks it.
- **`NewService` is the permissions-free constructor.** agent-pi has never served the permission endpoint and must not start now. Do NOT reach for `NewServiceWithPermissions`.

### The sibling implementation to mirror

`bborbe/agent-claude` is the same change, already landed and merged (PR #24). Its service mode wires exactly this API. The host path is not readable from the container, so the shape is quoted here — mirror it, do not re-derive it:

```go
// agent-claude main.go, runService
if a.InteractiveAuthToken == "" {
	return errors.Errorf(
		ctx,
		"INTERACTIVE_AUTH_TOKEN is required for a service agent; a service that cannot authenticate must not serve unauthenticated",
	)
}
if a.A2APublicURL == "" {
	return errors.Errorf(
		ctx,
		"A2A_PUBLIC_URL is required for a service agent; the Agent Card must advertise a real network endpoint",
	)
}
if a.A2AAgentName == "" {
	return errors.Errorf(
		ctx,
		"A2A_AGENT_NAME is required for a service agent; the Agent Card must name the deployed agent",
	)
}

svc := interactive.NewService(
	sessions,
	a.Listen,
	a.ProviderBaseURL,
	registry,
	interactive.NewAuthToken(a.InteractiveAuthToken),
	interactive.CardConfig{
		Name:      a.A2AAgentName,
		PublicURL: a.A2APublicURL,
	},
	interactive.DefaultSessionIdleTimeout,
	interactive.DefaultMaxSessions,
)
```

Its field tags are the same ones this change adds:

```go
InteractiveAuthToken string `required:"false" arg:"interactive-auth-token" env:"INTERACTIVE_AUTH_TOKEN" usage:"Bearer token required by the interactive service's gated routes" display:"length"`
A2APublicURL         string `required:"false" arg:"a2a-public-url" env:"A2A_PUBLIC_URL" usage:"Externally reachable A2A endpoint URL the Agent Card advertises (service agents only)"`
A2AAgentName         string `required:"false" arg:"a2a-agent-name" env:"A2A_AGENT_NAME" usage:"Name the A2A Agent Card advertises for this agent (service agents only)"`
```

One deliberate difference from the sibling: agent-claude inlines its three guards directly in `runService`, which leaves them untestable. This change extracts them instead (requirement 4), because `docs/dod.md` requires a test for every new branch of behaviour and an inlined guard cannot have one without starting a server.
</context>

<requirements>
1. **Bump the dependency to `v0.99.3`.** `go.mod` currently requires `github.com/bborbe/agent v0.92.0`, which predates the A2A surface (it landed in `v0.96.0`, and `CardConfig` in `v0.97.0`). Run:

   ```bash
   go get github.com/bborbe/agent@v0.99.3
   ```

   This repository already imports `github.com/bborbe/agent/interactive` in `main.go`, so the module is already a direct requirement and the order trap does not apply — the import exists before the tidy. Do NOT run `go mod vendor`: `vendor/` is a build-time artifact, is not committed in this repository, and `make precommit`'s `ensure` target wipes it. Do NOT add a `replace` or `exclude` directive and do NOT pin a pseudo-version or a commit SHA.

2. **Add three fields to the `application` struct in `main.go`**, in the existing tag style, with GoDoc comments that say why each exists:

   ```go
   InteractiveAuthToken string `required:"false" arg:"interactive-auth-token" env:"INTERACTIVE_AUTH_TOKEN" usage:"Bearer token required by the interactive service's gated routes" display:"length"`
   A2APublicURL         string `required:"false" arg:"a2a-public-url" env:"A2A_PUBLIC_URL" usage:"Externally reachable A2A endpoint URL the Agent Card advertises (service agents only)"`
   A2AAgentName         string `required:"false" arg:"a2a-agent-name" env:"A2A_AGENT_NAME" usage:"Name the A2A Agent Card advertises for this agent (service agents only)"`
   ```

   `display:"length"` on the token is required, not cosmetic: it is what makes the startup log name the field and report its length instead of printing the credential. `SentryDSN` and `ProviderAPIKey` already carry it for the same reason.

   All three stay `required:"false"`. A `required:"true"` tag would be evaluated for every agent shape, so it would also reject the task-routed jobs, which never serve HTTP. The requirement belongs in `runService`, which is reached only for a service agent — see requirement 4.

3. **Document the new configuration and the new routes in `README.md`.** Its "Service Agents" section currently documents the frozen contract and does not mention the A2A surface at all. Add to it: the two new routes (`GET /.well-known/agent-card.json`, the public card, and `POST /a2a`, the JSON-RPC `SendMessage` binding), that every gated route now requires `Authorization: Bearer <INTERACTIVE_AUTH_TOKEN>`, and the three environment variables a service agent must now set — `INTERACTIVE_AUTH_TOKEN`, `A2A_PUBLIC_URL` and `A2A_AGENT_NAME`. State plainly that these three are required for a service agent and that a service agent which does not set them now fails to start, because that is the breaking change this release carries for every existing deployment. State which routes are exempt and stay unauthenticated: `/readiness`, `/metrics` and the Agent Card at `/.well-known/agent-card.json`. Only `/prompt` and `/a2a` require `Authorization: Bearer <INTERACTIVE_AUTH_TOKEN>`. Say that explicitly — a README claiming "every route now requires a token" would contradict its own readiness row and mislead anyone writing a kubelet probe. Change nothing else in the file.

4. **Extract the guard into an unexported method and call it from `runService`.** Add to `main.go`:

   ```go
   // validateServiceConfig enforces the three values a service agent cannot serve
   // without. It is a method rather than a required:"true" struct tag because
   // argument.Parse evaluates required tags for every agent shape, so a tag would
   // also reject the task-routed jobs, which never serve HTTP. It is extracted from
   // runService so the failure paths can be tested without starting a server.
   func (a *application) validateServiceConfig(ctx context.Context) error
   ```

   It returns a `github.com/bborbe/errors` error for the first empty value among `InteractiveAuthToken`, `A2APublicURL` and `A2AAgentName`, in that order, using the sibling's messages verbatim:

   - `"INTERACTIVE_AUTH_TOKEN is required for a service agent; a service that cannot authenticate must not serve unauthenticated"`
   - `"A2A_PUBLIC_URL is required for a service agent; the Agent Card must advertise a real network endpoint"`
   - `"A2A_AGENT_NAME is required for a service agent; the Agent Card must name the deployed agent"`

   It returns `nil` when all three are set. Call it as the first statement of `runService`, before the session factory is built:

   ```go
   if err := a.validateServiceConfig(ctx); err != nil {
   	return err
   }
   ```

   **Also add a second unexported method to `main.go`, and use it in requirement 5 instead of an inline literal:**

   ```go
   // cardConfig is what this service advertises in its A2A Agent Card. It exists as a
   // method so the two fields' mapping can be asserted directly: both are plain
   // strings, so swapping them still compiles, still passes every other spec, and
   // yields a card whose name is a URL and whose interface URL is a bare name — a
   // defect nothing else in this change would catch.
   func (a *application) cardConfig() interactive.CardConfig
   ```

   It returns `interactive.CardConfig{Name: a.A2AAgentName, PublicURL: a.A2APublicURL}` — name to name, URL to URL. Do not add a third method or a constructor for anything else here; this one exists solely because the mapping is otherwise untestable.

5. **Rewire the constructor call in `runService`.** Old → new:

   ```go
   // OLD
   svc := interactive.NewService(sessions, a.Listen, a.ProviderBaseURL, registry)
   ```

   ```go
   // NEW
   svc := interactive.NewService(
   	sessions,
   	a.Listen,
   	a.ProviderBaseURL,
   	registry,
   	interactive.NewAuthToken(a.InteractiveAuthToken),
   	a.cardConfig(),
   	interactive.DefaultSessionIdleTimeout,
   	interactive.DefaultMaxSessions,
   )
   ```

   `runService` keeps its signature and its existing `service.Run` wiring unchanged.

6. **Extend `runService`'s existing log line** so an operator reading the pod log at `V(2)` can see which agent the card advertises and where. It currently reads `"agent-pi service mode: serving readiness, metrics and prompt intake on %s"`. Make it name the A2A surface too, including the agent name and the public endpoint — for example `"agent-pi service mode: serving readiness, metrics, prompt intake and the A2A Agent Card (/.well-known/agent-card.json, agent %s, endpoint %s) on %s"`. Keep it gated at `V(2)`; do not print the token or its value at any verbosity.

7. **Add the specs for both methods to `main_internal_test.go`**, following the shape of the existing `piEnv` `Describe`. Cover the behaviour, not the implementation:

   - a service configuration with all three values set returns no error
   - a configuration missing only `InteractiveAuthToken` returns an error
   - a configuration missing only `A2APublicURL` returns an error
   - a configuration missing only `A2AAgentName` returns an error
   - `cardConfig()` maps the agent name to the card's `Name` and the public URL to the card's `PublicURL` — assert both fields, with two distinct values so a transposition fails

   Assert on the returned error being non-nil (or nil) rather than on its message text where you can, so the specs do not become a change-detector on wording. Do NOT add a second `RunSpecs` — `main_test.go` holds the suite entry point for this binary. Do NOT add a spec that starts an HTTP server: the A2A route's own behaviour is the library's to test, and this repository must not re-implement or re-test it. The `cardConfig()` spec above is the boundary contract for this change — it is a direct field-mapping assertion, not an HTTP test, and it is the only thing standing between a `Name`↔`PublicURL` transposition and production.

8. **Add a `## Unreleased` section to `CHANGELOG.md`** directly above the highest released heading, after the existing preamble. One bullet, prefixed `feat:`, describing what a reader of the release notes needs to know: a service agent now serves an A2A Agent Card and a JSON-RPC endpoint, every gated route now requires the bearer credential, and a service agent must now set `INTERACTIVE_AUTH_TOKEN`, `A2A_PUBLIC_URL` and `A2A_AGENT_NAME` — naming that last point as the breaking change it is. Do not rename `## Unreleased`, do not touch the preamble or any released section, and do not invent a version number.

9. **Self-check before finishing.** Re-run every command in `<verification>` and confirm each one passes, quoting the actual output. Then walk each acceptance criterion above against the change, one by one.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The A2A surface is the library's; this repository only configures it. Do not implement, copy or vendor an Agent Card handler, a JSON-RPC handler, a router, a session cache or an auth middleware in this repository.
- The task-running mode, the flag and env parsing, the Kafka wiring, the metrics registry and `pkg/factory/` stay exactly as they are. This change touches service mode only.
- `cmd/run-task/main.go` is a second entry point. `factory.CreatePiRunner`'s signature must not change, and that file must still compile.
- No `replace` or `exclude` directive is added to `go.mod`, and `go.mod` is never hand-edited.
- Do NOT run `go mod vendor` and do not commit a `vendor/` directory.
- Errors are wrapped with `github.com/bborbe/errors` (`errors.Wrap`, `errors.Wrapf`, `errors.Errorf` with a `ctx`) — never `fmt.Errorf`, never a bare `return err`.
- Logging uses `glog`. New informational logs are gated at `V(2)`. The token is never logged, at any verbosity, in any form — no value, no prefix, no length beyond what `display:"length"` already emits at startup.
- Every exported type, function and interface carries a GoDoc comment that says why it exists.
- Existing tests must still pass.
- Do not rename `## Unreleased` and do not create a version tag — the release bot owns both.
- No daemon artifacts, editor files, build products or scratch output are committed.
</constraints>

<verification>
Run each command below. All must pass. Quote the actual output of each in your completion report.

1. `ROOTDIR=/workspace make precommit` — exits 0, zero lint findings. (`/workspace` is the container's mount point for this repository. The Makefile's own fallback resolves `ROOTDIR` from `git rev-parse --show-toplevel`, which comes back empty whenever a run masks `.git`; passing it explicitly is correct either way.)

2. `go list -m github.com/bborbe/agent` — prints `v0.99.3`, confirming the dependency bump landed. If it prints `v0.92.0`, requirement 1 was not satisfied: stop and report rather than continuing.

3. The surface is configured, not implemented here — this repository must still contain no HTTP-handling code of its own:

   ```bash
   ! grep -rqE 'ListenAndServe|ServeMux|a2asrv|httptest' --include='*.go' --exclude-dir=vendor .
   ```

   exits 0, i.e. the pattern matches nothing. Run it without `-q` as well and confirm the output is empty:

   ```bash
   grep -rnE 'ListenAndServe|ServeMux|a2asrv|httptest' --include='*.go' --exclude-dir=vendor .
   ```

   Expected output: nothing at all. A match means the surface was re-implemented rather than configured. Note this pattern deliberately does NOT include the literal `agent-card.json`: requirement 6 puts that path in a log line, which is a string in `main.go` and not HTTP-handling code. For contrast, on the pre-change tree this pattern matches nothing either — it is an absence assertion, so a match is the signal.

4. The three configuration values are wired through, not merely declared:

   ```bash
   grep -c 'INTERACTIVE_AUTH_TOKEN\|A2A_PUBLIC_URL\|A2A_AGENT_NAME' main.go
   ```

   Expected: a count of 3 or more (one env tag per field). Do not use `grep -c` as a pass/fail check anywhere it is expected to print `0` — it exits 1 when the count is zero, so an absence assertion written that way fails the step it was meant to pass. For absence, use `! grep -q`.

5. The credential never reaches a log or a test fixture:

   ```bash
   ! grep -rqE 'glog\.[A-Za-z]+f?\(.*InteractiveAuthToken|Expect\(.*InteractiveAuthToken' --include='*.go' --exclude-dir=vendor .
   ```

   exits 0, so no log call or spec interpolates the token field's value.

6. `go build ./...` — exits 0.

7. `go vet ./...` — no findings.
</verification>
