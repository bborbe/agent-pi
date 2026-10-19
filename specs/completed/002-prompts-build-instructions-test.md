---
status: completed
approved: "2026-10-19T21:06:37Z"
generating: "2026-10-19T21:19:35Z"
prompted: "2026-10-19T21:22:30Z"
verifying: "2026-10-19T21:39:32Z"
completed: "2026-10-19T21:39:37Z"
branch: dark-factory/prompts-build-instructions-test
---

## Summary

- Every agent-pi run receives its instructions assembled from two embedded markdown modules, and that assembly step has no test at all.
- This spec adds the package's first test: it asserts the assembled instructions carry both modules, in the workflow-before-output-format order.
- No production code changes. The assembly function, the two embedded markdown files, and every other package are untouched.
- The test is the whole deliverable: it is the regression lock for a step whose output no other test observes.

## Problem

The instruction-assembly step is the single place that decides what instructions an agent-pi run receives, and it is called from the factory wiring for every task-routed and service agent. If a future edit drops an embed directive, swaps the concatenation order, or replaces a module with an empty string, every agent still starts and still returns syntactically valid JSON — the failure is silent, and nothing in the suite would notice. The package currently contributes zero specs to `make test`, so `make test` exits 0 with the assembly entirely unobserved. Verified: `go test ./pkg/prompts/...` reports `[no test files]`.

## Goal

After this work, the prompts package has a Ginkgo suite that fails when the assembled instructions stop carrying either module, and fails when the output-format module is placed before the workflow module. The production code and the embedded markdown are byte-identical to before.

## Non-goals

- Do NOT change the assembly function's implementation, signature, or separator.
- Do NOT edit `workflow.md` or `output-format.md`, including their trailing newlines.
- Do NOT add an exported seam (a variable, a constructor, or a function parameter) to make the modules injectable — the test asserts the real embedded output.
- Do NOT touch `pkg/factory`, `main.go`, `cmd/`, or any other package.
- Do NOT add a `docs/` page; the test and its GoDoc are the documentation.
- Do NOT add a scenario; the behavior is a pure string assembly reachable by a unit test.
- Do NOT correct the stale `README.md` line that describes prompt assembly — that is a separate doc fix (see Constraints).

## Acceptance Criteria

- [ ] The package's suite is discovered and passes — evidence: `go test ./pkg/prompts/... -count=1 -v` exits 0 and its output contains `Running Suite`.
- [ ] The test asserts the assembled output carries the workflow module — evidence: `grep -nE 'ContainSubstring\("task execution agent"\)' pkg/prompts/prompts_test.go` returns ≥1 line.
- [ ] That workflow assertion is behavior-level, not decorative — evidence: this probe exits **0** (the mutated run fails and its log names the marker); a decorative assertion leaves `$rc` at 0 and the probe exits non-zero.
  ```bash
  bak=$(mktemp); cp pkg/prompts/workflow.md "$bak"
  trap 'cp "$bak" pkg/prompts/workflow.md' EXIT
  printf '\n' > pkg/prompts/workflow.md
  go test ./pkg/prompts/... -count=1 > /tmp/mut-workflow.log 2>&1; rc=$?
  cp "$bak" pkg/prompts/workflow.md; trap - EXIT
  test $rc -ne 0 && grep -q 'task execution agent' /tmp/mut-workflow.log
  ```
- [ ] The test asserts the assembled output carries the output-format module — evidence: `grep -nE 'ContainSubstring\("single JSON object"\)' pkg/prompts/prompts_test.go` returns ≥1 line.
- [ ] That output-format assertion is behavior-level — evidence: this probe exits **0** (the mutated run fails and its log names the marker).
  ```bash
  bak=$(mktemp); cp pkg/prompts/output-format.md "$bak"
  trap 'cp "$bak" pkg/prompts/output-format.md' EXIT
  printf '\n' > pkg/prompts/output-format.md
  go test ./pkg/prompts/... -count=1 > /tmp/mut-output.log 2>&1; rc=$?
  cp "$bak" pkg/prompts/output-format.md; trap - EXIT
  test $rc -ne 0 && grep -q 'single JSON object' /tmp/mut-output.log
  ```
- [ ] The test asserts the workflow module precedes the output-format module — evidence: `grep -nE 'BeNumerically\("<"' pkg/prompts/prompts_test.go` returns ≥1 line.
- [ ] That order assertion is behavior-level — evidence: this order-swap probe exits **0** (the mutated run fails); a tautological comparison leaves `$rc` at 0 and the probe exits non-zero.
  ```bash
  bak=$(mktemp); cp pkg/prompts/prompts.go "$bak"
  trap 'cp "$bak" pkg/prompts/prompts.go' EXIT
  sed -i '' 's/workflow + "\\n\\n" + outputFormat/outputFormat + "\\n\\n" + workflow/' pkg/prompts/prompts.go
  grep -q 'outputFormat + "\\n\\n" + workflow' pkg/prompts/prompts.go
  go test ./pkg/prompts/... -count=1 > /tmp/mut-order.log 2>&1; rc=$?
  cp "$bak" pkg/prompts/prompts.go; trap - EXIT
  test $rc -ne 0 && grep -q 'to be less than' /tmp/mut-order.log
  ```
- [ ] The production files are unchanged against the base branch — evidence: `git diff --exit-code origin/master...HEAD -- pkg/prompts/prompts.go pkg/prompts/workflow.md pkg/prompts/output-format.md` exits 0 (the three-dot form compares against the merge base, so the prompt's own commit cannot mask a change).
- [ ] `make precommit` exits 0 — evidence: `make precommit > /tmp/precommit.log 2>&1; echo $?` prints `0`.
- [ ] `CHANGELOG.md` carries an `## Unreleased` entry naming the added test — evidence: `awk '/^## /{print; exit}' CHANGELOG.md` prints `## Unreleased` AND `awk '/^## /{sec=$0} /prompts/{print sec; exit}' CHANGELOG.md` prints `## Unreleased`.

## Verification

```bash
go test ./pkg/prompts/... -count=1 -v
grep -nE 'ContainSubstring\("task execution agent"\)' pkg/prompts/prompts_test.go
grep -nE 'ContainSubstring\("single JSON object"\)' pkg/prompts/prompts_test.go
grep -nE 'BeNumerically\("<"' pkg/prompts/prompts_test.go
# AC3 / AC5 / AC7 mutation probes — run each verbatim from Acceptance Criteria;
# each restores its file through a trap and must exit 0.
git diff --exit-code origin/master...HEAD -- pkg/prompts/prompts.go pkg/prompts/workflow.md pkg/prompts/output-format.md
make precommit > /tmp/precommit.log 2>&1; echo $?
awk '/^## /{print; exit}' CHANGELOG.md
awk '/^## /{sec=$0} /prompts/{print sec; exit}' CHANGELOG.md
```

Success is the prompts suite reported as passed, all three greps returning ≥1 line, each mutation probe exiting 0, an empty diff on the three production files, `make precommit` printing `0`, and both CHANGELOG checks printing `## Unreleased`.

## Desired Behavior

1. `go test ./pkg/prompts/...` runs a Ginkgo suite named for the prompts package (not `[no test files]`).
2. The suite asserts that the assembled instructions contain the workflow module's opening sentence and the output-format module's schema sentence.
3. The suite asserts that the offset of the workflow marker is strictly less than the offset of the output-format marker, so a reordered concatenation fails.
4. The production files `pkg/prompts/prompts.go`, `pkg/prompts/workflow.md`, and `pkg/prompts/output-format.md` are unchanged in the diff.
5. `CHANGELOG.md` gains an `## Unreleased` section with one line naming the added test.

## Constraints

- Test style follows the repository's existing Ginkgo v2 / Gomega convention: a `_suite_test.go` file with `RunSpecs`, and a `_test.go` file with the specs in an external `package prompts_test` (see `pkg/factory/factory_suite_test.go`). See `docs/dod.md` § "Go conventions (this repo)" — a new package needs its suite entry point or its specs are silently not discovered.
- Marker substrings are drawn from the embedded markdown verbatim, so a legitimate reflow of a module does not fail the test as long as its opening sentence and schema sentence survive.
- The test must not read the files from disk; it exercises the assembly function only, so it also fails if an embed directive is dropped.
- No new dependency is added; Ginkgo and Gomega are already in `go.mod`.
- The copyright header used by every Go file in this repo is reproduced in both new files.
- `CHANGELOG.md` keeps its existing `## v0.6.1` heading and gains `## Unreleased` above it; see `docs/dod.md` § "Repository hygiene".
- `README.md` line 11 states that `main.go` assembles the prompt via `lib/pi`; the actual path is the assembly function called from `pkg/factory/factory.go`. Correcting that sentence is out of scope for this spec and is recorded as a follow-up, not silently changed.

## Failure Modes

| Trigger | Expected behavior | Recovery | Detection |
|---------|-------------------|----------|-----------|
| An embed directive is removed | The test fails on the missing marker substring | Restore the directive; `go test ./pkg/prompts/...` exits 0 | `go test ./pkg/prompts/...` exits non-zero |
| The concatenation order is swapped | The test fails on the index comparison | Restore the order; `go test ./pkg/prompts/...` exits 0 | AC7's order-swap probe exits 0 |
| A module file is emptied | The test fails on its marker substring | Restore the file; `go test ./pkg/prompts/...` exits 0 | AC3 / AC5 probes exit 0 |
| A module's opening sentence is reworded | The test fails even though behavior is intact | Update the marker substring in the same commit; `go test ./pkg/prompts/...` exits 0 | `go test ./pkg/prompts/...` exits non-zero |
| Suite entry point omitted | `go test` reports `[no test files]` and exits 0 | Add the `_suite_test.go` with `RunSpecs`; AC1's `Running Suite` check passes | AC1's `Running Suite` check |
| A mutation probe is interrupted (SIGKILL, closed shell) | A production file is left mutated | `git restore pkg/prompts/workflow.md pkg/prompts/output-format.md pkg/prompts/prompts.go`; `go test ./pkg/prompts/...` exits 0 | `git status --porcelain pkg/prompts/` is non-empty |

## Assumptions

- Ginkgo v2 and Gomega are available to the package without a `go.mod` change (they are already direct dependencies of `pkg/factory`).
- The two marker sentences are stable enough to pin: they are the first line of `workflow.md` and the first line of `output-format.md`'s schema paragraph. Failure Modes row 4 names the cost of a reword.
- `make test` discovers `pkg/prompts` once the suite entry point exists; `docs/dod.md` states this is the failure mode of omitting it.
- The mutation probes run on macOS, where `sed -i ''` is the in-place form; they are verification-time probes, never committed.

## Alternatives Considered

- **Golden-file comparison of the full assembled output.** Rejected: it fails on any legitimate markdown reflow, and the spec explicitly freezes the module files, so the golden file would be a second copy of content the repo already holds.
- **Exported seam for injectable modules.** Rejected in Non-goals: it changes production code for a test's convenience, and the task is to observe the real embedded output.
- **A scenario instead of a unit test.** Rejected: the assembly is a pure string function with no runtime dependency; `docs/rules/scenario-writing.md` conditions do not hold.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Add the prompts suite and its specs | 1-5 | 1-10 | — |

Rationale: a single-layer, single-behavior change; one prompt is sufficient and the spec must not generate more.

## Do-Nothing Option

The assembly step stays untested. Every future edit to the embedded modules or to the concatenation is unobserved by the suite, and a silently broken instruction assembly is discovered only by an agent run returning `failed` for reasons no test explains. The cost of this spec is two small test files and one CHANGELOG line.
