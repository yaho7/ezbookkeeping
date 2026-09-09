# Email Bill Settings UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native, responsive settings UI for configuring and running the integrated email bill importer without editing the INI file manually.

**Architecture:** Authenticated users configure the single built-in importer through new JSON endpoints. The server validates ownership of selected accounts/categories, atomically persists the `[email_bill]` INI section, swaps the immutable runtime configuration, and replaces the cron job immediately. Desktop and mobile pages share typed API models and schedule conversion helpers while using their existing Vuetify and Framework7 patterns.

**Tech Stack:** Go, Gin, gocron, ini.v1, Vue 3, TypeScript, Vuetify, Framework7, Vitest.

---

### Task 1: Runtime-safe configuration persistence

**Files:**
- Modify: `pkg/settings/setting.go`
- Modify: `pkg/settings/setting_container.go`
- Modify: `cmd/initializer.go`
- Test: `pkg/settings/setting_email_bill_test.go`

- [ ] Add failing tests proving email settings can be normalized, persisted without damaging other INI sections, reloaded, and swapped without mutating an existing config pointer.
- [ ] Run `go test ./pkg/settings -run 'TestEmailBill|TestConfigContainer'` and confirm the new tests fail for missing APIs.
- [ ] Add the config path, validation/persistence helpers, and copy-on-write container update.
- [ ] Re-run the focused tests and commit as `feat: persist email bill settings at runtime`.

### Task 2: Live scheduler and authenticated API

**Files:**
- Modify: `pkg/cron/cron_container.go`
- Modify: `pkg/cron/cron_container_test.go`
- Create: `pkg/models/email_bill_setting.go`
- Create: `pkg/api/email_bill_settings.go`
- Modify: `cmd/webserver.go`

- [ ] Add failing tests for enabling, replacing and disabling the importer cron job.
- [ ] Implement locked job replacement and expose authenticated get/update/run endpoints.
- [ ] On update, derive `target_user` from the current token, preserve a blank password, validate account/category ownership, persist settings, update runtime config and reschedule.
- [ ] Re-run focused Go tests and commit as `feat: expose email bill settings api`.

### Task 3: Shared frontend model and friendly schedule editor

**Files:**
- Create: `src/models/email_bill_setting.ts`
- Create: `src/lib/email_bill.ts`
- Create: `src/lib/email_bill.test.ts`
- Modify: `src/lib/services.ts`
- Create: `src/views/base/settings/EmailBillSettingsPageBase.ts`

- [ ] Add failing Vitest cases for daily, weekly and custom cron conversion.
- [ ] Implement typed API calls and a shared page controller that loads accounts/categories and validates required fields.
- [ ] Re-run `npx vitest run src/lib/email_bill.test.ts` and commit as `feat: add email bill settings client model`.

### Task 4: Matching desktop and mobile settings pages

**Files:**
- Create: `src/views/desktop/settings/EmailBillSettingsPage.vue`
- Create: `src/views/mobile/settings/EmailBillSettingsPage.vue`
- Modify: `src/views/desktop/settings/SettingsPageLayout.vue`
- Modify: `src/views/mobile/SettingsPage.vue`
- Modify: `src/router/desktop.ts`
- Modify: `src/router/mobile.ts`
- Modify: `src/locales/en.json`
- Modify: `src/locales/zh_Hans.json`
- Modify: `src/locales/zh_Hant.json`
- Modify: `docs/email-bill-importer.md`

- [ ] Add the navigation entries and responsive pages using existing controls and theme tokens.
- [ ] Provide enabled status, IMAP credentials, mapped account/category selects, daily/weekly/custom scheduling, security options, save, and run-now actions.
- [ ] Run `npx vue-tsc --noEmit`, `npx eslint` on changed frontend files, focused tests, Go formatting/tests, and `git diff --check`.
- [ ] Commit as `feat: add email bill settings ui`, inspect final history/status, and push `main`.
