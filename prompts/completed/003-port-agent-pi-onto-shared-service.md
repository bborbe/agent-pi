---
status: completed
summary: Replaced agent-pi's own HTTP routing, session cache, per-session locking and session-id validation with the shared interactive service from github.com/bborbe/agent v0.92.0, leaving only the pi-backed session-factory wiring in this repository.
execution_id: agent-pi-exec-003-port-agent-pi-onto-shared-service
dark-factory-version: v0.196.0
created: "2026-10-01T08:36:43Z"
queued: "2026-10-01T10:02:16Z"
started: "2026-10-01T10:03:01Z"
completed: "2026-10-01T10:10:54Z"
---

# Port agent-pi onto the shared interactive service

<summary>
- The long-running service mode stops carrying its own web routing, its own per-session bookkeeping and its own request locking; that machinery now comes from the shared library.
- The same three endpoints keep working exactly as before: the same routes, the same status codes, the same request header, the same body limit.
- A caller that sends no session header still reaches the same default conversation it always did.
- Two different sessions still run at the same time, and two requests on one session still take turns.
- The binary keeps its task-running mode, its flags, its environment variables, its Kafka publishing and its metrics registry unchanged.
- The service now emits a start/end pair around every turn, so per-session serialisation is visible in the pod log.
- What remains in this repository is wiring only: the code that hands the library a pi-backed session factory.
- The change is recorded in the changelog under an unreleased heading, and no version number is invented.
</summary>

<objective>
Make agent-pi a thin caller of the shared interactive service that now lives in `github.com/bborbe/agent`: delete this repository's own HTTP routing, session cache, per-session locking and session-id validation, and construct the library's service instead. The reason is the frozen `:9090` contract — it must be preserved exactly, and the only way to keep two interactive images from drifting apart is for one implementation to serve it. After this change, adding a second interactive backend means writing one session implementation, not another copy of the HTTP layer.
</objective>

<context>
Read these before writing any code:

- `CLAUDE.md` — this repository's conventions, including "Do NOT commit — dark-factory handles git".
- `docs/dod.md` — this repository's definition of done, and the dark-factory validation prompt for this run. Its rules on coverage, GoDoc comments, error wrapping, README and changelog hygiene all apply.
- `main.go` — the entire change to this file is a deletion plus one rewiring. Read `runService`, `createRunner`, `createHTTPServer`, `promptHandler`, `readinessHandler`, `sessionIDFromRequest`, `sessionRunners`, `dialAddress`, and the four constants above them.
- `pkg/factory/factory.go` — read `CreatePiRunner` and `AgentTypeService`. You add one sibling constructor here.
- `main_internal_test.go` — every spec in it targets the code you are deleting.
- `main_test.go` — holds the single `TestSuite` / `RunSpecs`. Do NOT add a second `RunSpecs`: Ginkgo registers specs in one global suite per test binary, and this binary holds both `package main` and `package main_test`.
- `cmd/run-task/main.go` — the second entry point in this repository. It calls `factory.CreatePiRunner` with the current five-argument signature; it must still compile after your change.
- `README.md` — its "Service Agents" section documents the HTTP contract. The behaviour does not change, so this file must not change either.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md` — factory functions are pure composition: no conditionals, no I/O, no `context.Background()`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments say why a thing exists, not what its signature already says.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suite shape for this repository.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — the `## Unreleased` section and the required conventional bullet prefixes.

### The landed library API you must call

The library source is NOT in this repository (there is no `vendor/` directory). The signatures below were read from the landed files on the host and are quoted verbatim. After you bump the dependency (requirement 1), the same source is readable in the module cache at `$(go env GOMODCACHE)/github.com/bborbe/agent@<version>/` — if what you read there differs from what is quoted below, the source wins; follow it.

```go
// github.com/bborbe/agent  (agent_session.go, package lib)
type Session interface {
	Prompt(ctx context.Context, prompt string) (string, error)
	Close(ctx context.Context) error
}

type SessionFactory interface {
	Create(id string) Session
}
```

```go
// github.com/bborbe/agent/interactive  (service.go)
type Service interface {
	Handler() http.Handler
	Run(ctx context.Context) error
}

func NewService(
	sessions agentlib.SessionFactory,
	listen string,
	providerBaseURL string,
	registry *prometheus.Registry,
) Service
```

```go
// github.com/bborbe/agent/pi  (session.go)
func NewSessionFactory(
	base PiRunnerConfig,
	newRunner func(PiRunnerConfig) Runner,
) agentlib.SessionFactory

func NewSession(runner Runner) agentlib.Session
```

The `pi.Runner` and `pi.Result` surface this repository already uses is unchanged by the library release:

```go
// github.com/bborbe/agent/pi
type Runner interface {
	Run(ctx context.Context, prompt string) (*Result, error)
}

func NewRunner(config PiRunnerConfig) Runner

type Result struct {
	Result string `json:"result"`
}

func (r Result) GetResult() string { return r.Result }
```

`pi.PiRunnerConfig` keeps `AgentDir`, `AllowedTools`, `Model`, `Env`, `PersistSession` and `SessionID` unchanged. `pi.NewSessionFactory` derives the continuity pair from the session id itself: a non-empty id sets `PersistSession: true` and `SessionID: id`; an empty id leaves both unset. That derivation is the library's, so this repository must not repeat it.

The library's service implements the whole frozen `:9090` contract — the router, the method gate, the status codes, the `X-Session-Id` header and its regex, the 1 MiB truncating body cap, the `text/plain; charset=utf-8` response, and both readiness body strings. Do not re-implement any of it here.
</context>

<requirements>
1. **Resolve the dependency before writing anything else.** This repository's `go.mod` currently requires `github.com/bborbe/agent v0.90.4`.

   The API you need is not in any released version. Verified on the host:
   - `github.com/bborbe/agent`'s highest tag is `v0.90.4` (commit `0f2795f`), and that tag's tree contains neither `interactive/` nor `pi/session.go`.
   - The three commits that introduce the API — `5c8e359`, `8cfbcbb`, `8554522` — are not in any tag. They currently live only on the branch `feat/shared-interactive-agent-service` (the repository's local `master` is 3 commits ahead of `origin/master`, which is still at `0f2795f release v0.90.4`).

   So do this first:

   ```bash
   go list -m -versions github.com/bborbe/agent
   ```

   If the highest version listed is `v0.90.4` or lower, a release containing the new API has not been cut. **STOP and report the blocker** — name the required release (the first tag after `v0.90.4` that contains `interactive/service.go` and `pi/session.go`) and say that this prompt cannot proceed until it exists. Do not continue to step 2.

   If a higher version exists, proceed, and after step 6 run `go get github.com/bborbe/agent@latest` followed by `go mod tidy`. If that build then fails with `no required module provides package github.com/bborbe/agent/interactive`, the release is not resolvable after all: **STOP and report the same blocker.**

   In every blocked case: do NOT add a `replace` or `exclude` directive to `go.mod`, do NOT pin a branch, a pseudo-version or a commit SHA, and do NOT hand-edit `go.mod` to point at a local path. Report and stop.

2. **Delete the service layer from `main.go`.** Remove each of these, including its GoDoc comment:

   - the constants and variable `maxPromptBytes`, `serviceSessionID`, `sessionHeader`, `sessionIDPattern`
   - `func sessionIDFromRequest(r *http.Request) (string, error)`
   - `type runnerFactory func(sessionID string) pilib.Runner`
   - `type sessionRunner struct { mu sync.Mutex; runner pilib.Runner }`
   - `type sessionRunners struct { factory runnerFactory; mu sync.Mutex; byID map[string]*sessionRunner }`
   - `func newSessionRunners(factory runnerFactory) *sessionRunners`
   - `func (s *sessionRunners) get(id string) *sessionRunner`
   - `func (a *application) createHTTPServer(registry *prometheus.Registry, sessions *sessionRunners) run.Func`
   - `func (a *application) promptHandler(sessions *sessionRunners) http.Handler`
   - `func (a *application) readinessHandler() http.Handler`
   - `func dialAddress(raw string) (string, error)`

   Keep `agentName`, the `application` struct and all of its fields, `Run`, `createRunner`, `createDeliverer`, and `runService` (which step 5 rewrites). Nothing about the task path, flag/env parsing, Kafka wiring or the metrics registry changes.

3. **Extract the pi subprocess environment into `func (a *application) piEnv() map[string]string`** in `main.go`, so the task path and the service path build it the same way instead of duplicating it. It returns the environment overrides handed to every pi subprocess this binary spawns: `MINIMAX_API_KEY` set to `a.ProviderAPIKey` when that field is non-empty, and an empty map otherwise — an unset key must leave pi's own default provider configuration intact.

   Rewire `createRunner` to use it:

   ```go
   func (a *application) createRunner(sessionID string) pilib.Runner {
   	return factory.CreatePiRunner(a.AgentDir, a.AllowedTools, a.Model, a.piEnv(), sessionID)
   }
   ```

   `createRunner` keeps its signature and its meaning: the task path passes `""` and stays ephemeral.

4. **Add one constructor to `pkg/factory/factory.go`**, alongside `CreatePiRunner`, as pure composition with no conditionals:

   ```go
   // CreatePiSessionFactory builds the shared service's per-session factory over
   // the pi runner. The session id selects the conversation: pi.NewSessionFactory
   // derives PersistSession and SessionID from it, so this repository does not
   // repeat that derivation.
   func CreatePiSessionFactory(
   	agentDir string,
   	allowedTools string,
   	model string,
   	env map[string]string,
   ) agentlib.SessionFactory {
   	return pilib.NewSessionFactory(
   		pilib.PiRunnerConfig{
   			AgentDir:     agentDir,
   			AllowedTools: allowedTools,
   			Model:        model,
   			Env:          env,
   		},
   		pilib.NewRunner,
   	)
   }
   ```

   Do NOT change `CreatePiRunner`'s signature — `cmd/run-task/main.go` and `main.go`'s task path both call it with five arguments, and both must keep compiling.

5. **Rewrite `runService` in `main.go`** to construct the shared service. It keeps its current signature `func (a *application) runService(ctx context.Context, registry *prometheus.Registry) error` and its current log line. Old → new:

   ```go
   // OLD
   sessions := newSessionRunners(a.createRunner)
   return service.Run(
   	ctx,
   	func(ctx context.Context) error {
   		<-ctx.Done()
   		return nil
   	},
   	a.createHTTPServer(registry, sessions),
   )
   ```

   ```go
   // NEW
   sessions := factory.CreatePiSessionFactory(a.AgentDir, a.AllowedTools, a.Model, a.piEnv())
   svc := interactive.NewService(sessions, a.Listen, a.ProviderBaseURL, registry)
   return service.Run(
   	ctx,
   	func(ctx context.Context) error {
   		<-ctx.Done()
   		return nil
   	},
   	svc.Run,
   )
   ```

   `github.com/bborbe/service`'s `Run` takes variadic `run.Func` (`func(context.Context) error`), so the `svc.Run` method value satisfies it — you do not need to name the `run` package.

6. **Fix `main.go`'s imports.** Add `interactive "github.com/bborbe/agent/interactive"`. Remove the imports that only the deleted code used: `crypto/sha256`, `encoding/hex`, `fmt`, `io`, `net`, `net/http`, `net/url`, `regexp`, `strings`, `sync`, `libhttp "github.com/bborbe/http"`, `github.com/bborbe/run`, and `github.com/prometheus/client_golang/prometheus/promhttp`. `time`, `prometheus`, `push`, `glog`, `pilib` and the rest stay.

   After the change, `go mod tidy` (run by `make precommit`'s `ensure` target) will drop `github.com/bborbe/http` and `github.com/bborbe/run` from `go.mod` because nothing in this repository imports them any more. That is expected — let it happen, and do not re-add them by hand.

7. **Replace the contents of `main_internal_test.go`.** Every spec in it targets deleted code and the file will not compile as-is. Keep the file's license header and its `package main` declaration (it stays the internal test file per `docs/dod.md`), and keep exactly one `Describe` covering the one unexported helper that survives:

   ```go
   var _ = Describe("piEnv", func() {
   	It("passes the provider key to the subprocess when it is configured", func() {
   		app := &application{ProviderAPIKey: "secret"}
   		Expect(app.piEnv()).To(Equal(map[string]string{"MINIMAX_API_KEY": "secret"}))
   	})

   	It("sets no override when no provider key is configured", func() {
   		app := &application{}
   		Expect(app.piEnv()).To(BeEmpty())
   	})
   })
   ```

   Do NOT add a second `RunSpecs` — `main_test.go` holds the suite entry point for this binary.

   Do NOT add any new test that touches `net/http`, `httptest` or `promhttp` in this repository. The shared service's HTTP contract is the library's to test (it already is), and requirement 11 asserts that no HTTP-handling code remains here; a test that imports `net/http` would make verification step 2 fail. The unit-level coverage of the HTTP contract moves with the code.

8. **Add a `## Unreleased` section to `CHANGELOG.md`** directly above `## v0.5.0`, after the existing preamble. One bullet, prefixed `refactor:`, describing what a reader of the release notes needs to know: the interactive service mode now uses the shared implementation from `github.com/bborbe/agent`, the `:9090` contract is unchanged, and each turn is now bracketed by a start/end log pair naming the session id. Do not rename `## Unreleased`, do not touch the preamble or any released section, and do not invent a version number.

9. **Update `README.md` in exactly one place; leave everything else in it unchanged.** Its "Service Agents" section documents the frozen contract, and the contract itself does not change — but one sentence in it does not survive this change. It currently reads "neither the prompt nor the session id is ever logged — only the prompt's length and a short digest". The shared service now brackets every turn with `turn start id=<id>` and `turn end id=<id>` at V(2), so the session id IS logged, and only there. Correct that sentence: the prompt content is still never logged, and the session id now appears only in the per-turn start/end pair, which is what makes per-session serialisation visible from outside the process. Change nothing else in the file, and do not add a changelog-style note to it.

10. **Self-check before finishing.** Re-run every command in `<verification>` and confirm each one passes, quoting the actual output. Then walk the list of deleted symbols in requirement 2 and confirm, one by one, that none of them still exists anywhere in the repository.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The frozen HTTP contract is preserved exactly and is now the library's responsibility: routes `/readiness`, `/metrics`, `POST /prompt`; default listen `:9090`; 1 MiB body cap that TRUNCATES an oversized body and still answers 200 (it does not reject); header `X-Session-Id`; id regex `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`; absent header resolves to `identity`; present-but-empty header is a 400; non-POST on `/prompt` is a 405; with `PROVIDER_BASE_URL` unset the readiness body is exactly `OK (PROVIDER_BASE_URL unset — provider reachability not checked)`. Do not re-implement any of these in this repository.
- The task-mode path, the flag and env parsing, the Kafka wiring and the metrics registry stay exactly as they are.
- `cmd/run-task/main.go` is a second entry point. `factory.CreatePiRunner`'s signature must not change, or `cmd/run-task/main.go` must be updated in the same change — it must compile either way.
- No `replace` or `exclude` directive is added to `go.mod`, and `go.mod` is never hand-edited.
- Errors are wrapped with `github.com/bborbe/errors` (`errors.Wrap`, `errors.Wrapf`, `errors.Errorf` with a `ctx`) — never `fmt.Errorf`, never a bare `return err`.
- Interface → Constructor → Struct → Method: the interface is exported, the implementation struct is private, and `New*` returns the interface. Factory functions are pure composition — no conditionals, no I/O, no `context.Background()`.
- No package-level mutable state and no `init()`.
- Every exported type, function and interface carries a GoDoc comment that says why it exists.
- Do not rename `## Unreleased` and do not create a version tag — the release bot owns both.
- No daemon artifacts, editor files, build products or scratch output are committed.
- Existing tests must still pass.
</constraints>

<verification>
Run each command below. All must pass. Quote the actual output of each in your completion report.

1. `make precommit` — exits 0, zero lint findings.

2. The routing must be gone from this repository, not merely moved to another file in it:

   ```bash
   ! grep -rqE 'ListenAndServe|ServeMux|http\.Handler' --include='*.go' --exclude-dir=vendor .
   ```

   exits 0, i.e. the pattern matches nothing. Run it without `-q` as well and confirm the output is empty:

   ```bash
   grep -rnE 'ListenAndServe|ServeMux|http\.Handler' --include='*.go' --exclude-dir=vendor .
   ```

   Expected output: nothing at all. Note the pattern matches `promhttp.HandlerFor` as a substring of `http.Handler`, so no `promhttp` usage may remain either. For contrast, on the pre-change tree this pattern matches exactly 6 lines, all in `main.go` — if your run still matches anything, the deletion is incomplete.

3. No session bookkeeping, locking or id-validation code remains either:

   ```bash
   ! grep -rqE 'sessionIDPattern|sessionRunners|sessionHeader|X-Session-Id|newSessionRunners|maxPromptBytes|serviceSessionID' --include='*.go' --exclude-dir=vendor .
   ```

   exits 0. Expected output of the same command without `-q`: nothing. For contrast, the pre-change tree matches 34 lines across `main.go` and `main_internal_test.go`.

4. `! grep -q 'X-Session-Id' main.go` — exits 0, so the header name no longer appears in `main.go`. Do not use `grep -c` as the pass/fail check: it exits 1 when the count is zero, so an absence assertion written with it fails the step it was meant to pass. (`grep -c 'X-Session-Id' main.go` prints `0`; that is the expected count, not the check.)

5. `go build ./...` — exits 0.

6. `go vet ./...` — no findings.

7. `go list -m github.com/bborbe/agent` — prints a version higher than `v0.90.4`, confirming the dependency bump landed. If it prints `v0.90.4`, requirement 1 was not satisfied: stop and report rather than continuing.
</verification>
