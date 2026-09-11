# Product Readiness Implementation Plan

> Execute inline with atomic commits; do not build, compile, or run compiled tests without explicit authorization.

**Goal:** Harden AI configuration and email automation before preparing upstream contributions.

**Architecture:** Keep the single-process deployment and native configuration, provider, and transaction APIs. Restrict global settings to an explicitly configured operator, use immutable request configuration, and separate email processing from permission to create transactions.

**Tech Stack:** Go, xorm, INI, Vue, TypeScript.

## Delivery units

- [ ] Configuration: serialize section updates, preserve environment ownership, restrict management, bind credentials to endpoints, construct providers from request snapshots, register stable AI routes. Files: pkg/settings/{llm_setting,email_bill_setting,setting,setting_container}.go; pkg/api/llm_settings.go; pkg/llm/large_language_model_provider_container.go; cmd/webserver.go; src/core/llm.ts; src/views/desktop/settings/LLMSettingsPage.vue.
- [ ] Task lifecycle: explicit cancellation, task identity checks, context propagation, read-only polling after orphan recovery. Files: pkg/services/email_bill_sync.go; pkg/api/email_bill_settings.go; src/components/desktop/EmailBillMailbox.vue; src/stores/emailBill.ts.
- [ ] Import policy: review by default, explicit opt-in for rule-based automatic imports, no implicit AI category creation or learning. Files: pkg/services/email_bill_finalize.go; pkg/settings/setting.go; pkg/models/email_bill_setting.go; email settings UI and translations.
- [ ] Documentation: configuration migration, operational limits, verification evidence and remaining upstream gates.

## Verification

Use gofmt, git diff --check, Vue no-emit typecheck, focused ESLint, JSON validation. Add regression test sources for cross-section saves, credential changes and cancellation state; execution remains pending explicit authorization because Go tests compile code. Do not claim runtime or database concurrency correctness from static checks.

## Further upstream gates

Per-user mailbox persistence, UID/UIDVALIDITY checkpoints, explicit reparse with immutable histories, full mobile settings, multi-database integration and restart/concurrency validation remain required before calling the whole feature upstream-ready. No deployment or PR publication in this implementation pass.

## Deployment migration for global settings

Global AI web management is disabled unless the deployment explicitly sets `[security] settings_management_user` to an existing username, or sets `EBK_SECURITY_SETTINGS_MANAGEMENT_USER`. This does not disable configured AI inference. An existing email importer retains its configured `target_user`; an unconfigured importer can only be claimed by the configured settings operator.

Environment-managed AI sections are read-only in the UI. Environment-managed email sections reject web saves. Changing an AI endpoint/provider or IMAP host/user requires entering the credential again to prevent forwarding a saved secret to another destination.

This delivery hardens configuration and improves routing forms, Unicode credit notification parsing, and AI draft diagnostics. It does not yet implement default-account classification, automatic retries, historical reparsing, or general AI extraction of unmatched mail. Do not describe those planned capabilities as shipped.
