# 0005 Ground truth is a label, then an outcome, then the human decision

- Date: 2026-10-11
- Status: accepted

## Decided

The log is append-only JSON Lines with four kinds of line, keyed by decision id:
`decision`, `human` (approved / changed / stopped), `outcome` (ok / problem), `label`
(continue / review / defer). A later line of a kind supersedes an earlier one *when read*; nothing
is rewritten.

What a decision *should have been* is derived in this order (`decisionlog.TruthOf`):

1. an explicit `label` wins;
2. `outcome=problem` → defer;
3. `human=changed` or `stopped` → defer;
4. `human=approved` → continue;
5. `outcome=ok` → continue;
6. otherwise unknown, and the decision is left out of every rate (and counted as such).

⚠ **Rules 3 and 4 are conventions, not observations.** Every metric prints how many truths came
from each source, so a reader can see how much rests on them.

## Why

- The brief lists what must be kept: context, evidence, score, recommendation, the human's
  actual decision, and the observed result. These four kinds hold exactly that.
- ⚠ **A log only shows the branch taken** (Li et al. 2011). When flagman deferred and the human
  approved, nobody saw what going on alone would have done. "The human let it through unchanged"
  is the best available evidence that the deferral was not needed — and `label` lets the owner
  overrule it.
- ⚠ **A problem after a continue is the false autonomy the brief singles out**, so `outcome
  problem` outranks the human decision.
- Append-only keeps what was believed at the time, which is part of tracing a decision.

## Alternatives considered

| Option | Why not |
|---|---|
| Truth from outcomes only | Deferred decisions would never get a truth; unnecessary deferrals could not be measured |
| Truth from the human decision only | A human approving something that later broke would count as correct |
| A two-level truth (needed a human or not) | Loses the review / defer distinction the brief's three answers make |
| Rewriting the decision line with its result | Loses history; makes the log harder to trust |

## Grounds

HiL-Bench's separate scoring of needless questions and missed blockers; Li et al. 2011 on
logged-branch bias ([RESEARCH §1](../RESEARCH.md)).

## Can it change?

The derivation order is code in one function, tested case by case. Changing it changes every
past metric, so it is a `RulesVersion`-level change and needs a new ADR.
