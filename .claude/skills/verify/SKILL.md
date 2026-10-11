---
name: verify
description: Decide which of flagman's checks to run, in what order, run them, and return PASS / FAIL / NOT-VERIFIED. Use before a PR, while fixing, and when CI fails. Never builds a new check runner.
---

# Verify

⚠ **The contract is [`rules/verification.md`](../../rules/verification.md).** ⚠ **This file only
names the entry points.** ⚠ **The checks themselves are the tests; this file never copies them.**

## 1. The entry points (⚠ this is all of them)

| Tier | Command | What separates it | ⚠ Leaves the machine? |
|---|---|---|---|
| **Fast** | `scripts/verify.sh fast` | No binary, no git, no gh: gofmt, `go vet` (all tags), `docs-check.mjs`, unit tests, and ⚠ **the labelled fixtures** (`testdata/scenarios`, `testdata/limitations`, `testdata/history`) replayed through the rules | ⚠ **No** |
| **Final gate** | `scripts/verify.sh final` | ⚠ **Builds the binary from this tree and drives it** against real git repositories made in temp dirs: check, the feedback loop, replay, compare, the Claude Code hook. ⚠ **`gh` is a stub on PATH that records its calls** | ⚠ **No** — the stub stands in for gh; nothing else is outward |
| **External** | `scripts/verify.sh external` | ⚠ **The other end is something we did not write**: a real GitHub issue through the real `gh`, and a real third-party repository's history (`git clone` of `spf13/pflag`). Asserts only that a verdict came back and every source is obtained or named as not obtained | ⚠ **Yes — GitHub API via gh, and a git clone.** ⚠ **Never on a PR** |

**Partial runs** (every tier):

```text
scripts/verify.sh <tier> --only=REGEX   one named case (go test -run); skips gofmt/vet/docs-check
scripts/verify.sh <tier> --list         count without running — reads the source, compiles nothing
node .claude/tools/docs-check.mjs --list / --only=NAME
```

⚠ **The first output line names the tier and subset; the last announces the count.**
⚠ **Copy the announced count into the report. Never write it into a document.**
⚠ **A skipped test makes the run `NOT-VERIFIED` (exit 3), never `PASS`.**

## 2. Order

```text
while fixing     scripts/verify.sh fast --only=<what you touched>   then   scripts/verify.sh fast
before the PR    scripts/verify.sh fast && scripts/verify.sh final
before a release scripts/verify.sh external    (⚠ say the date and the machine in the report)
```

## 3. ⚠ Am I measuring what I think I am?

- ⚠ **The final gate builds the binary inside `TestMain` on every run.** ⚠ **There is no prebuilt
  `bin/flagman` in the measurement** — `bin/` is ignored and never read by a test.
- ⚠ **Each final-gate test makes its own repository and its own gh stub.** ⚠ **`GIT_CONFIG_GLOBAL`
  is `/dev/null`, so the developer's git config cannot change a result.**
- ⚠ **`FLAGMAN_LOG` is emptied in the final gate** so the developer's own environment cannot
  redirect the log under test.
- ⚠ **`docs-check.mjs` reads `git ls-files`** — ⚠ **`git add` a new document before trusting its
  link check.**
- ⚠ **The scenario labels were written by the same hand as the rules.** ⚠ **Agreement there is
  necessary, not sufficient**: that is why `testdata/limitations` exists, and why the external tier
  and dogfooding on this repository are separate evidence.

## 4. ⚠ Proving a refusal (`rules/verification.md` § An exercise must not change the world)

⚠ **`TestIssueIsFetchedOnlyWhenNamed`**: with a task file, the gh stub's call record stays empty;
⚠ **the control** names an issue and the stub *is* called (and its `needs-decision` label is read).
⚠ **`TestLogHoldsNoFileContents`**: a secret-looking string in a changed file is absent from the
log; ⚠ **the control** is that the file's path is present.
⚠ **`TestClaudeCodeHook`**: the raw command is absent from the log; the hook still answers `ask`.

## 5. Splitting a failure

| Failed | ⚠ Ours? |
|---|---|
| fast | ⚠ **Ours.** Nothing outside the process is involved |
| final | ⚠ **Ours**, ⚠ unless `go build` or `git` itself could not run — say which |
| external | ⚠ **Possibly theirs** (GitHub, the network, a repository that moved). ⚠ **Still FAIL; not evidence our code broke.** Rerun once. ⚠ **A failed clone is a skip, which is NOT-VERIFIED** |

## 6. What CI runs

`.github/workflows/check.yml`, on every PR and on pushes to `main`:

| Tier | CI | ⚠ Why |
|---|---|---|
| fast | runs | — |
| final gate | runs | needs only Go and git |
| external | ⚠ **never** | ⚠ **Depends on GitHub's and a third party's uptime.** ⚠ **Every run's summary says so** |

## 7. Return

The block in [`rules/verification.md`](../../rules/verification.md) § What to return. ⚠ **No other shape.**
