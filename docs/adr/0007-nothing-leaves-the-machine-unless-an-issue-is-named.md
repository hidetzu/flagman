# 0007 Nothing leaves the machine unless an issue is named

- Date: 2026-10-11
- Status: accepted

## Decided

- flagman opens no network connection of its own.
- ⚠ **The one outward call is `gh issue view`, run only when `--task owner/repo#N` names an
  issue.** It sends that reference to GitHub; nothing from the working copy is sent.
- `--run-tests` runs the command the project put in its own config, in its own directory.
- The log stores paths, line counts, branch names, quoted task lines and the last line of test
  output. ⚠ **It never stores file contents or diff text.** The raw command seen by the agent
  hook is not stored either, only the action it maps to.
- The default log, `.flagman/decisions.jsonl`, is git-ignored by a `.flagman/.gitignore` that
  flagman writes; the config next to it is meant to be committed.

## Why

- The brief: local-first, and sending code or secrets out only by explicit opt-in. Naming an issue
  is that opt-in, and it sends no repository content.
- Branch names and task quotes can still be private, so publishing the log is left to the owner.

## Alternatives considered

| Option | Why not |
|---|---|
| Fetch the GitHub API directly | Needs token handling; `gh` already holds the user's auth |
| An LLM evaluator (opt-in) | Not in the core: it would send code out, and is not replayable ([ADR 0008](0008-no-llm-in-the-core-agents-plug-in-through-adapters.md)). Its result can still be handed in as an external finding |
| Commit the log by default | Would publish branch names and task text without asking |

## Grounds

The brief's product constraints. `TestIssueIsFetchedOnlyWhenNamed` proves a task file never
reaches `gh` (with a control that does), and `TestLogHoldsNoFileContents` that a secret in a
changed file is not logged.

## Can it change?

Any new outward call needs an ADR and an explicit flag.
