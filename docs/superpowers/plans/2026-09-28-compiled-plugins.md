# Compiled Plugins Implementation Plan

> Execute inline with superpowers:executing-plans; preserve concurrent pre-existing edits.

**Goal:** TPM installs Go source into the host binary; host updates rebuild installed sources.
**Architecture:** Reuse validated package storage, add a static registry and transactional build manager, replace process runtime at the product entry points.
**Tech Stack:** Go, Git, existing package ZIP/checksum helpers.
**Spec:** docs/superpowers/specs/2026-09-28-compiled-plugins.md

## Constraints
- Complete switch, explicit old-package migration errors; preserve state and manual sources.
- No Node runtime; builds require Go/Git. No implicit installation of system tools.
- Preserve all pre-existing working-tree changes; no automatic commit or publication.

## Tasks
- [x] Add v2 manifest validation and static plugin API/registry. First run tests that load a valid Go package and reject nested modules/legacy packages.
- [x] Add staged builds, static import generation, shared lock and recoverable binary/source replacement. Test real tiny Go builds, compiler failure, update preservation, rollback and state invariance.
- [x] Switch Telegram and CLI to compiled runtime and transactional manager; validate collision before swap. Exercise compiled handlers after changing/removing disk source.
- [x] Switch update to source release rebuild, protect installer paths, update help and package examples/catalog generation.
- [x] Run `go test ./... -coverprofile=...`, `go test -tags=integration ./...`, `go tool cover -func ...` and deployment checks. Report existing failures separately.

## Validation record

- Full unit and integration suites passed locally; core runtime/build race checks and go vet passed.
- Actual CLI compile/install/collision rejection/rollback/recovery exercised without Telegram access.
- Bash installer and migration wizard regressions passed. Windows cross-build passed; PowerShell execution unavailable.
- Version synchronized to 0.1.2; remote source publication remains pending.
