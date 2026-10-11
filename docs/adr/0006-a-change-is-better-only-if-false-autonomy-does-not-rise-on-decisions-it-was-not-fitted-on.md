# 0006 A change is better only if false autonomy does not rise, on decisions it was not fitted on

- Date: 2026-10-11
- Status: accepted

## Decided

- **Comparison** (`flagman replay --compare B.json`): B is *better* than A when it has fewer false
  autonomies; on a tie, fewer missed escalations; on a tie, a lower weighted cost. ⚠ **Every
  comparison also prints each measure on which B is worse** — unnecessary deferrals, over-review,
  the share left to the AI, exact verdicts — so "better" never hides what it costs.
- **Suggestion** (`flagman suggest`): the labelled decisions are split by time. Threshold pairs
  are searched on the older part and the best is judged on the newer part (30 % by default).
  ⚠ **Only thresholds are searched. Rule weights are never fitted.** Instead, `suggest` lists
  which rules fired in false autonomies, which fired only where no human was needed, and which
  false autonomies no rule saw at all.
- ⚠ **flagman never adopts a suggestion.** It writes a file; the owner compares and commits it.
- Reproducibility is measured on every replay: each decision re-scored under its own stored
  config must give the stored verdict and score.

## Why

- The brief: false autonomy is the important failure; improvement must be measurable; and the
  evaluation must cover missed human judgements, needless ones, the share left to the AI, and
  whether reasons can be reproduced. Each is its own printed number with its denominator.
- ⚠ **A lexicographic order rather than one blended number**, because no cost ratio makes a missed
  "the owner had not decided" acceptable, and a blend lets enough cheap wins outvote one of those.
- Judging on held-out, later decisions is the minimum that separates "fits this log" from "will
  help next week". A personal log is small; fitting a dozen weights to it would fit noise.
- Weights carry reasons (`why` in the config). A search would produce numbers with none.

## Alternatives considered

| Option | Why not |
|---|---|
| One cost-weighted score as the only yardstick | Hides trade-offs; see above |
| F-beta or Ask-F1 as the headline | Two-class; loses the review band. The components are printed instead |
| Fit weights by search or regression | Overfits a small log; produces reasons-free numbers |
| Cross-validation | Shuffles time; a rule tuned on next month's decisions would look better than it is |
| Auto-apply the best config | Moves the gate's own strictness out of the owner's hands — the very thing `.flagman/**` being sensitive guards against |

## Grounds

Learning to Defer and Elkan 2001 on asymmetric costs; HiL-Bench on scoring both failure
directions; Lithic backtesting on comparing a draft against the live rules
([RESEARCH](../RESEARCH.md)). `testdata/history` shows the loop reducing false autonomy on its
held-out part *and* printing the autonomy it gives up (`TestSuggestImprovesOnTheHeldOutPart`).

## Can it change?

Yes. The order lives in `replay.Better`; changing it is a new ADR, because it changes what
"improved" has meant in every past comparison.
