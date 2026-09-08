# Email Bill Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fixed CMB email importer with a user-configurable, auditable, idempotent email-to-transaction pipeline integrated directly into ezBookkeeping.

**Architecture:** Store mail ingestion, immutable parser versions, parser execution evidence, logical bill candidates and exact variants in the per-user data database. Execute user parsers in a bounded Starlark sandbox, aggregate every matching parser result, route accounts, classify by versioned rules before the existing LLM provider, and atomically bind one candidate to one native transaction.

**Tech Stack:** Go 1.27, XORM, SQLite/MySQL/PostgreSQL, Starlark-Go, Gin API, Vue 3/TypeScript, Vitest.

---

### Task 1: Domain identities, variants, and database invariants

**Files:**
- Create: `pkg/models/email_bill_automation.go`
- Create: `pkg/emailbill/identity.go`
- Test: `pkg/emailbill/identity_test.go`
- Modify: `cmd/database.go`

- [ ] Write failing table-driven tests proving message identities prefer Message-ID, fallback identities are normalized and versioned, candidate identity excludes mutable parsed fields, and exact fingerprints include parsed fields.
- [ ] Run `go test ./pkg/emailbill -run 'Test(MessageFingerprint|CandidateIdentity|BillFingerprint)'` and confirm the new symbols are missing.
- [ ] Implement canonical normalization and SHA-256 identity helpers plus XORM models for mailbox, inbound message, parser rule/version/run/output, import run/event, candidate/variant/evidence/conflict, routing/classification decisions, LLM run, category proposal/claim, confirmation, import intent/attempt, and audit event.
- [ ] Register every new model in `updateAllDatabaseTablesStructure`.
- [ ] Re-run the focused tests and `gofmt -w` on changed Go files.
- [ ] Commit as `feat: add email bill automation domain model`.

### Task 2: Versioned parser rules and bounded Starlark runtime

**Files:**
- Create: `pkg/emailbill/script.go`
- Create: `pkg/emailbill/script_test.go`
- Create: `pkg/emailbill/standard_bill.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] Write failing tests for matcher selection, multiple standard bills, invalid schemas, prohibited `load`, execution-step cancellation, output limits, and deterministic helper functions.
- [ ] Run `go test ./pkg/emailbill -run 'TestScript'` and confirm the runtime does not exist.
- [ ] Add `go.starlark.net`, expose only immutable mail values and safe helpers, require `parse(mail)`, reject unknown fields, and validate amount, currency, timestamp, direction, and account hints.
- [ ] Keep filesystem, network, process, environment, database, and module loading unavailable.
- [ ] Re-run focused tests and commit as `feat: add sandboxed email parser runtime`.

### Task 3: Persistent ingestion and independent parser execution

**Files:**
- Create: `pkg/services/email_bill_pipeline.go`
- Create: `pkg/services/email_bill_pipeline_test.go`
- Create: `pkg/services/email_bill_repository.go`
- Modify: `pkg/services/email_bill_importer.go`

- [ ] Write failing tests proving duplicate IMAP fetches create one inbound message, every matched parser version runs once per import run, one parser failure does not stop others, and unauthenticated mail never reaches scripts.
- [ ] Run the focused service tests and confirm they fail for missing pipeline behavior.
- [ ] Persist message identity before parsing, append run events, snapshot parser versions, execute all matching rules independently, and persist immutable parser outputs.
- [ ] Adapt built-in CMB parsers behind the same standard parser contract without direct account/category access.
- [ ] Re-run focused tests and commit as `feat: persist independent email parser runs`.

### Task 4: Candidate aggregation and conflict resolution

**Files:**
- Create: `pkg/emailbill/aggregate.go`
- Create: `pkg/emailbill/aggregate_test.go`
- Modify: `pkg/services/email_bill_pipeline.go`
- Modify: `pkg/services/email_bill_repository.go`

- [ ] Write failing tests for identical multi-parser results, conflicting variants with the same identity, distinct bills in one message, and repeated parser versions.
- [ ] Run the focused tests and verify expected failures.
- [ ] Upsert one logical candidate per identity, one exact variant per fingerprint, link all evidence, and force multiple variants into `awaiting_confirmation` until a selected variant exists.
- [ ] Re-run focused tests and commit as `feat: aggregate parser evidence and conflicts`.

### Task 5: Account routing and reversible classification rules

**Files:**
- Create: `pkg/services/email_bill_routing.go`
- Create: `pkg/services/email_bill_routing_test.go`
- Create: `pkg/services/email_bill_classification.go`
- Create: `pkg/services/email_bill_classification_test.go`

- [ ] Write failing tests for routing priority/scope, unresolved accounts, manual-over-learned ordering, exact/contains/regex matching, disabled rule versions, and immutable decision history.
- [ ] Run the focused tests and verify expected failures.
- [ ] Implement canonical rule signatures, versioned routing/classification rules, deterministic priority, current decision pointers, and append-only decision/audit rows.
- [ ] Re-run focused tests and commit as `feat: add auditable bill routing and rules`.

### Task 6: LLM fallback, category proposals, and learned mappings

**Files:**
- Create: `pkg/services/email_bill_llm.go`
- Create: `pkg/services/email_bill_llm_test.go`
- Modify: `pkg/services/transaction_categories.go`

- [ ] Write failing tests proving LLM is skipped after rule hits, receives the user's current type-compatible categories, validates structured responses, reuses existing categories, creates a claimed category only above threshold, deduplicates concurrent proposals, and records a reversible learned rule.
- [ ] Run the focused tests and verify expected failures.
- [ ] Reuse ezBookkeeping's configured LLM provider, store prompt/category snapshots without credentials, create category claims in the user database, and persist proposal, decision, and learned-rule versions.
- [ ] Route low confidence, invalid output, or provider failure to `awaiting_confirmation`.
- [ ] Re-run focused tests and commit as `feat: classify email bills with llm fallback`.

### Task 7: Atomic native transaction import

**Files:**
- Modify: `pkg/services/transactions.go`
- Create: `pkg/services/email_bill_transaction_import.go`
- Create: `pkg/services/email_bill_transaction_import_test.go`

- [ ] Write failing concurrency and crash-boundary tests proving one candidate owns one import intent and one transaction, while failed attempts remain retryable.
- [ ] Run the focused tests and verify expected failures.
- [ ] Extract transaction creation that accepts an existing XORM session, lock/create the intent, create the native transaction, bind its ID, append the attempt/audit event, and commit everything in one user-data transaction.
- [ ] Keep legacy comment-marker detection only for migration compatibility.
- [ ] Re-run focused tests and commit as `feat: import email bills exactly once`.

### Task 8: User APIs and parser test bench

**Files:**
- Create: `pkg/api/email_bill_automation.go`
- Create: `pkg/api/email_bill_automation_test.go`
- Modify: `cmd/webserver.go`

- [ ] Write failing API tests for mailbox CRUD, parser/version CRUD, parser dry-run, routing/classification rule CRUD, candidate confirmation, retry, audit retrieval, and user ownership checks.
- [ ] Run focused API tests and verify expected failures.
- [ ] Add authenticated APIs with redacted credentials; ensure parser tests stop after schema/fingerprint preview and cannot create routing, LLM, learning, category, or transaction rows.
- [ ] Re-run focused tests and commit as `feat: expose email bill automation api`.

### Task 9: Settings UI matching ezBookkeeping

**Files:**
- Create: `src/views/user/settings/email-bill/EmailBillSettings.vue`
- Create: `src/views/user/settings/email-bill/ParserRules.vue`
- Create: `src/views/user/settings/email-bill/ParserTestBench.vue`
- Create: `src/views/user/settings/email-bill/RoutingRules.vue`
- Create: `src/views/user/settings/email-bill/ClassificationRules.vue`
- Create: `src/views/user/settings/email-bill/ImportHistory.vue`
- Create: `src/stores/emailBill.ts`
- Create: `src/core/emailBill.ts`
- Modify: existing settings navigation and `src/locales/zh_Hans.json`, `src/locales/en.json`
- Test: focused Vitest files beside the new store/core modules

- [ ] Write failing store/core tests for API payload normalization, string IDs, redaction, rule version display, dry-run isolation, conflict display, and learned-rule disable/edit/delete behavior.
- [ ] Run the focused no-production-build test command and verify expected failures.
- [ ] Implement tabs for mailboxes, parser rules/test bench, account routes, classification policy/learned rules, pending confirmation, and immutable import audit.
- [ ] Re-run focused lint/type/tests and commit as `feat: add email bill automation settings ui`.

### Task 10: Scheduling, privacy, migration, and release gate

**Files:**
- Modify: `pkg/cron/cron_container.go`
- Modify: `pkg/settings/email_bill_setting.go`
- Modify: documentation and deployment examples
- Add focused tests in corresponding packages

- [ ] Write failing tests for daily/weekly schedules, retry-from-stage, raw-mail retention, legacy CMB settings migration, and feature gating before schema readiness.
- [ ] Run focused tests and verify expected failures.
- [ ] Wire configurable schedules, retention cleanup, migration of existing CMB configuration into built-in parser/routing records, and keep the feature hidden until all required schema/API/UI pieces are present.
- [ ] Run static checks and approved focused tests, inspect the branch diff and commit history, then commit as `feat: complete auditable email bill automation`.

