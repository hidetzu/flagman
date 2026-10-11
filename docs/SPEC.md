# SPEC — what may be claimed

⚠ **This file holds what flagman may claim about itself.** ⚠ Nothing else.
**How to work** is [`CLAUDE.md`](../CLAUDE.md); **how to write it** is
[`.claude/rules/`](../.claude/rules/); **why** is [`adr/`](adr/).

⚠ **Never write a count in here** ([`evidence.md`](../.claude/rules/evidence.md)).
⚠ **Counts are announced by whatever produced them**, at the moment it runs.

---

## 1. What this implements

⚠ **A row goes in here only once the behaviour exists and a check asserts it.**
⚠ **"What asserts it" names a case.** `docs-check.mjs --only=spec-cases-exist` fails when a row
names no case or a case that does not exist; ⚠ **whether the case covers every clause is the
reviewer's to read.**

| Area | What is supported | Which authority, which section | What asserts it |
|---|---|---|---|
| Verdict | Three verdicts from a capped sum of named findings and two thresholds; a fired `hard` rule forces `defer` | [ADR 0004](adr/0004-the-score-is-a-sum-of-named-findings-not-a-probability.md) | `TestCompute`, `TestComputeRecordsHardRules` |
| Verdict | `check` exits 0 / 10 / 20 for continue / review / defer; 1 error, 2 usage | ADR 0004 | `TestScopedTestedCommitContinues`, `TestMergeDefers`, `TestUsageExitCodes` |
| Rules | Every rule reports on every snapshot; evidence that was not obtained is `not-obtained`, never `clear`; an unreadable repository fires `repo.not-obtained` | `.claude/rules/evidence.md` | `TestEveryRuleRunsOnEverySnapshot`, `TestOutsideARepositoryItStillAnswers` |
| Output | Every rule that could not run is named under "Not obtained", grouped by reason | `.claude/rules/evidence.md` | `TestNotObtainedIsNeverAnEmptyHeading` |
| Rules | Same snapshot and config give the same findings | [ADR 0003](adr/0003-rules-are-pure-functions-of-a-stored-snapshot.md) | `TestEvaluateIsDeterministic` |
| Rules | A path the task names out of scope forces `defer` and is named in the evidence | ADR 0004 | `TestOutsideScopeIsHard`, `TestOutOfScopeDefers` |
| Rules | An undecided point in the task (heading, TBD / 未定 marker, or a needs-decision label) forces `defer`, quoting where it was found | ADR 0004 | `TestUndecidedTaskDefers`, `TestParseTaskJapaneseAndOpenDecisions`, `TestIssueStateAndLabelsAreRead` |
| Rules | A closed or merged task forces `defer` | ADR 0004 | `TestMergedOrClosedTaskIsHard` |
| Rules | Unknown test status counts for more than passing and less than failing | [RESEARCH §1](RESEARCH.md) selective prediction | `TestUnknownTestsCountForLessThanFailingTests` |
| Rules | Weights, hard flags and off switches come from the config; a rule absent from the config scores 0 and says so | ADR 0004 | `TestOffRuleScoresNothingButStillReports`, `TestUnconfiguredRuleScoresZeroAndSaysSo`, `TestSensitiveGroupsCarryTheirOwnPoints` |
| Evidence | Diff against the merge base, including untracked files and renames; history touches and reverts per changed path | — | `TestParseNumstatZ`, `TestParseNameStatusZ`, `TestParseLog`, `TestThirdPartyRepositoryHistory` |
| Evidence | A failing test command gives `tests.failing` with its last output line | — | `TestFailingTestCommandOnDefaultBranchDefers` |
| Evidence | Task sections are read from headings only (English and Japanese); fenced code and prose are not read as sections | — | `TestParseTaskEnglish`, `TestParseTaskWithoutHeadingsFindsNothing` |
| Evidence | ADRs naming a changed path or its directory are cited by file and line; the ADR index README is not a record | — | `TestReadADRs` |
| Evidence | External findings are read from a JSON file, each with an id and a summary | [ADR 0008](adr/0008-no-llm-in-the-core-agents-plug-in-through-adapters.md) | `TestReadExternal` |
| Privacy | `gh` is run only when an issue is named; a task file never reaches it | [ADR 0007](adr/0007-nothing-leaves-the-machine-unless-an-issue-is-named.md) | `TestIssueIsFetchedOnlyWhenNamed` |
| Privacy | The log holds paths, not file contents; the hook does not log the raw command; the default log is git-ignored | ADR 0007 | `TestLogHoldsNoFileContents`, `TestClaudeCodeHook`, `TestFeedbackLoop` |
| Log | Append-only; later follow-ups supersede on read; unreadable lines and orphan follow-ups are counted, not dropped | [ADR 0005](adr/0005-ground-truth-is-a-label-then-an-outcome-then-the-human-decision.md) | `TestAppendReadRoundTripAndSupersede` |
| Log | Truth is derived label → problem outcome → human changed/stopped → human approved → ok outcome | ADR 0005 | `TestTruthOf` |
| Replay | Each decision re-scored under its stored config is compared with its stored verdict and score | [ADR 0006](adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md) | `TestReproduceDetectsAChangedVerdict`, `TestFeedbackLoop` |
| Replay | Metrics with numerator and denominator: false autonomy, human needed caught, missed escalation, unnecessary deferral, over-review, left to the AI alone, exact, weighted cost, confusion matrix, and the ids behind each mismatch | ADR 0006 | `TestMeasureDenominators` |
| Replay | Comparison ranks false autonomy, then missed escalation, then cost, and lists what the better side is worse at | ADR 0006 | `TestBetterRanksFalseAutonomyFirst`, `TestSuggestImprovesOnTheHeldOutPart`, `TestFeedbackLoop` |
| Suggest | Thresholds fitted on older labelled decisions, judged on newer ones; unchanged when nothing is wrong; rules seen in false autonomies, and false autonomies no rule saw, are listed | ADR 0006 | `TestSuggestImprovesOnTheHeldOutPart`, `TestSuggestKeepsThresholdsWhenNothingIsWrong`, `TestLimitationsAreMeasuredAsFalseAutonomy` |
| Scenarios | Every hand-labelled case in `testdata/scenarios` gets its label under the default config, with several of each verdict | ADR 0006 | `TestScenariosGetTheirLabelUnderDefaults` |
| Hook | `hook claude-code` maps shell commands to actions, answers `ask` on defer, shows the reasons without a permission decision on review, says nothing on continue, and exits 0 on bad input; each message names the decision id and the command to record it | ADR 0008 | `TestActionForCommand`, `TestHookNeverFailsTheAgent`, `TestClaudeCodeHook`, `TestHookMessageNamesTheDecisionAndHowToRecordIt` |
| Log | `pending` lists decisions with no known truth, newest first, with the count over all decisions, and calls out unrecorded continue decisions | ADR 0005 | `TestPendingListsOnlyDecisionsWithoutTruthNewestFirst` |
| Config | Validated on load; the hash changes with any parameter and survives a round trip with each weight's grounds | — | `TestDefaultIsValidAndRoundTrips`, `TestHashChangesWithAnyParameter`, `TestValidateRejectsMeaninglessNumbers` |

## 2. What this deliberately does not implement

⚠ **A gap named here is a decision.** ⚠ **An unnamed gap is just something not done yet.**

| Not implemented | Deliberate? | Why |
|---|---|---|
| ⚠ **Understanding what a change means** (an exported API changing, user-facing wording, an ambiguous task with no marker) | ⚠ **yes, for now** | No rule reads semantics. ⚠ **These are measured as false autonomies in `testdata/limitations`**, not hidden. Hand such evidence in with `--external`, or label the outcome so `suggest` lists it as unseen |
| A language model in the rules | yes | [ADR 0008](adr/0008-no-llm-in-the-core-agents-plug-in-through-adapters.md) |
| Fitting rule weights automatically | yes | [ADR 0006](adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md) |
| Adopting a suggestion without the owner | yes | ADR 0006 |
| Defending against an agent that lies to flagman or edits its log | ⚠ **yes** | flagman assumes a cooperative agent. An agent can pass `--tests pass` falsely or skip the call. `--run-tests` and the hook narrow this; ⚠ **nothing here closes it**. `.flagman/**` is a sensitive path so a change to the gate's config is itself flagged |
| Risk-coverage curves, AURC, calibration | not yet | They need far more labelled decisions than a personal log starts with ([RESEARCH §1](RESEARCH.md)) |
| Adapters for agents other than Claude Code | not yet | Any agent that can run a command and read its exit code can use `check` directly |
| Reading issue trackers other than GitHub | not yet | A task file works for anything |

## 3. Measured numbers

⚠ **Conditions shared by every row**: measured 2026-10-11 on linux/amd64, go1.26.2, git 2.x,
flagman built from the commit that adds this file, default config, rules v1, by running the
command in the row. ⚠ **The fixtures are hand-written by the same author as the rules**
(see [`verify` §3](../.claude/skills/verify/SKILL.md)); these numbers show the mechanism works on
them, ⚠ **not how flagman performs on anyone's real decisions** — no real labelled log exists yet.

| What was measured | Value | When | Under what conditions |
|---|---|---|---|
| False autonomy on `testdata/scenarios` | 0/9 | 2026-10-11 | `flagman replay --scenarios testdata/scenarios` |
| Left to the AI alone on `testdata/scenarios` | 6/20 | 2026-10-11 | same run |
| Exact verdict on `testdata/scenarios` | 20/20 | 2026-10-11 | same run |
| False autonomy on `testdata/limitations` | 3/3 | 2026-10-11 | `flagman replay --scenarios testdata/limitations` |
| False autonomy on the held-out part of `testdata/history`, defaults → suggested thresholds | 1/3 → 0/3 | 2026-10-11 | `flagman suggest --scenarios testdata/history` (fitted on the older 21, judged on the newer 9) |
| Left to the AI alone on that held-out part, defaults → suggested | 7/9 → 1/9 | 2026-10-11 | same run — ⚠ **the cost of that suggestion** |
