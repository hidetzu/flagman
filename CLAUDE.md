# flagman — how we work

This file holds **how to work**.
**What may be claimed** goes in `docs/SPEC.md`; **why a decision was made** goes in `docs/adr/`.
⚠ **How to write code** (the language, the layer split, testing priorities, forbidden git
operations) lives in `.claude/rules/`.

⚠ **Never duplicate.** ⚠ **Written in two places, one of them goes stale.**
When the spec changes, fix the spec. Do not restate it here.

### ⚠ A section that points owns nothing

⚠ **Several sections below own no rule: they name the file that does.**
⚠ **A pointer section carries the pointer and nothing else** — ⚠ **no summary, no excerpt,
no "the gist is".** ⚠ **Naming the subjects the file covers is fine; ⚠ stating any of its rules
is not.**

⚠ **Grounds: the body moves and the summary does not.** ⚠ **So the summary is where the stale
copy lives** — ⚠ **and it sits directly under the words "not here", which is the one place a
reader will not think to doubt.**
⚠ **Stepped on where this template came from**: ⚠ **on the day that project moved its rules out,
7 of its 9 pointer sections had grown 16 lines of summary**, ⚠ **one of them a verbatim copy of
the evidence rules.**

⚠ **Never delete a heading here, and never renumber one.**
⚠ **Records already cite these rules by section number** — ADRs, issues, code comments, the
text a check prints — ⚠ **and the number is the address.**
⚠ **Measured in that same project: its two most-cited sections were cited 114 and 94 times.**
⚠ **Rewriting those citations rewrites records of what was known at the time.**
⚠ **So a rule moves out and the heading stays, holding one line that says where it went.**

⚠ **Make it a wall, not a note**: ⚠ **a check that reads this file and fails when a pointer
section grows a second line.** ⚠ **The rule alone is only a promise.**

Before starting work, read `.claude/rules/README.md`.

⚠ **`⚠` marks "it hurts if you step on it".** ⚠ It is not decoration.

---

## 0. What this repository is

flagman is a Go CLI an AI coding agent calls before a step (commit, push, PR, merge, release…).
From evidence in the repository — the diff, tests, history, the task, decision records — it answers
`continue` / `review` / `defer`, logs the decision with everything it was made from, and later
replays the log to measure whether a change to its rules lowered false autonomy and at what cost.
⚠ **The project is an experiment in whether "when should the AI ask a human" can be improved by
measurement rather than by intuition.** The owner's brief is [`docs/BRIEF.md`](docs/BRIEF.md);
what may be claimed is [`docs/SPEC.md`](docs/SPEC.md); prior art is [`docs/RESEARCH.md`](docs/RESEARCH.md).

⚠ **`.claude/` is a port of [`hidetzu/claude-dev-template`](https://github.com/hidetzu/claude-dev-template).**
⚠ **When this project has to fight the template to do the right thing, the template is wrong** —
the fix goes back there naming this project.

---

## 1. The first principle

> **Never miss a needed human before saving a human's time.** ⚠ False autonomy outranks every
> other measure ([ADR 0006](docs/adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md)).

⚠ **Whatever it says, it never outranks the evidence rules.**
⚠ **Those are [`.claude/rules/evidence.md`](.claude/rules/evidence.md), and they hold everywhere** —
in the code, in the tests, and in every report.

⚠ **Never restate them here.** ⚠ **They are the one thing this template does not let a project
re-word**, because re-wording them is how they get softened.

---

## 2. Verification

⚠ **The contract is [`.claude/rules/verification.md`](.claude/rules/verification.md).**
⚠ **What to actually run is `.claude/skills/verify/SKILL.md`, which each project writes for itself.**
⚠ Neither belongs here (never two copies).

---

## 3. Architecture boundaries

⚠ **One Go binary, standard library only** ([ADR 0002](docs/adr/0002-one-go-binary-standard-library-only.md)).
⚠ **Only `internal/collect` touches the outside; `internal/rules` is a pure function of a stored
snapshot** ([ADR 0003](docs/adr/0003-rules-are-pure-functions-of-a-stored-snapshot.md)).
⚠ **Every weight, threshold and cost is config (`internal/config`), never a constant in a rule.**
⚠ **Every sentence a human reads is `internal/report`.** ⚠ **Agent-specific code is an adapter in
`cmd/flagman`** ([ADR 0008](docs/adr/0008-no-llm-in-the-core-agents-plug-in-through-adapters.md)).

⚠ **Not to be introduced without a reason recorded in an ADR:**

```text
any third-party Go module
a network call other than `gh issue view` for a named issue (ADR 0007)
a language model in the rules (ADR 0008)
storing file contents or diff text in the log (ADR 0007)
fitting rule weights automatically (ADR 0006)
the agent hook answering "allow"
```

Two clauses hold regardless of the domain:

- ⚠ **Never keep two implementations that answer the same question.**
  If one is unavoidable, cross-check them mechanically.
  ⚠ **Writing the same decision in two places is how the two silently diverge.**
- ⚠ **A layer split belongs in an ADR before it belongs in code.**

---

## 4. Words

- **Never leak internal state into what a human reads.** Not an error code, but a sentence
  that says what happened and what to do about it.
- **Name things after the concept the domain already named, not after the data structure.**
  ⚠ **Borrow the existing name exactly.** ⚠ If a name here differs, that difference is a claim —
  justify it.
- **Never rename in bulk.** Changing a term does not license a sweep through the ADRs and past
  discussions.

⚠ **Names come from the brief and the research it named**: *false autonomy* (the brief),
*defer* (Learning to Defer), *coverage* is printed as "left to the AI alone" because that is the
brief's own wording. ⚠ **The three verdicts are `continue`, `review`, `defer` — never renamed,
never a fourth** (a new verdict is a spec change). ⚠ **Rule names are `area.what-was-seen`**
(`diff.outside-scope`), and a rule name in the log is permanent: rename one and old decisions stop
matching ([ADR 0003](docs/adr/0003-rules-are-pure-functions-of-a-stored-snapshot.md)).

⚠ **Statuses already spoken for**: `fired`, `clear`, `not-obtained`, `off`. ⚠ **`not-obtained` is
never shown as `clear`** (`.claude/rules/evidence.md`).

⚠ **CLI output, documents, code and commits are English** (a public tool other agents read).
⚠ **A Japanese summary sits at the top of the README** for the owner.

### 4-1. Never open with what does not work

⚠ **This is not about hiding anything.** §1 outranks it, and **limitations are always stated**.
What changes is the **order, the subject, and the tense** — not whether it is said.

- **Say what this does first.** What it does not do comes after, with the reason
  and with what to do instead.
- **Do not use the progressive tense for a state.** "not receiving" reads as something
  happening right now on the reader's machine.
- **Never phrase our own gap as the other side's fault.** If we never implemented it,
  do not report it as "no response". ⚠ **The reader's next move depends on which it is** —
  retry, wait, or give up.
- **Do not sound stalled.** "not implemented yet" beats "unavailable": leave a reason to come back.

---

## 5. Comments

Comments carry **why this, why this value, what is being avoided**. That is an asset.
⚠ **But a stale comment misleads harder than stale code**, because it is believed.

Change code, and update the whole set:

```
implementation → test → comment → README → docs/SPEC.md
```

⚠ **When a check reads documentation or comments, strip the comments first.**
⚠ Otherwise the check picks up the very words written to describe it.

---

## 6. How to write numbers

⚠ **Owned by [`.claude/rules/evidence.md`](.claude/rules/evidence.md).** ⚠ Not here.

---

## 7. How to proceed

1. **Measure before polishing.** Before fixing anything, state what it does now, in numbers.
2. Report **observation** (measured values, captured output) and **inference** (interpretation)
   separately.
3. Never report as confirmed what was not verified.
4. Do not widen a change past its `Non-goals`. The smallest change that meets the goal is the default.

⚠ **Deciding yourself vs. asking, and `ready-for-ai`, are owned by
[`.claude/rules/owner-decisions.md`](.claude/rules/owner-decisions.md).** ⚠ Not here.

---

## 8. git

⚠ **Owned by [`.claude/rules/git.md`](.claude/rules/git.md)** — Conventional Commits, permission
for `git push` and merge, how to split commits, and what never goes into anything public.
⚠ Not here.

---

## 9. Pitfalls we have stepped on

⚠ **This table starts empty, and that is correct.**

⚠ **Nothing goes in here that did not happen in this repository.** Not an analogy from another
project, not something plausible, not something an AI expects to be true.
⚠ **A pitfall is a measurement**: it names what happened, and what to do instead.

⚠ **This is not where engineering constraints go.** A rule that holds because of the language,
because of a protocol, or because the input is hostile ⚠ **belongs in `.claude/rules/`, and binds
already.**
⚠ **Never manufacture an incident to move a constraint in here**, and
⚠ **never soften a constraint on the grounds that this table has no row for it yet.**

⚠ **When you fill a row in, also leave the test behind.** A row with no test is a note;
a row with a test is a wall.
⚠ **If the incident also produces a new rule, the rule goes to `.claude/rules/` citing this row.**
⚠ **Both records stay. Neither replaces the other.**

| What happened | What to do instead |
|---|---|
| ⚠ **flagman polluted its own evidence** (2026-10-11, found by dogfooding): it wrote `.flagman/.gitignore` into the working tree, so the next check saw an untracked change to the gate's config and deferred in every fresh repository | ⚠ **Nothing flagman writes may land in the working tree as a side effect.** The log is excluded through `.git/info/exclude`. Guarded by `TestFeedbackLoop` (a second check must not mention `.flagman/`) |
| ⚠ **The task file counted as a change outside its own scope** (2026-10-11, dogfooding): `TASK.md` itself fired `diff.not-in-scope` | The task file is skipped by the scope rules. Guarded by `TestTheTaskFileIsNotOutsideItsOwnScope` |
| ⚠ **"Not obtained" printed as an empty heading** (2026-10-11, dogfooding on this repository with no task): the task rules could not run, and no line said which | Rules that could not run are listed under the heading, grouped by reason. Guarded by `TestNotObtainedIsNeverAnEmptyHeading` |
| ⚠ **A merged pull request named as a task read as open** (2026-10-11, the external tier against `cli/cli#1`): `gh` reports `merged`, and `task.closed` only knew `closed` | Both are finished. Guarded by `TestMergedOrClosedTaskIsHard`, `TestIssueStateAndLabelsAreRead` |
