# 0004 The score is a sum of named findings, not a probability

- Date: 2026-10-11
- Status: accepted

## Decided

- Each rule that fires adds its configured points. The sum is capped at 100.
- Two thresholds turn the score into one of three verdicts: `continue` below `review`,
  `review` from `review`, `defer` from `defer` (defaults 30 and 60).
- ⚠ **A rule marked `hard` forces `defer` whatever the sum.** By default: irreversible actions,
  an undecided owner decision, a closed or merged task, a path named out of scope.
- The score is never printed or described as a probability, a confidence or a risk percentage.
- Exit codes: 0 continue, 10 review, 20 defer; 1 error, 2 usage.

## Why

- The brief wants a machine-usable score *and* a three-way human answer, with the reasons
  traceable. A sum of named findings is fully explained by its parts: every point comes from a
  line that says which rule and which path.
- ⚠ **Hard rules exist because some things must not be outvoted by a low total.** "The owner has
  not decided" is not a 40 % risk; it is a reason to stop.
- The three verdicts match the bands in AI Control (act / trusted review / human audit) and the
  brief's three answers.
- Exit codes leave 1 and 2 to errors so a caller cannot read a crash as "continue".

## Alternatives considered

| Option | Why not |
|---|---|
| A learned model (logistic regression on findings) | No labelled data at the start; and the brief asks not to start with ML |
| An LLM judging "is this safe to continue" | Not reproducible under replay, sends code out, and is the self-reported confidence the brief rules out |
| Rule hits only, no score (as `decide-first` does) | The brief asks for a machine-usable score, and thresholds are what replay can tune |
| Calibrated probability | A rule sum is not a probability; calibrating it needs many labels ([RESEARCH §1](../RESEARCH.md)) |

## Grounds

Guo et al. 2017 on what calibration requires; Lewis et al. 2013 on unexplained predictions being
ignored; Greenblatt et al. 2024 for the three bands ([RESEARCH](../RESEARCH.md)).

## Can it change?

Weights, thresholds and which rules are hard are config, not code. Changing how findings combine
(e.g. not capping) is a `RulesVersion` bump.
