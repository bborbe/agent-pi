---
status: completed
spec: [001-per-caller-session-id]
summary: Added per-request session addressing via an optional X-Session-Id header on /prompt, with lazy per-session runners and locks, so distinct sessions run concurrently while one session stays serialized and header-less callers keep their existing conversation.
execution_id: agent-pi-pi-pin-exec-001-spec-001-session-id-intake
dark-factory-version: v0.196.0
created: "2026-09-28T21:06:06Z"
queued: "2026-09-28T21:29:50Z"
started: "2026-09-28T21:29:52Z"
completed: "2026-09-28T21:36:40Z"
branch: dark-factory/per-caller-session-id
---

# Serve each caller's session id on the prompt endpoint

<summary>
- A caller may name which conversation a prompt belongs to, using a request header.
- A caller that sends no header keeps reaching exactly the conversation the service uses today.
- A caller that sends a header gets its own conversation, separate from every other session id.
- Two different session ids are served at the same time; neither waits for the other.
- Two prompts on the same session id are still served one at a time, never overlapping.
- A malformed session id is refused before the agent is ever invoked, so a bad id cannot reach the agent process.
- The service still starts with the same configuration as today, and still needs no authentication.
- Existing behavior is untouched: the same status codes, the same request body limit, the same response format.
- The session id is never written to the log, so the log cannot be used to list live conversations.
</summary>

<objective>
Let a caller address a service agent with a session id of its own choosing and hold an independent conversation, while every caller that sends no session id continues to reach the conversation it reaches today. Today the session id is the compiled-in constant `serviceSessionID`, so every caller shares one conversation. This prompt makes the session id a per-request decision, validates it before it reaches the agent process, and keeps one runner and one lock per session id so that distinct sessions run in parallel and one session stays serialized.
</objective>

<context>
Read these before writing any code:

- `main.go` — the whole change lives here. Read `promptHandler`, `createHTTPServer`, `runService`, `createRunner`, and the `serviceSessionID` constant with its rationale comment.
- `main_internal_test.go` — the existing `promptHandler` specs, `fakeRunner`, and `overlapRunner`. Your new specs go in this file.
- `main_test.go` — `TestSuite`/`RunSpecs`. Do NOT add another `RunSpecs`: Ginkgo registers specs in one global suite per test binary, and this binary holds both `package main` and `package main_test`, so the existing `RunSpecs` in `main_test.go` runs specs registered from `main_internal_test.go`.
- `pkg/factory/factory.go` — `CreatePiRunner(agentDir, allowedTools, model string, env map[string]string, sessionID string) pilib.Runner`. This is the runner factory the handler must reach indirectly, through the seam you add.
- `docs/dod.md` — this repo's definition of done; it is also the dark-factory validation prompt for this run. Its rules on coverage, GoDoc comments, error wrapping, and changelog hygiene all apply.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega suite shape, counterfeiter mocks, coverage rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — mutex ownership, why the caller owns the lock, no raw `go func()` outside entry points.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` (`errors.Wrap`/`errors.Wrapf`/`errors.Errorf` with a `ctx`), never `fmt.Errorf`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments say why a thing exists, not what its signature says.

The exact `pilib` surface you will touch, verified against `github.com/bborbe/agent@v0.90.4`:

```go
// pi/pi-runner.go
type Runner interface {
	Run(ctx context.Context, prompt string) (*Result, error)
}

// pi/types.go
type Result struct {
	Result string `json:"result"`
}

func (r Result) GetResult() string { return r.Result }
```
</context>

<requirements>
1. **Add the session header contract to `main.go`, next to `serviceSessionID`.**

   ```go
   // sessionHeader is the request header a caller uses to address a session. It is
   // part of the contract this change establishes: callers depend on the exact
   // spelling, so it is a constant rather than a literal repeated in the handler.
   const sessionHeader = "X-Session-Id"

   // sessionIDPattern is the allowed session id format: one to 64 characters, the
   // first of which is not '-'. This is the security boundary, not a style
   // preference — the id reaches the agent CLI as a command-line argument, so a
   // leading '-' would be parsed as a flag, and '.' or '/' would escape a session
   // directory if the id were ever used to build a storage path.
   var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`)
   ```

   Import `regexp` (stdlib). Keep `serviceSessionID = "identity"` exactly as it is: it is the conversation existing callers already reach, and changing it would orphan that conversation.

2. **Add the session-id resolution function.** Signature:

   ```go
   func sessionIDFromRequest(r *http.Request) (string, error)
   ```

   Contract:
   - The header is **absent** → return `serviceSessionID` and a nil error. An absent header is an existing caller and must keep the behavior it has today.
   - The header is **present** → return the value only if it matches `sessionIDPattern`; otherwise return a non-nil error.
   - An **empty header value** is a present header that does not match, so it is an error. An absent header and an empty header are deliberately different outcomes; `http.Header.Values(sessionHeader)` distinguishes them (`len(values) == 0` means absent) while `http.Header.Get` does not, so use `Values`.
   - Wrap the error with `github.com/bborbe/errors`, passing `r.Context()`. The error message must **not** contain the session id: the id is caller-supplied and must not become something a future log line can enumerate.

3. **Add the runner-factory seam and the per-session runner store.**

   ```go
   // runnerFactory builds the runner for one session id. It is the seam the prompt
   // handler depends on instead of calling factory.CreatePiRunner directly, so a
   // spec can observe which session id a request resolved to.
   type runnerFactory func(sessionID string) pilib.Runner

   // sessionRunner is one session's conversation: the runner that owns it and the
   // lock that serializes turns within it. Two sessions hold two sessionRunners and
   // never contend; two requests on one session share its lock and take turns.
   type sessionRunner struct {
   	mu     sync.Mutex
   	runner pilib.Runner
   }

   // sessionRunners holds one runner per session id, built on first use. Building
   // lazily is what lets an unseen session id start a fresh conversation: pi's
   // --session-id creates the session when it is missing, so no registration step
   // exists anywhere in this design.
   type sessionRunners struct {
   	factory runnerFactory
   	mu      sync.Mutex
   	byID    map[string]*sessionRunner
   }

   func newSessionRunners(factory runnerFactory) *sessionRunners

   // get returns the sessionRunner for id, building and caching it on first use.
   func (s *sessionRunners) get(id string) *sessionRunner
   ```

   Contract for `get`: it holds `s.mu` only long enough to look up or insert the map entry; it must not hold `s.mu` while the session's own lock is held or while a prompt runs. `get` is the **only** place the factory is called.

4. **Change `promptHandler` to take the store instead of one runner.**

   `func (a *application) promptHandler(sessions *sessionRunners) http.Handler`

   Order of operations inside the handler, which is what makes the rejection path cheap and observable:
   1. Non-POST → `http.Error(w, "POST required", http.StatusMethodNotAllowed)`, return. (unchanged)
   2. Resolve the session id via `sessionIDFromRequest(r)`. On error → `http.Error(w, "invalid session id", http.StatusBadRequest)`, return. **This happens before the body is read and before any runner is built**, so an invalid id costs no read and produces zero factory calls.
   3. Read the body through `io.LimitReader(r.Body, maxPromptBytes)`; on read error → `http.Error(w, fmt.Sprintf("read prompt: %v", err), http.StatusBadRequest)`, return. (unchanged)
   4. Trim, reject an empty prompt with `http.Error(w, "empty prompt", http.StatusBadRequest)`, return. (unchanged)
   5. Log the existing line **verbatim** — `glog.V(2).Infof("prompt intake: bytes=%d sha256=%s", len(prompt), hex.EncodeToString(digest[:8]))`. Do **not** add the session id to it, or to any other log line.
   6. `session := sessions.get(sessionID)`, then `session.mu.Lock()` / `defer session.mu.Unlock()`, then `session.runner.Run(r.Context(), prompt)`.
   7. On runner error → log with `glog.Warningf` and `http.Error(w, "prompt failed", http.StatusInternalServerError)`, return. (unchanged)
   8. Success → set `Content-Type: text/plain; charset=utf-8` and write `result.GetResult()`. (unchanged)

   Delete the `var mu sync.Mutex` that lives inside `promptHandler`; its job is now the per-session lock on `sessionRunner`.

5. **Rewire the service path.**

   - `func (a *application) createHTTPServer(registry *prometheus.Registry, sessions *sessionRunners) run.Func` — register `/prompt` with `a.promptHandler(sessions)`.
   - In `runService`, build the store once and pass it: `sessions := newSessionRunners(a.createRunner)`, then `a.createHTTPServer(registry, sessions)`. `a.createRunner` already has the signature `func(sessionID string) pilib.Runner`, so it satisfies `runnerFactory` directly — do not add a wrapper closure.
   - `runService` no longer calls `a.createRunner(serviceSessionID)` eagerly; the default session's runner is built on the first request that resolves to it. That is not a behavior change: `createRunner` only constructs a struct and performs no I/O. Do not add an eager build back.
   - Leave `createRunner` and `factory.CreatePiRunner` unchanged, and leave the task-routed path in `Run` on `a.createRunner("")` exactly as it is — an empty id means no persistence, and this change does not touch that path.

6. **Update the existing specs in `main_internal_test.go` to the new construction.** Add one helper and use it at every existing `promptHandler` call site:

   ```go
   // staticSessions returns a session store whose factory hands out one runner for
   // every session id, for specs that do not care which session a request resolves to.
   func staticSessions(runner pilib.Runner) *sessionRunners {
   	return newSessionRunners(func(string) pilib.Runner { return runner })
   }
   ```

   Then `app.promptHandler(runner)` becomes `app.promptHandler(staticSessions(runner))`. The assertion of the existing spec `serializes concurrent prompts rather than interleaving two runs on one session` survives untouched: two requests with no session header both resolve to the default session, and the per-session lock still serializes them. Do not weaken or delete it.

7. **Add a request helper for the new specs:**

   ```go
   // promptRequest builds a POST /prompt request carrying the given session header
   // values. Called with no values it sends no header at all, which is the case
   // existing callers are in; called with one empty value it sends a present but
   // empty header, which is a different case.
   func promptRequest(body string, headerValues ...string) *http.Request
   ```

   It must use `req.Header.Add(sessionHeader, v)` for each value, so that `promptRequest(body)` sends no header and `promptRequest(body, "")` sends an empty one.

8. **Add five specs to `main_internal_test.go` (package `main`), each with the exact description string below.** The strings are grepped by this spec's verification, so each must appear on exactly one line, in the `It(...)` description only — never repeated in a comment, a `By`, a `Describe`/`Context` label, or a table entry.

   - `It("uses the default session when no session header is sent", ...)` — inject a factory that appends each session id it is handed to a slice and returns a `fakeRunner`; drive one `promptRequest("a question")` with no header. Assert the recorded slice equals `[]string{serviceSessionID}`, and assert `serviceSessionID` equals `"identity"` so a future rename cannot silently orphan the conversation existing callers reach.

   - `It("passes the caller's session id to the runner", ...)` — same recording factory; drive `promptRequest("a question", "Session_A-1")`. Assert the recorded slice equals `[]string{"Session_A-1"}`. Use a value with mixed case and both `_` and `-` so that any normalizing (lowercasing, trimming, slugging) in the handler fails the assertion — the contract is verbatim.

   - `It("rejects an invalid session id", ...)` — one spec that loops over these five values, and for each: builds a fresh handler over a factory that records its calls, sends `promptRequest("a question", value)`, and asserts status 400, that the factory was called zero times, and that the runner's `Run` was called zero times. Attach the offending value to each assertion's description (Gomega's `To(matcher, description...)` form) so a failure names the case.
     - `strings.Repeat("a", 65)` — longer than 64 characters
     - `"session/a"` — contains `/`
     - `"session.a"` — contains `.`
     - `""` — a present but empty header value
     - `"-dash"` — first character is `-`

   - `It("keeps two sessions apart", ...)` — a fake factory that binds each runner to the session id it was given, over a map shared by all sessions:

     ```go
     // memoryRunner is a fake runner that remembers one word per session id, so a
     // spec can show that two session ids hold two conversations and never share one.
     type memoryRunner struct {
     	sessionID string
     	words     map[string]string
     }

     func (r *memoryRunner) Run(_ context.Context, prompt string) (*pilib.Result, error) {
     	if word, ok := strings.CutPrefix(prompt, "remember the word "); ok {
     		r.words[r.sessionID] = word
     		return &pilib.Result{Result: "ok"}, nil
     	}
     	return &pilib.Result{Result: r.words[r.sessionID]}, nil
     }
     ```

     Drive the two sessions **sequentially** — `memoryRunner`'s shared map is not mutex-guarded, so parallel drives would be a `concurrent map writes` fatal rather than a meaningful assertion. Give session `session-a` the word `pelican` and session `session-b` the word `walrus`, then ask each. Assert `session-a`'s answer equals `"pelican"` and does not contain `"walrus"`, and `session-b`'s answer equals `"walrus"` and does not contain `"pelican"`. This fails if the handler ever hands the factory a constant instead of the caller's id, because both sessions would then write and read the same map key.

   - `It("serves different sessions concurrently and serialises one session", ...)` — the blocking `overlapRunner` already in the file, with a factory that returns the same runner instance for every id (the per-session lock, not the runner, is what serializes). Assert both halves in this one spec:
     - two requests with **different** ids: start the first in a goroutine, `Eventually(runner.entered).Should(Equal(1))`, start the second with a different id, `Eventually(runner.entered).Should(Equal(2))`, release, wait for both, and assert `runner.maxConcurrent()` is 2;
     - two requests with the **same** id on a fresh `overlapRunner`: start the first, `Eventually(runner.entered).Should(Equal(1))`, start the second with the same id, `Consistently(runner.entered, 200*time.Millisecond).Should(Equal(1))`, release, wait for both, and assert `runner.maxConcurrent()` is 1.

9. **Do not add, in this prompt:**
   - any bound, eviction, or LRU on the number of session ids — the spec puts bounding explicitly out of scope;
   - any authentication, token, or credential;
   - any new required environment variable or new `application` field;
   - any timeout around the runner call — the spec adds none, and the per-session lock already keeps a slow provider from blocking other sessions;
   - any test that inspects pi's argv or runs the `pi` binary. The pi library's own suite covers `--session-id`, and the spec assigns the live acceptance of an unseen session id to its operator rung;
   - any change to `CHANGELOG.md` — a later prompt in this spec owns that entry. `docs/dod.md` also requires a `## Unreleased` bullet; its absence from this diff is deliberate and is not a gap in this prompt's work.

10. **Document the new behavior in `README.md`, in this same change.** `docs/dod.md` requires that public HTTP behavior which changes — a new header, a new status code — be documented in `README.md` in the same commit, so the README edit belongs here rather than in a sibling prompt.

    In the `## Service Agents` section:
    - Name `X-Session-Id` as an optional request header carrying the conversation the prompt belongs to. Absent, the request is served from the default session exactly as before.
    - State the accepted format, `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}`, and that anything else is answered `400` before the agent process is invoked.
    - State the concurrency behavior explicitly, using the word `serial` or a form of `concurren`: requests on different session ids run at the same time, and requests on one session id are serialized — one turn at a time — because a session's transcript is a single store on the mounted volume, so two interleaved runs there would corrupt the continuity the session exists to keep.
    - Do not claim the header provides isolation from a caller that can reach the endpoint — a session id is an address, not a credential. Do not describe listing, renaming, or deleting sessions; the service cannot do those.
    - Keep the existing endpoint table and paragraphs; the section must still read as prose a reader who was not present for this change can follow.

11. **Before finishing**, re-read `docs/dod.md` and walk each of the six behaviors in the spec's Desired Behavior list against your change, then re-run `<verification>` and confirm every command passes.
</requirements>

<constraints>
- **Backward compatibility is absolute.** A request with no session header must behave exactly as it does today, including reaching the same conversation. Any existing caller must keep working untouched.
- The header name is `X-Session-Id`. It is part of the contract this spec establishes; callers depend on the exact spelling.
- The allowed session id format is `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}` — one to 64 characters, the first of which is not `-`. This is the security boundary and is not a style preference.
- The endpoint stays unauthenticated. No token, key, or credential is introduced.
- No new required environment variable. The service must start with the same configuration it starts with today.
- The request body limit (`maxPromptBytes`) and the response content type are unchanged.
- The default session id remains the value the service uses today, so an existing conversation is not orphaned by this change.
- Session ids are not persisted anywhere new. The service holds no registry of known sessions and no API to list, rename, or delete one.
- The prompt body is never logged, and neither is the session id, so the log cannot be used to enumerate live conversations. Today's handler logs the prompt's length and an eight-byte digest, never its text; keep that line byte-for-byte and add no session id to any log line.
- The existing behavior of returning HTTP 405 for a non-POST request and HTTP 400 for an empty prompt is preserved.
- The existing same-session serialization spec must keep passing. Its construction changes because the handler's runner argument changes shape; its assertion survives untouched.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run `make precommit` — must exit 0 with zero lint findings.

Run `make test` — must exit 0, and the `github.com/bborbe/agent-pi` line must read `ok`.

Each of these must print exactly `1`:

```
grep -c 'uses the default session when no session header is sent' main_internal_test.go
grep -c "passes the caller's session id to the runner" main_internal_test.go
grep -c 'rejects an invalid session id' main_internal_test.go
grep -c 'keeps two sessions apart' main_internal_test.go
grep -c 'serves different sessions concurrently and serialises one session' main_internal_test.go
```

Each of these must print at least `1`:

```
sed -n '/## Service Agents/,/## Env Vars/p' README.md | grep -c 'X-Session-Id'
sed -n '/## Service Agents/,/## Env Vars/p' README.md | grep -ciE 'serial|concurren'
```

These greps close the half of each acceptance criterion that `make test` cannot: this repo's `docs/dod.md` records that a spec whose suite entry point is missing is silently not discovered while `make test` still exits 0.
</verification>
