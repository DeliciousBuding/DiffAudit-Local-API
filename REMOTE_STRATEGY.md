# Local-API Remote / Backup Strategy

## Current State

`D:\Code\DiffAudit\Services\Local-API` is now an independent local Git repository.

At the moment:

- local commit history exists
- no remote is configured
- no automatic mirror or backup policy is configured

This is acceptable for short-term local iteration, but not strong enough for long-term maintenance.

## Recommended Strategy

### Short Term

- Keep the local repository as the working source of truth
- Export periodic patches or bundles when making meaningful changes
- Record important interface and operational changes in:
  - `README.md`
  - `AGENTS.md`
  - `HANDOFF.md` in the corresponding agent state

### Preferred Medium Term

Choose one of these:

1. Dedicated private remote repository
   - best if `Services/Local-API` will continue growing independently
2. Mirror into a monorepo-managed service remote
   - best if you want service code tracked alongside other infrastructure repos

### Not Recommended

- Leaving the service only as an unbacked local Git repo indefinitely

## Minimum Operational Rule

Before large edits:

1. check `git status`
2. commit locally in small steps
3. keep `README.md` and `AGENTS.md` current

## Pending Decision

The team leader still needs to decide whether `Services/Local-API` should:

- remain local-first with manual backup
- or get a dedicated remote repository
