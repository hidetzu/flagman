# flagman

**Should the AI coding agent go on alone, or ask a human?** flagman answers that from evidence in
the repository, not from the agent's own confidence. It logs every answer with what it was based
on, and it can replay that log to show whether a change to its rules made the answers better.

> **日本語の概要** — AI coding agent が commit / push / merge などの一歩を踏み出す前に呼ぶ CLI です。
> diff・テスト結果・git 履歴・タスク（Issue）・ADR といった客観的な証拠から
> `continue`（AI だけで継続してよい）/ `review`（人間の確認を推奨）/ `defer`（人間の判断が必要）を返します。
> 判断ごとに証拠・スコア・設定を丸ごと記録し、あとから「人間が実際どうしたか」「結果どうなったか」を追記できます。
> その記録を再生して、閾値やルールを変えたときに **false autonomy（本来聞くべきだったのに AI だけで進めた件数）**
> が減ったか、その代わりに何が悪化したかを分子・分母つきで比較します。ローカル完結で、LLM は使いません。

```text
$ flagman check --task TASK.md --tests pass --action push
flagman: DEFER — a human decides before this step
  score 100/100 (review at 30, defer at 60; forced by diff.outside-scope)
  action=push  branch=fix/exponent  base=main files=4 +13 −0  task=TASK.md

Why
  HARD diff.outside-scope — 1 changed path(s) are named out of scope by the task
         internal/billing/charge.go (out of scope: internal/billing/)
  +25  diff.sensitive-paths — sensitive area "billing" is changed (1 path(s))
         internal/billing/charge.go
  +20  action.publishes — the next step "push" makes the change visible to others
         action=push
  +20  diff.not-in-scope — 1 of 3 changed path(s) are outside the declared scope
         internal/billing/charge.go

decision d-20261011-094010-1bd3  (config sha256:cf4ca5e51e7070a4, rules v1)
next: ask the human, then record what they decided:
  flagman human d-20261011-094010-1bd3 approved|changed|stopped
[exit 20]
```

(Captured on 2026-10-11 from a throwaway repository: a task scoped to `internal/lexer/`, and a
change that also touched `internal/billing/`.)

## Why

Coding agents rarely ask unprompted, and their own sense of "I'm unsure" is a weak signal
([Ambig-SWE](https://arxiv.org/abs/2502.13069), [HiL-Bench](https://arxiv.org/abs/2604.09408)).
Existing gates decide by *which command* is run (approval modes, allowlists) or by a policy
written once and never checked against what happened. flagman looks at *what this change is*, and
it keeps score: every decision can later be marked right or wrong, and every rule change can be
replayed over past decisions before it is adopted. How it relates to prior work is in
[`docs/RESEARCH.md`](docs/RESEARCH.md).

## Install

```sh
go install github.com/hidetzu/flagman/cmd/flagman@latest
```

Or download an archive from the releases page. One static binary, no dependencies.
It needs `git` on PATH, and `gh` only if you name GitHub issues as tasks.

## Use

```sh
flagman init                        # optional: writes .flagman/config.json with the defaults
flagman check --action commit --task TASK.md --tests pass
echo $?                             # 0 continue, 10 review, 20 defer (1 error, 2 usage)
```

| Flag | Meaning |
|---|---|
| `--action` | The step about to be taken: `edit`, `commit` (default), `push`, `pr`, `merge`, `release`, `deploy`, `force-push`, `delete`, `migrate`, `external` |
| `--task` | A markdown task file, or `owner/repo#N` (read through `gh`) |
| `--tests` | A test result you already have: `pass`, `fail` or `not-run` |
| `--run-tests` | Run `evidence.tests.command` from the config and use its exit code |
| `--base` | Diff base (default: the remote's default branch) |
| `--external` | A JSON file of findings from another tool (below) |
| `--json`, `-v`, `--no-log` | JSON output; show grounds and clear rules; do not log |

### From an agent

Any agent that can run a shell command can use the exit code. A line for an agent's instructions
(`CLAUDE.md`, `AGENTS.md`, `.cursorrules`):

```text
Before each commit, push, PR or merge, run `flagman check --action <step> --task <task> --run-tests`.
Exit 0: go on. Exit 10: go on, and tell the human the reasons it printed.
Exit 20: stop and ask the human; then run `flagman human last approved|changed|stopped`.
```

For Claude Code, flagman can sit in the `PreToolUse` hook. It maps `git commit`, `git push`,
`gh pr create`, `gh pr merge`, `gh release create`, `git reset --hard` and the like to an action,
answers **ask** (with the reasons) when the verdict is defer, and says nothing otherwise, so
Claude Code's own permission settings still apply. It never answers *allow*, and an error in
flagman never blocks the agent. In `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "flagman hook claude-code --run-tests", "timeout": 600 }] }
    ]
  }
}
```

## How it decides

1. **Collect evidence** into a snapshot: the working tree against the merge base (including
   untracked files), git history of the changed paths, the test result, the task, and the decision
   records in `docs/adr/` that name a changed path. A source that cannot be read is recorded as
   *not obtained*, with the reason. It never counts as fine.
2. **Run the rules.** Each rule is a pure function of the snapshot and the config. It reports
   `fired`, `clear`, `not-obtained` or `off`, and when it fires it names the path or the line.
3. **Score.** Fired rules add their points, capped at 100. At 30 the verdict is `review`, and at
   60 it is `defer`. A **hard** rule forces `defer` whatever the sum.

`flagman rules` prints every rule with its points and the reason for them. The defaults:

| Rule | Default | Fires when |
|---|---|---|
| `action.irreversible` | **hard** | merge, release, deploy, force-push, delete, migrate, external |
| `task.open-decision` | **hard** | the task has an open "Human Decision" / "Open Questions" item, a TBD / 未定 marker, or a `needs-decision`-type label |
| `task.closed` | **hard** | the named issue is closed or merged |
| `diff.outside-scope` | **hard** | a changed path is listed under the task's Out of Scope |
| `tests.failing` | 40 | the tests fail |
| `diff.sensitive-paths` | per group | CI, auth/secrets, migrations, infrastructure, billing, licence/ADRs; `.flagman/**` (the gate's own config) is 60 |
| `repo.default-branch` | 25 | a commit or publish on the default branch |
| `action.publishes`, `diff.not-in-scope`, `history.recent-revert`, `repo.not-obtained` | 20 | push / PR; a path outside the declared scope; a recent revert on a changed path; no repository |
| `task.missing`, `diff.large`, `diff.no-tests`, `diff.dependencies`, `adr.names-changed-path` | 15 | no task; ≥ 10 files or ≥ 400 lines; code without a test change; a manifest changed; an ADR names a changed path |
| `task.no-scope`, `task.no-acceptance-criteria`, `history.hotspot`, `tests.not-obtained`, `diff.deletes-files` | 10 | as named; a hotspot is ≥ 5 commits in 90 days |
| `external` | per finding | a finding handed in with `--external` |

⚠ **These numbers are starting points, not measurements.** The score is an ordering for
thresholds, not a probability ([ADR 0004](docs/adr/0004-the-score-is-a-sum-of-named-findings-not-a-probability.md)).
The way to know whether they suit your project is the loop below.

A task file is read by its headings, in English or Japanese: `Goal`/`目的`, `Scope`/`対象`,
`Out of Scope`/`Non-goals`/`対象外`, `Acceptance Criteria`/`受け入れ条件` (or `- [ ]` items),
`Human Decision`/`Open Questions`/`未決事項`. Paths under Scope and Out of Scope are taken from
backticks or path-like words.

**External evidence** such as a scanner, a CI result or a review you trust goes in as JSON. It is
stored and replayed like any other evidence:

```json
[{"id": "gosec-G204", "summary": "subprocess launched with variable input", "points": 50, "hard": true,
  "evidence": ["internal/run.go:88"]}]
```

## Recording what happened

Each decision is a line in `.flagman/decisions.jsonl`. It holds the snapshot, the findings, the
score, the verdict, and the whole config with its hash. Afterwards, add what you know:

```sh
flagman human   last approved|changed|stopped   # what the human actually did
flagman outcome <id> ok|problem --note "reverted: broke hex parsing"
flagman label   <id> continue|review|defer      # what it should have been, if you know
flagman log                                     # one line per decision, with its truth
flagman show    <id>                            # everything recorded, and whether it reproduces
```

The truth of a decision, meaning what it should have been, is taken in this order:

1. a label;
2. `outcome problem` gives defer;
3. human `changed` or `stopped` gives defer;
4. human `approved` gives continue;
5. `outcome ok` gives continue.

Steps 3 and 4 are conventions, and every report says how many truths came from each source
([ADR 0005](docs/adr/0005-ground-truth-is-a-label-then-an-outcome-then-the-human-decision.md)).
A `continue` whose truth turns out to be `defer` is a **false autonomy**, and flagman says so when
you record it.

## Measuring and improving

```sh
flagman replay                       # metrics for the log under the current config
flagman replay --compare new.json    # the same log under another config, side by side
flagman suggest --write new.json     # propose thresholds; see which rules missed what
```

Every rate is printed with its numerator and denominator:

- **false autonomy**: it continued alone where a human was needed, out of the cases where one was needed;
- **human needed, caught**: out of the same cases, how many it deferred;
- **missed escalation**: the verdict was one step too low;
- **unnecessary deferral**: it stopped for a human who was not needed;
- **over-review**: it asked for a look where none was needed;
- **left to the AI alone**: how much it let the agent do without a human;
- **exact verdict**, a **weighted cost**, the confusion matrix, and the decision ids behind every mismatch;
- **reproducibility**: each decision re-scored under its own stored config must give the stored verdict.

⚠ **"Better" means fewer false autonomies first, then fewer missed escalations, then lower cost,
and every comparison also lists what the better side is worse at.** `suggest` fits thresholds
on the older part of the log and judges them on the newer part. It never fits rule weights. It
lists the rules that fired in false autonomies without reaching review, and the false autonomies
no rule saw at all; those need new evidence, not new weights. flagman never adopts a suggestion.
You compare it and commit it
([ADR 0006](docs/adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md)).

The repository ships three labelled fixture sets that show the loop. The authoritative values
and their conditions are in [`docs/SPEC.md` §3](docs/SPEC.md#3-measured-numbers).

```sh
flagman replay  --scenarios testdata/scenarios    # hand-labelled continue / review / defer cases
flagman replay  --scenarios testdata/limitations  # cases flagman cannot see (measured as false autonomy)
flagman suggest --scenarios testdata/history      # a synthetic history where a threshold change helps
```

On `testdata/history`, the suggested thresholds remove the false autonomy in the held-out part.
The same output shows the price: the share left to the AI alone drops sharply. That trade-off
is the owner's call, and the tool prints it instead of hiding it.

## Privacy

- flagman makes no network call of its own. The one outward call is `gh issue view`, and only when
  you name an issue with `--task owner/repo#N`. It sends nothing from the working copy.
- The log holds paths, line counts, branch names, quoted task lines and the last line of test
  output. It never holds file contents or diff text. The agent hook does not log the command line.
- The default log is excluded from git through `.git/info/exclude`. The config is meant to be committed.

## Limitations

- **flagman does not understand what a change means.** It cannot see an exported API changing,
  user-facing wording, or a vague task that carries no undecided marker.
  `testdata/limitations` keeps such cases, and replay counts them as false autonomies.
  Hand such evidence in with `--external`, or record the outcome so `suggest` lists it.
- **It trusts the agent that calls it.** An agent can pass `--tests pass` falsely, or never call
  flagman. `--run-tests` and the hook narrow this, but nothing closes it. A change to `.flagman/`
  is itself flagged.
- **The defaults are not tuned on real data.** The fixtures were written by the same author as
  the rules. They show the mechanism works, not that the defaults suit your project. Your own log is
  the evidence that matters, and it starts empty.
- **The metrics need labels.** Until decisions have a human decision, an outcome or a label,
  there is nothing to measure. A small log gives noisy numbers; `suggest` warns below 20.
- **Agent adapters:** only Claude Code's hook is built in. Other agents use the exit code.
- **Tasks:** GitHub issues through `gh`, or a markdown file. Other trackers can export a file.

## Development

How this repository is worked on is [`CLAUDE.md`](CLAUDE.md), a port of
[`hidetzu/claude-dev-template`](https://github.com/hidetzu/claude-dev-template). Design decisions,
with the alternatives that were rejected, are in [`docs/adr/`](docs/adr/). What may be claimed,
and which test asserts each claim, is [`docs/SPEC.md`](docs/SPEC.md).

```sh
scripts/verify.sh fast       # gofmt, vet, docs-check, unit tests and the labelled fixtures
scripts/verify.sh final      # builds the binary and drives it against real git repositories
scripts/verify.sh external   # real gh and a real third-party repository (network; not on CI)
scripts/verify.sh fast --list            # count without running
scripts/verify.sh final --only=Feedback  # one case
```

## Releasing

Push a `v*` tag. `.github/workflows/release.yml` runs the final gate, then
[GoReleaser](https://goreleaser.com) builds archives for Linux, macOS and Windows (amd64,
arm64) with a checksum file. To build locally without publishing:

```sh
goreleaser release --snapshot --clean
```

## License

[MIT](LICENSE). flagman has no third-party Go dependencies.
