# Local-API Remote / Backup Strategy

## Current State

This repository is standalone and can run without a monorepo. A remote may or may not
be configured.

## Recommended Strategy

### Short Term

- Keep local commits small and frequent.
- Export patches or bundle archives when offline.
- Keep `README.md`, `AGENTS.md`, and configuration examples current.

### Preferred Medium Term

Choose one:

1. Public remote repository (recommended for open service reuse).
2. Private remote repository (if distribution is restricted).
3. Mirror into a larger mono-remote only if required.

### Not Recommended

- Leaving the service only as an unbacked local repository.

## Minimum Operational Rule

Before large edits:

1. check `git status`
2. commit locally in small steps
3. verify example configs stay generic and do not include machine-specific paths

## Pending Decision

Decide whether the official remote is public or private, and record the URL here once created.
