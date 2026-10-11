# RESEARCH — what already exists, and what flagman takes from it

⚠ **Surveyed on 2026-10-11, from papers, public documentation and READMEs.** ⚠ **Nothing below
was run by us unless it says so.** Items marked *(unconfirmed)* came from a search snippet or a
secondary source and were not checked against the primary one. Decisions taken from this survey
live in [`adr/`](adr/), not here.

⚠ **The question this survey asks is not "what is the state of the art".** ⚠ **It is "for a
personal developer whose AI agent is about to take a step, what can decide *ask a human or not*
from evidence, and how do we know the deciding got better?"**

---

## 1. Research concepts

| Concept | Key sources | What flagman takes | What it does not take |
|---|---|---|---|
| **Learning to Defer** | Madras, Pitassi, Zemel, *Predict Responsibly*, NeurIPS 2018 ([arXiv:1711.06664](https://arxiv.org/abs/1711.06664)); Mozannar & Sontag, *Consistent Estimators for Learning to Defer to an Expert*, ICML 2020 ([arXiv:2006.01862](https://arxiv.org/abs/2006.01862)) | ⚠ **Deferring is a choice with a cost, not a class.** The cost of asking a human and the cost of a wrong autonomous step are different numbers, and the thresholds follow from their ratio → `costs` in the config, [ADR 0006](adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md) | Training a rejector jointly with a classifier. There is no classifier here, and a personal log has too few decisions to learn one |
| **Reject option, ambiguity vs. novelty** | Hendrickx et al., *Machine Learning with a Reject Option: A Survey*, Machine Learning 113, 2024 ([arXiv:2107.11277](https://arxiv.org/abs/2107.11277)) | The rule set covers both kinds: *ambiguity* (failing tests, large diff, missing acceptance criteria) and *novelty* (a path an ADR guards, a revert, a hotspot) | — |
| **Selective prediction** | Geifman & El-Yaniv, *Selective Classification for Deep Neural Networks*, NeurIPS 2017 ([arXiv:1705.08500](https://arxiv.org/abs/1705.08500)); Traub et al., *Overcoming Common Flaws in the Evaluation of Selective Classification Systems*, 2024 ([arXiv:2407.01032](https://arxiv.org/abs/2407.01032)) | ⚠ **Coverage and risk are measured together, never one alone.** flagman reports "left to the AI alone" (coverage) beside "false autonomy" (the risk that matters). Traub et al. state the selection score does not have to be a model probability, so a rule score qualifies | AURC / AUGRC curves. They need many labelled decisions to be stable; a personal log starts with none. A candidate once a project has hundreds |
| **Calibration** | Guo et al., *On Calibration of Modern Neural Networks*, ICML 2017 ([PMLR 70](https://proceedings.mlr.press/v70/guo17a.html)) | ⚠ **What it says not to do:** a rule score is not a probability, so ECE on it means nothing. flagman never prints its score as a probability or a confidence ([ADR 0004](adr/0004-the-score-is-a-sum-of-named-findings-not-a-probability.md)) | Isotonic regression of score → "needed a human" rate. Possible later; with few labels it would fit noise |
| **Asking in coding agents** | Vijayvargiya et al., *Ambig-SWE*, 2025 ([arXiv:2502.13069](https://arxiv.org/abs/2502.13069)): agents rarely ask unprompted, and asking improved resolution by up to 74 % on underspecified tasks; Edwards & Schuster, *Ask or Assume?*, 2026 ([arXiv:2603.26233](https://arxiv.org/abs/2603.26233)) *(abstract only)* | ⚠ **The agent's own sense of "I am unsure" is the weak link** — so the judgement is made outside the agent, from evidence. Missing acceptance criteria and undecided points in the task are rules | An LLM judge of ambiguity |
| **HiL-Bench** | Trinh et al. (Scale AI), *HiL-Bench: Do Agents Know When to Ask for Help?*, 2026 ([arXiv:2604.09408](https://arxiv.org/abs/2604.09408), [data](https://huggingface.co/datasets/ScaleAI/hil-bench)) | ⚠ **The two failure directions, measured separately**: Ask-F1 = harmonic mean of question precision (not asking needlessly) and blocker recall (not missing what needed asking). flagman reports *unnecessary deferral* and *false autonomy* as separate rates, never folded into one number | Its tasks and oracle. It benchmarks agents; flagman is a gate an agent calls |
| **AI Control** | Greenblatt et al., *AI Control*, ICML 2024 ([arXiv:2312.06942](https://arxiv.org/abs/2312.06942)) | The three bands (act alone / defer to something trusted / human audit) have the same shape as continue / review / defer | Adversarial-agent threat model. flagman assumes a cooperative agent ([SPEC §2](SPEC.md)) |
| **Reflexion / ExpeL** | Shinn et al., *Reflexion*, NeurIPS 2023 ([arXiv:2303.11366](https://arxiv.org/abs/2303.11366)); Zhao et al., *ExpeL*, AAAI 2024 | ⚠ **Learning without weight updates**: keep what happened, look at it next time. flagman's version is the log plus `suggest`: which rules fired in the false autonomies, and which false autonomies no rule saw | Free-text reflections fed back into a prompt. flagman's improvements are config changes a human adopts, so they can be replayed |
| **Cost-sensitive evaluation** | Elkan, *The Foundations of Cost-Sensitive Learning*, IJCAI 2001; NIST detection cost function | A weighted confusion matrix (`costs`) — but ⚠ **as a tie-breaker only**, after false autonomy and missed escalation ([ADR 0006](adr/0006-a-change-is-better-only-if-false-autonomy-does-not-rise-on-decisions-it-was-not-fitted-on.md)) | A single blended score as the yardstick |
| **Offline evaluation from logs** | Li et al., *Unbiased Offline Evaluation of Contextual-bandit-based News Article Recommendation*, WSDM 2011 ([arXiv:1003.5956](https://arxiv.org/abs/1003.5956)) | ⚠ **A log only shows the branch that was taken.** When flagman deferred and the human approved, nobody saw what proceeding alone would have done. This is why truth comes from human labels and decisions, not from outcomes alone ([ADR 0005](adr/0005-ground-truth-is-a-label-then-an-outcome-then-the-human-decision.md)) | Propensity-weighted estimators. The policy here is deterministic; there is no exploration to weight |
| **Rule backtesting in production** | Lithic, [Backtesting Authorization Rules](https://docs.lithic.com/docs/backtesting-authorization-rules); Panther, [Data Replay](https://docs.panther.com/detections/testing/data-replay); DoorDash rules engine *(article not readable, snippet only)* | ⚠ **Store the inputs immutably, re-run a draft rule set over them, compare the two.** That is `flagman replay --compare` | A shadow/live rollout. One developer does not need staged rollout |
| **Defect hotspots** | Tornhill, *Your Code as a Crime Scene*; Lewis et al., *Does Bug Prediction Support Human Developers?*, ICSE 2013 | Frequently changed paths and recent reverts are rules. ⚠ **Lewis et al. found developers ignored predictions whose reasons were not obvious and actionable** — so every finding names the path or line that triggered it | A trained defect predictor |
| **Measured agent stops** | Anthropic, *Measuring AI agent autonomy in practice*, 2026-02 ([post](https://www.anthropic.com/research/measuring-agent-autonomy)) | Why agents stop in practice (choosing between options, ambiguity, credentials, approval before acting) — a reminder that the "ask" categories are mostly about *decisions*, not about code quality | Its percentages. They describe another population |

## 2. Existing tools

| Tool | What it does, and how it decides | Closest to flagman in | ⚠ Where flagman differs |
|---|---|---|---|
| Claude Code hooks (PreToolUse `ask` / `deny` / `allow`) — [docs](https://code.claude.com/docs/en/hooks) | The plumbing to stop a tool call and ask. No judgement of its own | Where the answer is delivered | flagman *is* a judgement that can be plugged into it (`flagman hook claude-code`). It never answers `allow` |
| OpenAI Codex approval modes, Cursor run modes, Aider confirmations | Ask by command category or sandbox boundary, or always / never | — | They look at *what command*, not at *what this change is*: no diff, tests, task or history |
| [HumanLayer](https://github.com/humanlayer/humanlayer) | Wraps chosen functions so a human must approve them (Slack, email) | Delivering the question to a human | The functions to gate are fixed in code; there is no evidence, scoring or after-the-fact measurement. README says the SDK is largely deprecated *(as of the survey)* |
| [cupcake](https://github.com/eqtylab/cupcake) (EQTY Lab, Apache-2.0) | Agent hooks → collected "signals" (branch, CI state) → Rego policies → Allow / Block / Warn / Require Review | ⚠ **The closest design** | Policies decide on the spot; there is no decision log tied to outcomes, and no replay of a changed policy over past decisions |
| [Microsoft Agent Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit) (MIT) | Pre-execution policy (YAML / Rego / Cedar), `require_approval`, tamper-evident audit log | The audit trail | An enterprise governance platform. No re-scoring of past decisions under a new policy |
| [Danger](https://danger.systems/js/), [conftest / OPA](https://www.openpolicyagent.org/) | Hand-written rules over a PR or structured input → pass / warn / fail | Rules over a diff | Immediate pass/fail; nothing records whether the rule was right |
| [gitrisk](https://github.com/jacobarthurs/gitrisk) (Go, MIT), [BlastGuard](https://github.com/kearns2000/blastguard) (.NET, MIT), [hidetzu/riskcheck](https://github.com/hidetzu/riskcheck) | Weighted risk score of a diff from history and path rules | The diff evidence | ⚠ **A risk score, not a decision.** None asks "may the AI go on alone", none ties the score to what happened, none measures a threshold change |
| [hidetzu/decide-first](https://github.com/hidetzu/decide-first) | Is this issue ready to hand to an agent? (before the work) | Reading a task for scope, acceptance criteria, open decisions | Runs once, before the work. flagman runs at each step during the work, and also looks at the diff, tests and history |
| ADR tools (adr-tools, log4brains, MADR), agent audit-log libraries (auditlog-ai, agent-decision-log) | Records of decisions | The log | ⚠ **None attaches an outcome to a decision afterwards and evaluates the rule that made it** |
| Claude Code auto mode *(secondary sources)* | A model classifies each action | — | An LLM classifier; its reasons cannot be replayed under a different rule set |

⚠ **What no surveyed tool does**: keep each decision's evidence, attach what the human did and
what happened later, and re-run a changed rule set over that history to say — with numerator and
denominator — whether false autonomy went down and what it cost. That loop is the product.

## 3. Name

⚠ **Checked on 2026-10-11** with `gh search`, the npm, PyPI, crates.io, Homebrew and AUR APIs,
pkg.go.dev, and web search. No trademark registry was checked.

| Name | Finding | Verdict |
|---|---|---|
| `seatbelt` (working name) | ⚠ **Apple's macOS sandbox is called Seatbelt, and the coding-agent tools the users of this CLI already run (Claude Code, Codex, OpenCode) name it in their sandboxing docs.** Many 2026 GitHub repositories use "seatbelt" for agent sandboxes and agent guardrails, at least one of them a Go CLI hook; a commercial product installs a `/seatbelt` command into agents; npm, PyPI and crates.io names are taken; `GhostPack/Seatbelt` is a well-known security tool | ⚠ **Likely to be confused.** To this audience "seatbelt" already promises *the agent is sandboxed*, which is not what this tool does |
| `flagman` | No AI or developer-tool project found under the name. npm `flagman` is a dormant unrelated package; PyPI, crates.io, Homebrew, AUR and `github.com/hidetzu/flagman` were free | ⚠ **Chosen** ([ADR 0001](adr/0001-the-tool-is-named-flagman-not-seatbelt.md)) |
| `umpire`, `holdpoint`, `handrail`, `tollgate`, `arbiter`, `warden`, `handoff`, `greenlight` | Each already names an AI-agent gate, guardrail or hand-off tool, or a large developer tool | Rejected |
| `signalman`, `referee`, `pitstop`, `deferral`, `kickstand` | No agent-gating collision; exact names taken on one or more registries, or a weak metaphor | Not chosen |
