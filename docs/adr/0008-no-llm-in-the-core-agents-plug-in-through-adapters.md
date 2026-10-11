# 0008 No LLM in the core; agents plug in through adapters

- Date: 2026-10-11
- Status: accepted

## Decided

- No rule calls a language model. Findings come from repository evidence.
- Evidence flagman cannot collect — a security scanner, a CI run, an LLM review the owner
  chose — is handed in as `--external findings.json`: each finding carries an id, a summary,
  its own points and optionally `hard`. It is stored in the snapshot like any other evidence, so
  it replays.
- The CLI is the interface. Agent integrations are thin adapters that call it. One is built in:
  `flagman hook claude-code`, for Claude Code's PreToolUse hook. ⚠ **It answers `ask` on defer and
  says nothing otherwise; it never answers `allow`, and on any error it exits 0 with no opinion,
  leaving the agent's own permission flow in charge.**

## Why

- The brief: not a self-reported-confidence checker, not tied to one agent, and external
  information should be usable.
- An LLM judgement cannot be re-run identically under replay, which breaks traceability; as an
  external finding it is at least recorded exactly as it was.
- Never answering `allow` means flagman can only add a human, never remove one the agent would
  otherwise have asked.

## Alternatives considered

| Option | Why not |
|---|---|
| Built-in LLM risk judge | Above; and it would send code out |
| Adapters for every agent | Most agents can run a shell command and read an exit code; the CLI already is the adapter. Claude Code is built in because its hook protocol needs JSON both ways |
| Hook answers `allow` on continue | Would bypass the user's own permission settings |

## Grounds

Ambig-SWE on agents' weak sense of when to ask; Claude Code hook docs for `ask`
([RESEARCH](../RESEARCH.md)).

## Can it change?

Yes: more adapters can be added without touching the core.

## Updates

### 2026-10-11 — the hook also speaks on review

The body's "says nothing otherwise" stays as the record of when it was written.
⚠ But since the change that added `flagman pending`, the hook shows the reasons on `review` as a `systemMessage` (to
the human) and `additionalContext` (to the agent), still without a `permissionDecision`. Grounds:
a review the human never sees cannot get an outcome recorded, and unrecorded decisions bias the
log toward deferrals (Claude Code hook docs: `systemMessage` is shown to the user; a hook without
a decision leaves the normal permission flow in charge).
