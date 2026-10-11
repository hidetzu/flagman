# Security

flagman runs locally, reads a git working copy, and can run the test command a project puts in
its own config. The most serious bugs it can have are:

- **It sends something out that it should not**: any network call other than `gh issue view` for
  an issue named with `--task owner/repo#N` ([ADR 0007](docs/adr/0007-nothing-leaves-the-machine-unless-an-issue-is-named.md)).
- **It writes something into the decision log that it should not**: file contents, diff text,
  or the raw command line seen by the agent hook.
- **It runs something it was not told to**: any command other than `git`, `gh` (for a named
  issue), or `evidence.tests.command` when `--run-tests` is given.
- **The agent hook widens what an agent may do**: answering `allow`, or failing in a way that
  blocks the agent's own permission prompt.

## How to report

Use **GitHub's private vulnerability reporting** for this repository: the *Security* tab →
*Report a vulnerability*. Please do not open a public issue for a vulnerability.

## Out of scope

- flagman assumes a cooperative agent. An agent that passes `--tests pass` falsely, skips calling
  flagman, or edits `.flagman/` is outside what flagman can stop ([SPEC §2](docs/SPEC.md)).
- `evidence.tests.command` runs with your permissions by design; it is your project's command.

This is a personal project. Reports are read; there is no guaranteed response time.
