# 0003 Rules are pure functions of a stored snapshot

- Date: 2026-10-11
- Status: accepted

## Decided

- `internal/collect` is the only package that reads the outside world. It produces a
  `snapshot.Snapshot`: what was observed, plus a `missing` list naming every source that could
  not be read and why.
- `internal/rules` maps (Snapshot, Config) → findings with no I/O, no clock and no randomness.
- ⚠ **The whole snapshot is stored with each decision.** Replay re-scores the stored snapshot; it
  never re-reads the repository.

## Why

- The brief: decisions must be traceable afterwards, and rule changes must be re-evaluated on
  past data. Both need the inputs as they were, not as they are now — the repository has moved on.
- Rules become testable with fixtures alone (`testdata/scenarios`), without git or a network.
- ⚠ **"Could not be obtained" stays distinct from "not there"** (`.claude/rules/evidence.md`):
  a rule whose evidence is missing reports `not-obtained`, never `clear`.

## Alternatives considered

| Option | Why not |
|---|---|
| Rules that call git themselves | Cannot be replayed; tests need a repository for every case |
| Store only the findings | A new rule could never be run over old decisions |
| Store the diff text | Puts source code into the log, which may hold secrets ([ADR 0007](0007-nothing-leaves-the-machine-unless-an-issue-is-named.md)). Paths and line counts are enough for every rule |

## Grounds

Lithic's and Panther's rule backtesting both store inputs immutably and re-run drafts over them
([RESEARCH §1](../RESEARCH.md)). `hidetzu/decide-first` ADR 0003 made the same split.

## Can it change?

Adding a field to the snapshot is free: old decisions simply lack it and the rule reports
`not-obtained` for them. Removing or re-meaning a field is a `RulesVersion` bump.
