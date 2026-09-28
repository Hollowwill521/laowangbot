# Monitor/QDSG Go remote plugins

User-approved design: preserve source-plugin functionality, Go independent executables, Python OCR on demand, no Node. Fixed project catalog. Increment core to 0.1.1 and plugins to 1.0.0; changelog required for future changes.

- [ ] Define versioned capability-limited localhost host bridge and shared Go SDK; expose entity/message/history/button/media/WebView operations, never Telegram credentials.
- [ ] Port monitor commands/filtering/dedup/Bot API callback/leader sync and legacy state schema with tests.
- [ ] Port qdsg cron/task management/flows/OCR/AI/WebView/notifications and legacy state schema with tests.
- [ ] Publishable six-platform Go plugin artifacts, same-project Release URLs+checksums, catalog variants, local build examples.
- [ ] Upgrade migration of legacy state into plugin-private directories and four-language docs.
- [ ] Fix tpm built-in/unknown/manual update classification, preserve owner-only authorization.
- [ ] Unit/race/integration/coverage, cross-builds, review; record untested real Telegram/AI interactions. No silent removal of source features.

Interfaces: pkg/pluginapi SDK communicates through LAOWANGBOT_HOST_URL and LAOWANGBOT_HOST_TOKEN, capability names in manifest. Request/Response for stdio remain protocol_version 1; HTTP bridge methods individually allowlisted by host. Plugins share no source files. Caller owns core/pkg SDK integration, child owns each plugin.
