---
status: draft
spec: [001-per-caller-session-id]
created: "2026-09-28T21:06:06Z"
branch: dark-factory/per-caller-session-id
---

# Record the per-caller session id in the changelog

<summary>
- The changelog carries the change under an unreleased heading.
- The entry names what a caller can now do, not which files were touched.
- The entry is one bullet, prefixed so the release tooling picks the right version bump.
- The changelog's existing header and every released section are left exactly as they are.
- No version number is invented and no release is cut.
</summary>

<objective>
Record this spec's change in `CHANGELOG.md` under the existing `## Unreleased` heading, so the release tooling sees it and the next release notes describe it. The spec's acceptance criterion is that the section extracted by `sed -n '/^## Unreleased/,/^## /p' CHANGELOG.md` contains at least one bullet beginning `feat:` that names the session header `X-Session-Id`; the `chore:` bullet already there describes unrelated work and does not satisfy it.
</objective>

<context>
Read these before editing:

- `CHANGELOG.md` — read the top of the file. It has a frozen header block (`# Changelog`, the "All notable changes…" line), then an existing `## Unreleased` section that already carries one unrelated `chore:` bullet (adopting dark-factory in this repo), then the newest released section, `## v0.4.2`. You are adding your `feat:` bullet to the existing `## Unreleased` section — do not add a second heading, and do not remove the `chore:` bullet.
- `docs/dod.md` — this repo's definition of done, in particular its repository-hygiene rule for the changelog.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — the entry format, the conventional prefixes, and the rules for the header block and for `## Unreleased`.
- `main.go` — `sessionHeader`, `sessionIDPattern`, `sessionIDFromRequest`, `promptHandler`, `sessionRunners`. Write the entry from the shipped behavior, not from this prompt.
- `README.md` — the `## Service Agents` section, for the reader-facing description of the same behavior.

What changed, in one sentence: a service agent's `/prompt` endpoint now accepts an `X-Session-Id` request header, serving each distinct id as its own conversation — concurrently across ids, one turn at a time within an id — while a request with no header keeps reaching the default conversation exactly as before, and an id outside `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}` is answered `400` before the agent process is invoked.
</context>

<requirements>
1. **Add your bullet to the existing `## Unreleased` section in `CHANGELOG.md`.** That section already sits after the header block and directly above `## v0.4.2`; do not add a second `## Unreleased` heading. The header block is frozen: do not move, delete, or insert anything above or inside the `# Changelog` title, the "All notable changes…" line, or the blank lines around them. The existing `chore:` bullet stays exactly as it is.

2. **Write exactly one new bullet under `## Unreleased`** (the section keeps its existing `chore:` bullet), because this is one logical change. It must begin with a conventional prefix — use `feat:`, since this adds functionality in a backwards-compatible way and therefore selects a minor bump. Follow the guide's shape: `- feat: <what changed> [why it matters]`.

3. **Describe the shipped behavior, specifically enough to be useful a year from now.** The bullet must name the header (`X-Session-Id`), state that a request without the header reaches the default session unchanged, state that each distinct session id is its own conversation, state the concurrency behavior (different ids concurrently, one id serialized), and state that an id outside the allowed format is rejected with `400` before the agent process runs. Name the mechanism — the per-session runner and its lock — rather than describing the diff file by file.

4. **Write it as one flowing bullet, not a list of sub-points.** Do not add `### Added` / `### Fixed` categories; the changelog is a flat list. Do not use the prompt's filename, a file path, or a test name as the entry.

5. **Change nothing else in `CHANGELOG.md`.** Do not rename `## Unreleased`. Do not create, renumber, or tag a version. Do not edit any released section. Do not add a SemVer preamble that is not already there.

6. **Before finishing**, run `<verification>` and confirm every command passes, then read the new bullet on its own and confirm a reader who was not present for this change learns what a caller can now do.
</requirements>

<constraints>
- The entry describes what was implemented, not what was verified, and not the steps taken.
- One bullet per logical change. This change is one bullet.
- The prefix is required; dark-factory reads it to determine the version bump.
- `## Unreleased` stays named `## Unreleased` on a feature branch. The release bot owns renaming it to a version and owns tagging.
- Do NOT commit — dark-factory handles git.
- Only `CHANGELOG.md` is in scope for this prompt. Do not edit `README.md` or any Go file.
</constraints>

<verification>
This change touches no Go code. Run `make precommit` anyway — it is the repo's definition of done (`docs/dod.md`) — and then the checks below.

Each of these must succeed:

```
[ "$(grep -c '^## Unreleased' CHANGELOG.md)" = "1" ]
sed -n '/^## Unreleased/,/^## /p' CHANGELOG.md | grep -cE '^- feat: '
sed -n '/^## Unreleased/,/^## /p' CHANGELOG.md | grep -c 'X-Session-Id'
```

The first must pass — the section exists exactly once and is not duplicated. It is a `[ ... ]` test rather than a `grep -c` comparison because the daemon reads exit codes, not printed counts, and `grep -c` exits 0 on a duplicate count of 2. The second must print at least `1` — the section carries a `feat:` bullet; the `chore:` bullet already there describes unrelated work and does not satisfy it. The third must print at least `1` — the new bullet names `X-Session-Id`. Both `grep -c` commands exit non-zero only when the count is zero, which is exactly the failure they are meant to catch.
</verification>
