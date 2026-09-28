---
status: verifying
approved: "2026-09-28T20:59:12Z"
generating: "2026-09-28T20:59:12Z"
prompted: "2026-09-28T21:18:55Z"
verifying: "2026-09-28T21:36:40Z"
branch: dark-factory/per-caller-session-id
---

## Summary

- A service agent's prompt endpoint serves exactly one conversation today, shared by every caller.
- This spec adds an optional request header naming which conversation a prompt belongs to.
- A caller that sends no header keeps today's behavior exactly: the same conversation they have always used.
- A caller that sends a session id gets its own conversation, independent of every other session id.
- Two different session ids are served at the same time, and neither sees the other's context.

## Problem

A service agent exists so a person can talk to a durable agent. Today every caller shares one conversation: the session id is a compiled-in constant, so the second person to address the service joins the first person's thread and reads context they never provided. The service cannot be used by more than one person, and cannot be demonstrated to a second person without showing them someone else's conversation. Addressing that requires the caller to say which conversation it wants, and the service to keep those conversations apart.

## Goal

A caller can address a service agent with a session id of its choosing and hold an independent conversation, while every existing caller that sends no session id continues to reach the conversation it reaches today. Two sessions are served concurrently. The session id is validated before it reaches the agent process.

## Non-goals

- Authentication or authorization. The endpoint stays unauthenticated and reachable only from inside the namespace.
- An Ingress, TLS, or any change to how the service is reached.
- Enumerating, listing, renaming, or deleting sessions. A caller addresses a session; it cannot manage one.
- Session affinity across replicas. The service runs as a single instance with one volume; a second replica is out of scope.
- Changing the prompt request or response format. The body stays the prompt, the response stays plain text.
- Bounding the number of distinct sessions. Any caller that can reach the endpoint can start a new conversation; that is inherent to an unauthenticated service and is not solved here.

## Assumptions

Each of these is load-bearing. If one is false, the spec is wrong rather than merely incomplete.

- **The agent process creates a session on first use.** Passing a session id that has never been seen starts a fresh conversation rather than failing. This is the reason no registration step exists anywhere in this design; it is stated in the current code's own rationale for the default session constant.
- **The session id reaches the agent process as a distinct argument element**, with no shell interpolation between the HTTP handler and the process invocation.
- **The service runs as a single instance against a single volume.** Two instances sharing one volume is out of scope, and per-session isolation assumes one writer per session id.
- **The session store is per-session.** One session id's conversation cannot be read through another session id's store.
- **Existing callers send no session header.** Backward compatibility is defined against that population; a caller that already sends an `X-Session-Id` header today does not exist.
- **The deployment is not defined by this repo.** The workload, its name, and its image pin live in the `nuke` config repo; this repo ships the binary and its image. The operator rung reads deployed state that this repo does not define.

## Acceptance Criteria

- [ ] A request with no session header is answered from the conversation the service used before this change — evidence: a Ginkgo spec named `uses the default session when no session header is sent` asserts the runner was built with the pre-existing default session id (not with an empty id, and not with a new one), and `make test` exits 0.
- [ ] A request carrying a valid session header builds a runner for exactly that session id — evidence: a Ginkgo spec named `passes the caller's session id to the runner` asserts the session id handed to the runner factory equals the header value verbatim, and `make test` exits 0.
- [ ] A session id outside the allowed format is rejected with HTTP 400 before the agent process is invoked — evidence: a Ginkgo spec named `rejects an invalid session id` asserts status 400 and zero runner-factory calls for each of: a 65-character id, an id containing `/`, an id containing `.`, an empty header value, and an id whose first character is `-`; `make test` exits 0.
- [ ] Two different session ids hold independent conversations — evidence: a Ginkgo spec named `keeps two sessions apart` drives a fake runner keyed by session id, gives session `A` the word `pelican` and session `B` the word `walrus`, and asserts that asking each returns its own word and never the other's; `make test` exits 0.
- [ ] Requests on different session ids are served concurrently, and requests on the same session id do not overlap — evidence: a Ginkgo spec named `serves different sessions concurrently and serialises one session` uses a blocking fake runner to assert that two requests on different ids are both in flight at the same time, and that two requests on the same id are never in flight together; `make test` exits 0.
- [ ] `README.md` documents the session header and states the concurrency behavior — evidence: the section extracted by `sed -n '/## Service Agents/,/## Env Vars/p' README.md` contains at least one `X-Session-Id` occurrence and matches `serial` or `concurren` (case-insensitive).
- [ ] `CHANGELOG.md` carries the change under `## Unreleased` — evidence: the section extracted by `sed -n '/^## Unreleased/,/^## /p' CHANGELOG.md` contains at least one bullet beginning `feat:` that names the session header. A bullet describing unrelated work does not satisfy this.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — exits 0, zero lint findings, gosec/trivy/osv clean
- `make test` — exits 0; a green run prints `ok github.com/bborbe/agent-pi`
- `grep -c 'uses the default session when no session header is sent' main_internal_test.go` — returns 1
- `grep -c "passes the caller's session id to the runner" main_internal_test.go` — returns 1
- `grep -c 'rejects an invalid session id' main_internal_test.go` — returns 1
- `grep -c 'keeps two sessions apart' main_internal_test.go` — returns 1
- `grep -c 'serves different sessions concurrently and serialises one session' main_internal_test.go` — returns 1
- `sed -n '/## Service Agents/,/## Env Vars/p' README.md | grep -c 'X-Session-Id'` — returns at least 1
- `sed -n '/## Service Agents/,/## Env Vars/p' README.md | grep -ciE 'serial|concurren'` — returns at least 1

Each `grep -c` line closes the half of an AC's evidence that `make test` cannot: this repo's own `docs/dod.md` records that a spec whose suite entry point is missing is silently not discovered while `make test` still exits 0.

### Operator-executable (runs on the host after PR merge, spec verification ladder)

The service has no Ingress, so every HTTP command below is issued through a port-forward to the pod, approved for that turn. The pod name is resolved from the deployment in the `nuke` config repo rather than assumed; `pi-service-0` is the expected StatefulSet ordinal.

- `kubectlnukedev -n dev get pods | grep pi-service` — returns `1/1 Running`
- `kubectlnukedev -n dev get pod pi-service-0 -o jsonpath='{.spec.containers[0].image}'` — returns the release tag cut from this change
- `kubectlnukedev -n dev port-forward pod/pi-service-0 9090:9090` (operator-approved for the turn, running in a second shell)
- `curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'X-Session-Id: session-a' --data 'remember the word pelican' localhost:9090/prompt` — returns `200`
- `curl -sS -X POST -H 'X-Session-Id: session-a' --data 'what word did I give you?' localhost:9090/prompt` — returns a body naming `pelican`
- `curl -sS -X POST -H 'X-Session-Id: session-b' --data 'what word did I give you?' localhost:9090/prompt` — returns a body that does not name `pelican`
- `curl -sS -o /dev/null -w '%{http_code}' -X POST -H "X-Session-Id: $(printf 'a%.0s' {1..65})" --data 'x' localhost:9090/prompt` — returns `400`
- `curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'X-Session-Id: -dash' --data 'x' localhost:9090/prompt` — returns `400`
- `kubectlnukedev -n dev logs pi-service-0 --since=5m | grep -ciE 'panic|fatal|corrupt'` — returns `0`

This rung is load-bearing, not optional: the Ginkgo specs drive a fake runner, so real conversation isolation and the agent process's own acceptance of an unseen session id are evidenced here and nowhere else. Both `v0.4.0` and `v0.4.2` in `CHANGELOG.md` record session-continuity defects that only a live run surfaced.

## Desired Behavior

1. A request with no session header is served from the default session, unchanged from the current behavior. Existing callers observe no difference.
2. A request whose session header matches the allowed format is served from that session. The id reaches the agent process unmodified.
3. A request whose session header is empty, longer than 64 characters, begins with `-`, or contains a character outside `A-Z a-z 0-9 _ -` is rejected with HTTP 400. The agent process is not invoked.
4. Two requests naming different session ids are served concurrently. Neither waits for the other.
5. Two requests naming the same session id are served one at a time. The second does not begin until the first has finished.
6. A session id that has never been used before starts a new conversation. The service does not need to be told about sessions in advance.

## Constraints

- **Backward compatibility is absolute.** A request with no session header must behave exactly as it does today, including reaching the same conversation. Any existing caller must keep working untouched.
- **The header name is `X-Session-Id`.** It is part of the contract this spec establishes; callers depend on the exact spelling.
- **The allowed session id format is `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}`** — one to 64 characters, the first of which is not `-`. This is the security boundary (see Security / Abuse) and is not a style preference.
- The endpoint stays unauthenticated. No token, key, or credential is introduced.
- No new required environment variable. The service must start with the same configuration it starts with today.
- The request body limit and the response content type are unchanged.
- The default session id remains the value the service uses today, so an existing conversation is not orphaned by this change.
- Session ids are not persisted anywhere new. The service holds no registry of known sessions.
- The prompt body is never logged, and neither is the session id, so the log cannot be used to enumerate live conversations. This is a preservation invariant: today's handler logs the prompt's length and an eight-byte digest, never its text.
- The existing behavior of returning HTTP 405 for a non-POST request and HTTP 400 for an empty prompt is preserved.
- **The existing same-session serialization test must keep passing.** The current suite asserts that two concurrent prompts on one session do not interleave. Its construction changes with this work because the handler's runner argument changes shape, but its assertion survives untouched: two requests with no session header both land on the default session, which behavior 5 still serializes.
- **The deployment is not in this repo.** The workload, its name, and its image pin live in the `nuke` config repo; this repo ships the binary and its image. The spec's operator rung reads the deployed state, which this repo does not define.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Session header fails the format check | HTTP 400, agent process not invoked, no session created | Caller retries with a corrected id: the same request with `-H 'X-Session-Id: session-a'` returns 200 |
| Agent process fails mid-prompt | HTTP 500; the session's conversation is left as the process left it | Caller retries on the same session id: the retry resumes that conversation and returns 200 |
| Provider unreachable or slow | The prompt hangs holding that session's turn; other session ids continue to be served | Caller retries after the provider recovers: the same request returns 200 and the reply names the token set earlier |
| Pod restarts mid-prompt | The in-flight prompt is lost; the session store on the volume may hold a partial turn | Caller retries on the same session id; if the resumed conversation returns a reply that does not name the earlier token, the caller addresses a new session id |
| Many distinct session ids are used | Each id holds a runner and a session store on the single shared volume; memory and disk grow with the number of distinct ids | Operator restarts the pod, which drops in-memory runners; the volume retains the stores. Bounding this is out of scope (see Non-goals) |
| Two requests on one session id arrive together | Served sequentially; both complete | None needed — this is the intended path |
| A session id names an unused conversation | A new conversation starts | None needed — this is the intended path |

## Security / Abuse

The session id is caller-supplied and reaches a subprocess as a command-line argument. It is the only new attacker-controlled input on this surface, and the endpoint is unauthenticated.

- **Argument injection.** A value that begins with `-` can be parsed by the agent's CLI as a flag rather than as the value of the session argument. The format's first character is therefore restricted to `[A-Za-z0-9_]`, which excludes a leading `-`. The remaining characters exclude whitespace, quotes, and shell metacharacters.
- **Path traversal.** If the session id is used to build a storage path, `.` or `/` would allow escaping the session directory. The allowed character set excludes both.
- **Unbounded input.** The 64-character maximum bounds the value. It does not bound the number of distinct ids; see Non-goals and the resource row in Failure Modes.
- **Conversation disclosure.** Session ids are not secrets and are not treated as such: a caller who guesses another caller's session id reaches that conversation. This is accepted, because the endpoint is reachable only from inside the namespace and has no authentication at all — a session id is an address, not a credential. This spec does not change that posture, and no AC claims otherwise.
- **Log exposure.** The prompt body is not logged, and neither is the session id, so the log cannot be used to enumerate live conversations.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Session id intake: header parse, format validation, and the rejection path with its five invalid-input cases | 1, 2, 3 | 1, 2, 3 | — |
| 2 | Per-session runner lifecycle and the concurrency primitive that replaces the single global mutex | 4, 5, 6 | 4, 5 | prompt 1 (consumes the parsed id) |
| 3 | README section documenting the header and the concurrency behavior | — | 6 | prompt 2 (documents settled behavior) |
| 4 | CHANGELOG `## Unreleased` entry | — | 7 | — |

Rationale: prompt 1 settles the intake contract and can be verified alone for AC1 and AC3. It also introduces the runner-factory seam, because AC2's evidence observes the id at that seam — but the factory is not yet per-session, and the handler still holds one runner. Prompt 2 makes the factory genuinely per-session and replaces the global mutex, which is what AC4 and AC5 measure; it is the only part that changes existing concurrency behavior, so it is isolated for review. Prompts 3 and 4 are documentation and depend on the behavior being settled.

## Do-Nothing Option

The service keeps one shared conversation. It cannot be used by two people, cannot be demonstrated to a second person without exposing the first person's context, and every future front end — a test page, Open WebUI, Google Chat — inherits the same single-caller limit. The cost of not doing this is that the service remains a single-user prototype, and the multi-user work that depends on it cannot start.
