# 0002 One Go binary, standard library only

- Date: 2026-10-11
- Status: accepted

## Decided

- flagman is one statically linked Go binary. `go.mod` has no `require` block.
- Config and log are JSON (`encoding/json`). CLI parsing is `flag`. git and gh are run as
  external commands, never linked.

## Why

- The brief puts the personal developer first and asks for a tool usable from any agent.
  A single binary with no runtime installs with `go install` or a download.
- ⚠ **A gate that judges dependency changes should not drag in dependencies of its own.** With
  none, the dependency licence check for publication is empty by construction.
- The standard library covers everything needed: JSON, subprocesses, globbing, hashing.

## Alternatives considered

| Option | Why not |
|---|---|
| YAML config (a third-party parser) | Readability gain is small; JSON keeps zero dependencies and is what agents emit most reliably |
| SQLite for the log | Needs cgo or a large pure-Go driver. Append-only JSONL is greppable, diffable and enough at personal scale |
| A git library (go-git) | Large, and `git` is always present where flagman is useful |
| Rego / OPA for rules (as cupcake does) | Powerful, but a second language to learn, and a policy engine does not by itself give replay or measurement |
| Cobra for the CLI | Convenience only |

## Grounds

`connect-doctor` (same owner) made the same choice in its ADR 0006 and it held.

## Can it change?

Yes. Adding a dependency needs an ADR naming what it buys and its licence.
