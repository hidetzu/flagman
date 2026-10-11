# Go

⚠ **`MUST` = required, `SHOULD` = default, `MAY` = optional.**

## Grounds

⚠ **flagman's whole value is that a decision can be traced and replayed.** ⚠ **Anything that
makes a rule depend on something other than its snapshot and its config breaks that silently** —
the decision still prints, and the replay quietly disagrees with it.

## Dependencies

- MUST: ⚠ **Standard library only.** ⚠ **`go.mod` has no `require` block.** ⚠ **Adding one is an
  ADR first** ([ADR 0002](../../docs/adr/0002-one-go-binary-standard-library-only.md)).

## Rules stay pure

- MUST: ⚠ **`internal/rules` and `internal/score` import nothing that does I/O**: no `os`, no
  `os/exec`, no `net`, no `time.Now`, no `math/rand`
  ([ADR 0003](../../docs/adr/0003-rules-are-pure-functions-of-a-stored-snapshot.md)).
  ⚠ **`TestEvaluateIsDeterministic` and `TestReproduceDetectsAChangedVerdict` hold this from the
  outside; the import rule holds it from the inside.**
- MUST: ⚠ **A rule whose evidence is missing returns `not-obtained`, never `clear`.**
- MUST: ⚠ **Points, thresholds and costs come from the config.** ⚠ **A number in a rule is a
  size threshold the config names (`evidence.*`), never a weight.**
- MUST: ⚠ **Changing what a rule means, or how findings combine, bumps
  `decisionlog.RulesVersion`** — ⚠ **so a replay can tell old verdicts were made under other rules.**

## Where things live

- MUST: ⚠ **Every sentence a human reads: `internal/report`.** Commands compose; they do not word.
- MUST: ⚠ **Every outward call: `internal/collect`.** ⚠ **A new one needs an ADR**
  ([ADR 0007](../../docs/adr/0007-nothing-leaves-the-machine-unless-an-issue-is-named.md)).

## Errors

- MUST: ⚠ **A collector that cannot read a source records it in `Snapshot.Missing` with a sentence
  saying why, and the rest goes on.** ⚠ **It never fails the whole decision.**
- MUST: ⚠ **A timeout is not a failure** — ⚠ **a test command that did not finish is
  `not-obtained`, not `fail`** (`.claude/rules/evidence.md`: a timer expiring is not an answer).
- MUST: ⚠ **Use `errors.Is` / `errors.As`**, never match an error's text — ⚠ **except git's and gh's
  stderr, which have no typed errors; say so in a comment where it happens.**

## Tests

- MUST: ⚠ **`gofmt` clean, `go vet` clean** (all build tags) — the fast tier runs both.
- SHOULD: ⚠ **Table tests for pure functions; fixtures for rules (`testdata/`).**
- MUST: ⚠ **A test that touches git or gh lives in `e2e/` behind a build tag**, and ⚠ **gh is a stub
  that records its calls** — ⚠ **with a control that reaches the stub**
  (`.claude/rules/verification.md` § An exercise must not change the world).
