# Definition of Done — agent-pi

Self-review criteria for every change in this repo. Run this check after `make precommit` passes and before reporting the work complete.

## Correctness

- The change does what the spec or prompt asked, and nothing else. Unrelated edits are removed, not explained.
- Every new branch of behavior has a test that fails without the change. A test that passes before and after is not coverage.
- Tests assert behavior, not implementation shape. No test asserts on a private field, a log string used as a control signal, or a call count that only the current structure produces.

## Go conventions (this repo)

- Errors are wrapped with `github.com/bborbe/errors` (`errors.Wrap`, `errors.Wrapf`). `fmt.Errorf` is not used to construct errors, and bare `return err` is not used to propagate them.
- Logging uses `glog`. New informational logs are gated at `V(2)` or higher unless they mark a state change an operator needs at default verbosity.
- Constructor and factory functions follow the repo's existing split: `Create*` performs wiring only and returns an interface; business logic does not live there.
- Every exported type, function, and interface has a GoDoc comment that says why it exists, not what its signature already says.
- New interfaces carry a `//counterfeiter:generate` directive when the repo mocks their siblings.
- Test files follow the existing split: `main_test.go` is external (`package main_test`), `main_internal_test.go` is `package main` and covers unexported symbols. A new package needs its suite entry point (`RunSpecs`) or its specs are silently not discovered and `make test` still exits 0.

## Behavior and contracts

- Public HTTP behavior that changes — a new header, a new status code, a changed response body — is documented in `README.md` in the same commit. A reader who only reads the README must not be surprised by the service.
- Configuration read from the environment keeps a working default. A new required variable is a breaking change to every existing deployment and is called out as such.
- Concurrency behavior is stated explicitly, not left to be inferred: if requests are serialized, the code says so and says why; if they run in parallel, the code names what protects shared state.

## Repository hygiene

- `CHANGELOG.md` carries an `## Unreleased` entry describing the change in one line. Do not rename `## Unreleased` and do not create a version tag — the release bot owns both.
- No daemon artifacts, editor files, or scratch output are committed. Build products and local logs stay out of the diff.
- The diff contains no commented-out code, no debug prints, and no `TODO` without a linked issue.

## Verification

- `make precommit` exits 0 with zero lint findings.
- Any claim in the completion report that a behavior works is backed by a command whose output is quoted, not by a description of the code.
