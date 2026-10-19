---
status: completed
spec: [002-prompts-build-instructions-test]
summary: Added Ginkgo v2 test suite to pkg/prompts with three specs pinning the workflow-before-output-format concatenation order of BuildInstructions(), and updated CHANGELOG.md with an Unreleased test entry.
execution_id: 079e6ee0-9582-59b9-a8ee-c00a72d02b61-exec-005-spec-002-prompts-build-instructions-test
dark-factory-version: dev
created: "2026-10-19T22:00:00Z"
queued: "2026-10-19T21:24:12Z"
started: "2026-10-19T21:24:14Z"
completed: "2026-10-19T21:39:31Z"
branch: dark-factory/prompts-build-instructions-test
---

# Add the prompts package's first Ginkgo test suite

<summary>
- The `pkg/prompts` package gains its first test files, so `go test ./pkg/prompts/...` no longer reports `[no test files]`.
- The suite asserts that the instructions assembled by `prompts.BuildInstructions()` carry both embedded modules in the workflow-before-output-format order.
- The test exercises the real assembly function with the real embedded bytes; it does not parameterize the modules and does not read the files from disk.
- A future edit that drops an embed directive, swaps the concatenation order, or empties a module now fails the suite with a named marker.
- Production code in `pkg/prompts/prompts.go`, `pkg/prompts/workflow.md`, and `pkg/prompts/output-format.md` is byte-identical to before.
- The CHANGELOG gains an `## Unreleased` entry naming the added test.
- No other package, no `main.go`, and no other artifact in the repository is touched.

</summary>

<objective>
Add a Ginkgo v2 / Gomega test suite to `pkg/prompts/` that locks the contract of `prompts.BuildInstructions()` — it must carry the workflow module followed by the output-format module — so a future edit that drops an embed directive, swaps the concatenation order, or replaces a module with an empty string fails the suite. The deliverable is the package's first test; production code, the embedded markdown, every other package, `main.go`, `cmd/`, and `README.md` are untouched.

</objective>

<context>
Read these before writing any code:

- `pkg/prompts/prompts.go` — 20 lines. Holds the two `//go:embed` directives (`workflow.md`, `output-format.md`) and the `BuildInstructions() string` function that returns `workflow + "\n\n" + outputFormat`. This file is frozen — every byte stays exactly as it is.
- `pkg/prompts/workflow.md` — frozen. Its first line is `You are a task execution agent.`, which is the marker substring the new suite pins (per spec AC2/AC3).
- `pkg/prompts/output-format.md` — frozen. Its first paragraph contains `Your final response MUST be a single JSON object with this schema`, which is the marker substring the new suite pins (per spec AC4/AC5).
- `pkg/factory/factory_suite_test.go` — the sibling `_suite_test.go` to mirror. It is the file in this repository that demonstrates the `_suite_test.go` + external `package factory_test` pattern, sets `time.Local = time.UTC` and `format.TruncatedDiff = false`, registers the fail handler, and calls `RunSpecs(t, "Factory Suite")`. Use it as the structural template.
- `pkg/factory/factory_test.go` — the sibling `_test.go` to mirror. It shows the `package factory_test` external test package, the `var _ = Describe(...)` anchor at file scope, the Ginkgo/Gomega dot-imports, and the `BeNumerically` / `ContainSubstring` matcher style used in this repository.
- `CHANGELOG.md` — the existing `## v0.6.1` heading stays. The spec requires an `## Unreleased` section added above it with one bullet naming the added test.
- `/home/node/.claude/plugins/marketplaces/coding/docs/dod.md` — the dark-factory validation prompt. Its `Repository hygiene` section pins the `## Unreleased` placement and conventional prefix rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — the `## Unreleased` section shape and the conventional prefix rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — the Ginkgo v2 / Gomega suite shape for this repository.

The marker substrings come verbatim from the embedded markdown. A legitimate reflow of a module will not fail the test as long as its opening sentence (`workflow.md` line 1) and its schema sentence (`output-format.md` paragraph 1) survive.

</context>

<requirements>

1. **Create `pkg/prompts/prompts_suite_test.go`.** This is the suite entry point — without it, `make test` reports `[no test files]` for `pkg/prompts/...` and the specs are silently not discovered. Mirror `pkg/factory/factory_suite_test.go` exactly. The file MUST contain, in this order:

   - The copyright header reproduced verbatim from every Go file in this repository:
     ```
     // Copyright (c) 2026 Benjamin Borbe All rights reserved.
     // Use of this source code is governed by a BSD-style
     // license that can be found in the LICENSE file.
     ```
   - `package prompts_test` (external test package, separate from `package prompts`).
   - Imports: `"testing"`, `"time"`, `. "github.com/onsi/ginkgo/v2"`, `. "github.com/onsi/gomega"`, `"github.com/onsi/gomega/format"`.
   - One function:
     ```go
     func TestPrompts(t *testing.T) {
         time.Local = time.UTC
         format.TruncatedDiff = false
         RegisterFailHandler(Fail)
         RunSpecs(t, "Prompts Suite")
     }
     ```
   - The `TestPrompts` name is what `go test ./pkg/prompts/...` invokes. Do NOT use `TestSuite` — that name is already used by `main_test.go` and would shadow the suite entry point this binary already has. Do NOT add a second `RunSpecs` call anywhere in the package. Do NOT add a `//go:generate` counterfeiter directive — this package declares no interface and uses no mock.

2. **Create `pkg/prompts/prompts_test.go`.** This holds the specs. The file MUST contain:

   - The same copyright header reproduced verbatim.
   - `package prompts_test`.
   - Imports: `"strings"`, `. "github.com/onsi/ginkgo/v2"`, `. "github.com/onsi/gomega"`, `"github.com/bborbe/agent-pi/pkg/prompts"`.
   - One `var _ = Describe("BuildInstructions", func() { ... })` anchor at file scope. Inside it, exactly these three `It` blocks, in this order:

   ```go
   It("contains the workflow module", func() {
       Expect(prompts.BuildInstructions()).To(ContainSubstring("task execution agent"))
   })

   It("contains the output-format module", func() {
       Expect(prompts.BuildInstructions()).To(ContainSubstring("single JSON object"))
   })

   It("places the workflow module before the output-format module", func() {
       instructions := prompts.BuildInstructions()
       Expect(strings.Index(instructions, "task execution agent")).
           To(BeNumerically("<", strings.Index(instructions, "single JSON object")))
   })
   ```

   The marker substrings come from the embedded markdown verbatim:
   - `"task execution agent"` is the unique fragment of `workflow.md`'s first sentence (`You are a task execution agent.`).
   - `"single JSON object"` is the unique fragment of `output-format.md`'s schema sentence (`Your final response MUST be a single JSON object with this schema`).

   Do NOT add a fourth `It`, a `Context`, a `DescribeTable`, a `BeforeEach`, a `JustBeforeEach`, or any other Ginkgo construct. The contract is exactly these three assertions, no more. Do NOT add a spec that reads `workflow.md` or `output-format.md` from disk — the test must fail if an embed directive is dropped, and a disk read would not. Do NOT add a spec that asserts on the separator (`"\n\n"`) — the order assertion already pins the contract and asserting on the separator would couple the test to the implementation, which is a non-goal.

3. **Add a `## Unreleased` section to `CHANGELOG.md`** directly above the highest released heading (currently `## v0.6.1`). Place it AFTER the preamble block (`# Changelog`, "All notable changes..." paragraph, SemVer link, the MAJOR/MINOR/PATCH bullets) and BEFORE `## v0.6.1`. The new section MUST contain exactly one line, in this form:

   ```
   ## Unreleased

   - test: Add Ginkgo suite for pkg/prompts asserting the assembled instructions carry workflow.md before output-format.md

   ## v0.6.1
   ```

   The prefix `test:` is required (not `feat:` — the deliverable is a test, not a feature). Do NOT touch the preamble, do NOT touch `## v0.6.1`, do NOT add a second bullet, do NOT invent a version number, and do NOT correct the stale `README.md` line about prompt assembly — that fix is explicitly out of scope for this spec.

4. **Self-check before finishing.** Run every command in `<verification>` and confirm each one passes, quoting the actual output. Then walk every line of the spec's Acceptance Criteria against the change, one by one.

</requirements>

<constraints>

- Do NOT commit — dark-factory handles git.
- Do NOT edit `pkg/prompts/prompts.go`, `pkg/prompts/workflow.md`, or `pkg/prompts/output-format.md`, including their trailing newlines. They are byte-identical to before.
- Do NOT edit `pkg/factory`, `main.go`, `cmd/`, or any other package.
- Do NOT add an exported seam — no new variable, constructor, or function parameter — to make the modules injectable. The test asserts the real embedded output.
- Do NOT add a `docs/` page; the test and its GoDoc are the documentation.
- Do NOT correct the stale `README.md` line about prompt assembly; that is a separate follow-up.
- Do NOT add a new dependency. Ginkgo v2 (`v2.32.1`) and Gomega (`v1.43.0`) are already direct dependencies in `go.mod`.
- The copyright header used by every Go file in this repo is reproduced verbatim at the top of both new files.
- No daemon artifacts, editor files, build products or scratch output are committed.
- Existing tests must still pass.
- Do NOT add a `//go:generate` counterfeiter directive to either new file — this package declares no interface.
- Do NOT add a second `RunSpecs` anywhere in the package.

</constraints>

<verification>

Run each command below. All must pass. Quote the actual output of each in the completion report.

1. The suite is discovered and passes:

   ```bash
   go test ./pkg/prompts/... -count=1 -v
   ```

   Expected: exits 0. The output MUST contain the literal `Running Suite` (printed by Ginkgo v2 when a suite starts).

2. The three assertion markers are present in the test file. Each grep must return at least one line:

   ```bash
   grep -nE 'ContainSubstring\("task execution agent"\)' pkg/prompts/prompts_test.go
   grep -nE 'ContainSubstring\("single JSON object"\)' pkg/prompts/prompts_test.go
   grep -nE 'BeNumerically\("<"' pkg/prompts/prompts_test.go
   ```

3. The production files are unchanged against the base branch:

   ```bash
   git diff --exit-code origin/master...HEAD -- pkg/prompts/prompts.go pkg/prompts/workflow.md pkg/prompts/output-format.md
   ```

   Expected: exits 0. The three-dot form compares against the merge base, so this prompt's own commit cannot mask a change. If `origin/master` is not reachable inside the container, fall back to `git diff --exit-code HEAD -- pkg/prompts/prompts.go pkg/prompts/workflow.md pkg/prompts/output-format.md` and verify the only changes in the working tree are the two new files and the CHANGELOG diff.

4. Mutation probes — run each one verbatim from the spec's Acceptance Criteria; each restores its file through a trap and must exit 0:

   ```bash
   bak=$(mktemp); cp pkg/prompts/workflow.md "$bak"
   trap 'cp "$bak" pkg/prompts/workflow.md' EXIT
   printf '\n' > pkg/prompts/workflow.md
   go test ./pkg/prompts/... -count=1 > /tmp/mut-workflow.log 2>&1; rc=$?
   cp "$bak" pkg/prompts/workflow.md; trap - EXIT
   test $rc -ne 0 && grep -q 'task execution agent' /tmp/mut-workflow.log
   ```

   ```bash
   bak=$(mktemp); cp pkg/prompts/output-format.md "$bak"
   trap 'cp "$bak" pkg/prompts/output-format.md' EXIT
   printf '\n' > pkg/prompts/output-format.md
   go test ./pkg/prompts/... -count=1 > /tmp/mut-output.log 2>&1; rc=$?
   cp "$bak" pkg/prompts/output-format.md; trap - EXIT
   test $rc -ne 0 && grep -q 'single JSON object' /tmp/mut-output.log
   ```

   The order-swap probe from the spec uses macOS-only `sed -i ''` and may fail on Linux. On Linux the equivalent is `sed -i 's/workflow + "\\n\\n" + outputFormat/outputFormat + "\\n\\n" + workflow/' pkg/prompts/prompts.go`. If the probe cannot run cleanly on the host platform, confirm by inspection that swapping the operands of the `+` expression in `BuildInstructions()` would cause `strings.Index(instructions, "task execution agent")` to return a value larger than the output-format index, which is what `BeNumerically("<", ...)` rejects. The remaining probes above are platform-independent.

5. `make precommit` exits 0:

   ```bash
   make precommit > /tmp/precommit.log 2>&1; echo $?
   ```

   Expected: prints `0`.

6. The CHANGELOG carries the `## Unreleased` entry naming the added test. Both awk invocations must print `## Unreleased`:

   ```bash
   awk '/^## /{print; exit}' CHANGELOG.md
   awk '/^## /{sec=$0} /prompts/{print sec; exit}' CHANGELOG.md
   ```

</verification>