# Email workspace and background sync implementation plan

**Goal:** Run mail import independently of HTTP requests and make every scanned mail's outcome inspectable in an email-client-style workspace.

**Architecture:** Persist a sync task and a separate folder/message scan index without changing import identity or deleting existing accounting data. A service-owned worker uses its own bounded background context, streams downloaded messages into the existing pipeline, and writes progress during scanning. Read-only APIs expose task status, paginated message metadata and retained body/processing evidence.

**Stack:** Go, xorm/SQLite, IMAP, Vue 3, Vuetify, existing i18n.

## 1. Background worker and scan evidence

- Add `pkg/models/email_bill_sync.go`: task records (owner, state, timestamps, counters, folder progress, error) and message scan index (owner, folder, UIDVALIDITY/UID identity, headers, outcome, linked pipeline message/run). Register both in `cmd/database.go` using the existing additive schema synchronization.
- Add `pkg/services/email_bill_sync.go`: one active task per owner, immediate task creation, background execution, persisted completion/failure and restart interruption recovery. Schedule and manual requests enter the same coordinator; repeated clicks return the existing task. Status polling never starts work.
- Extend `pkg/emailbill/mailbox.go` with scan events and a message handler. Record folder discovery, selected folder, scanned headers, authentication/matcher/duplicate/size skips and failures. Fully finish each IMAP fetch before running the message handler. Process each downloaded batch immediately; retain earlier progress if later network work fails. Bound all command waits and the overall worker lifetime.
- Extend `pkg/services/email_bill_importer.go`: keep legacy callers working; use the streaming handler and observer for background work. Link index rows to the existing pipeline IDs and final candidate outcomes. Preserve raw-body retention settings and mail identity deduplication.
- Add API handlers/routes for latest task, message list/detail. Existing run endpoint returns a task object immediately. Check current user ownership on every route, bound page size, and never return credentials or unsanitized HTML.
- Add focused regression cases for background lifetime and duplicate start, scan skip visibility, streaming before full scan completes, and partial progress on failure. User verification policy forbids running compiled Go tests; review source and run gofmt/diff checks only.
- Commit this coherent backend unit after static review.

## 2. Mailbox workspace

- Add `src/components/desktop/EmailBillMailbox.vue`; isolate mailbox browsing and polling from the already large settings editor. Add shared TS task/message/detail types and service methods.
- Desktop layout: folder navigation | paginated messages | reading and processing pane. On narrow screens use a folder selector and stacked list/detail. Reuse application typography, surface/background colors and primary selection color; status colors carry processing meaning. No external fonts, decorative hero or duplicate card wrappers.
- Make the workspace the first tab. Put task state, scanned/processed counts, current folder, elapsed time, last update and error beside the run action. Starting a task returns immediately; periodically refresh task and current message page with one timer and no overlapping requests. Refreshing/reopening restores task state. Stop the timer on unmount and preserve current message selection.
- Expose sender, subject, folder, authentication, processing status/reason, retained plain-text body and parser/candidate results. Use text rendering only. Distinguish unseen, skipped, no parser output, awaiting review and imported; show clear empty and unavailable-body states. Keep existing rule editing and review actions available.
- Add English and Chinese interface strings, reuse existing strings for common controls. Run targeted ESLint, full `vue-tsc --noEmit`, JSON validation and `git diff --check`; do not build or package.
- Commit the frontend unit after static review.

## 3. Final checks

- Read current SSH logs and report the observed live task outcome separately from unshipped changes.
- Inspect final diffs for owner scoping, credentials/raw HTML exposure, worker/context cleanup, finite polling and schema compatibility.
- Update `docs/email-bill-importer.md` with background task semantics, scan visibility, folder handling, restart behavior and raw-body retention.
- Record completed validation and commit state. Deployment/build remains outside the user's static-only verification authorization.
