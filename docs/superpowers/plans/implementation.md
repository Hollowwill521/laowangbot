# laowangbot Implementation Plan

User approved Go core + independent process plugins, no mandatory Node.js.
Work in isolated clone laowangbot, refactor/laowangbot; original checkout preserved.
Use writing-plans, test-driven-development and subagent-driven-development.

## Tasks
- [x] Platform: share legacy-compatible process locks across app/login; isolate Unix CPU calls; portable restart; six release targets; shell/PowerShell and Docker deployment. Preserve old config/environment support. Test lock contention, cross compilation, scripts and offline installation.
- [x] Plugin host: versioned JSON lines on stdin/stdout, command/interval/event requests, bounded output and timeout, per-plugin private state; manual manifest install; checksum-verified current-repository remote installs/updates; no automatic third-party code conversion. Tests use real helper processes and httptest downloads.
- [x] Migration: staged destination, unchanged source, config/session validation, copy mibot-lite state, reuse MiBox converters, inspect TeleBox formats and plugin inventory, map supported builtins to current repository and archive unsupported plugins with actionable report. Test collisions, invalid sources, symlinks, source preservation and partial failures.
- [x] Integration: separate CLI lifecycle/migration/plugin entrypoints; default Chinese comma while preserving explicit prefixes; register plugin commands/jobs without overriding builtins. Unit/integration tests and coverage.
- [x] Documentation: four rewritten READMEs; architecture/configuration/deployment/plugin protocol/migration coverage; preserve license and attribution, state publication and runtime validation limits.
- [x] Verification: go test -race ./..., integration tests, go test -coverprofile, go vet, six cross-builds, shell validation; independent review and fix findings.

Interfaces: plugin.Manager{Root string}, plugin.Manifest and plugin.Request/Response are host-owned; migration accepts converter and plugin inventory callbacks to avoid circular package dependencies. Platform code must avoid depending on command registration.

## Verification record
See docs/verification.md for exact commands, coverage and unverified runtime boundaries. No remote publication or actual account login performed.
