# 0001 The tool is named flagman, not seatbelt

- Date: 2026-10-11
- Status: accepted

## Decided

The project, the binary and the Go module are `flagman` (`github.com/hidetzu/flagman`).
`seatbelt` was the working name in the owner's brief.

## Why

- ⚠ **The brief asked for a rename if confusion was likely, and it is.** To people who run
  coding agents, "Seatbelt" is the macOS sandbox their agents use, named in Claude Code's, Codex's
  and OpenCode's own docs. Many 2026 repositories use the word for agent sandboxes and guardrails,
  and npm, PyPI and crates.io names are taken ([RESEARCH §3](../RESEARCH.md#3-name)).
- A flagman is the person at a road works who signals *go* or *stop*. That is the job: say
  whether the agent goes on, and when to stop for a person.

## Alternatives considered

| Name | Why not |
|---|---|
| `seatbelt` | Above |
| `umpire` | An npm CLI already does "safe agent continuation" under that name |
| `holdpoint`, `handrail`, `tollgate` | Each already names an agent approval or guardrail tool |
| `arbiter`, `warden`, `greenlight`, `handoff` | Large existing developer tools, or "handoff" already means agent-to-agent context transfer |
| `signalman`, `referee`, `deferral` | Free of agent tools, but exact names taken on several registries |

## Grounds

The survey in [RESEARCH §3](../RESEARCH.md#3-name), run on 2026-10-11. No trademark registry was
checked.

## Can it change?

Yes, before a first release, at the cost of a module path change. After a release, every
`go install` line and every hook config would break, so a rename would need a new ADR.
The local working directory is still called `seatbelt`; that is not part of the product.
