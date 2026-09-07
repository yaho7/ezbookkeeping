# Integrated Email Bill Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move CMB email bill importing into the ezBookkeeping Go server so one image, one process, one database, and one Compose service provide the complete feature.

**Architecture:** A new `pkg/emailbill` package owns MIME/IMAP input and deterministic CMB parsing. A service adapter resolves the configured ezBookkeeping user, checks stable markers in the existing transaction table, and writes through `TransactionService`; an existing gocron scheduler job invokes it. Configuration follows ezBookkeeping's INI/environment conventions, while the deployment file stays intentionally minimal.

**Tech Stack:** Go 1.27, `github.com/emersion/go-imap` v1, net/mail and MIME packages, xorm-backed existing transaction service, go-co-op/gocron, Docker Compose, GitHub Actions.

---

### Task 1: Built-in CMB parser

**Files:**
- Create: `pkg/emailbill/models.go`
- Create: `pkg/emailbill/parsers.go`
- Test: `pkg/emailbill/parsers_test.go`

- [ ] **Step 1: Write failing parser tests**

Cover allowed sender/subject matching, credit-card expense/refund, debit expense/income, funds aggregation with its postfixed date, decimal-to-minor-unit rounding, and year rollover for `MM月DD日` dates.

```go
func TestCMBCreditParserExpense(t *testing.T) {
    txs, err := NewCMBCreditParser().Parse(
        "2026/09/06 11:03:15 CNY 1.53 尾号5460 消费 麦当劳 (每日邮件)",
        time.Date(2026, 9, 7, 9, 0, 0, 0, shanghai),
    )
    require.NoError(t, err)
    assert.Equal(t, int64(-153), txs[0].AmountMinor)
}
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run: `go test ./pkg/emailbill -run 'TestCMB|TestMoney'`

Expected: compilation fails because the package API does not exist.

- [ ] **Step 3: Implement deterministic parsers**

Define `ParsedTransaction`, `Message`, and a `Parser` interface. Port only the CMB credit/debit rules actually enabled in `../billCheckFormMail`; use integer minor units and timezone-aware transaction timestamps.

```go
type ParsedTransaction struct {
    Source      Source
    OccurredAt time.Time
    AmountMinor int64
    Merchant   string
    Description string
}
```

- [ ] **Step 4: Run focused tests and confirm GREEN**

Run: `go test ./pkg/emailbill -run 'TestCMB|TestMoney'`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/emailbill
git commit -m "feat: add built-in CMB email parsers"
```

### Task 2: Native configuration and IMAP mailbox

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `pkg/settings/setting.go`
- Modify: `conf/ezbookkeeping.ini`
- Create: `pkg/emailbill/mailbox.go`
- Test: `pkg/settings/email_bill_test.go`
- Test: `pkg/emailbill/mailbox_test.go`

- [ ] **Step 1: Write failing configuration and mailbox tests**

Verify disabled-by-default settings, required fields when enabled, known IMAP/auth-service inference, custom trusted auth-service domains, MIME text extraction, and rejection of forged `Authentication-Results` domains such as `message.cmbchina.com.evil`.

```go
func TestAuthenticationResultRejectsSuffixAttack(t *testing.T) {
    header := "mx.qq.com; dkim=pass header.d=message.cmbchina.com.evil"
    assert.False(t, hasBankAuthenticationResult(header, []string{"qq.com"}))
}
```

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `go test ./pkg/settings ./pkg/emailbill -run 'TestEmailBill|TestAuthentication|TestMIME'`

Expected: FAIL because native email-bill configuration and mailbox do not exist.

- [ ] **Step 3: Add stable IMAP dependency and implementation**

Add `github.com/emersion/go-imap v1.2.1`. Search by supported sender, fetch newest candidates read-only, parse RFC822/MIME bodies, apply per-parser limits, and trust only the first authentication result from a configured auth-service domain with exact bank-domain boundaries.

- [ ] **Step 4: Add `[email_bill]` settings**

Support INI keys and automatic `EBK_EMAIL_BILL_*` overrides for `enabled`, `target_user`, mailbox credentials/server/port, account/category IDs, timezone, five-field cron expression, limit, authentication requirement, and trusted auth-service domains. Reject incomplete enabled configurations at startup.

- [ ] **Step 5: Run tests and confirm GREEN**

Run: `go test ./pkg/settings ./pkg/emailbill -run 'TestEmailBill|TestAuthentication|TestMIME'`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum pkg/settings/setting.go conf/ezbookkeeping.ini pkg/emailbill
git commit -m "feat: add native email bill mailbox settings"
```

### Task 3: Main-database transaction import and cron integration

**Files:**
- Create: `pkg/services/email_bill_importer.go`
- Test: `pkg/services/email_bill_importer_test.go`
- Modify: `pkg/services/transactions.go`
- Modify: `pkg/cron/cron_jobs.go`
- Modify: `pkg/cron/cron_container.go`
- Test: `pkg/cron/cron_container_test.go`

- [ ] **Step 1: Write failing service and cron tests**

Use interfaces for mailbox, user lookup, marker lookup, and transaction creation. Verify target-user resolution, account/category mapping, marker idempotency, one malformed message not stopping others, and registration only when enabled.

```go
func TestImporterSkipsExistingMarker(t *testing.T) {
    sink := &fakeSink{existing: true}
    summary, err := importer.Run(context, config, mailbox, sink)
    require.NoError(t, err)
    assert.Equal(t, 1, summary.Skipped)
    assert.Empty(t, sink.created)
}
```

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `go test ./pkg/services ./pkg/cron -run 'TestEmailBill'`

Expected: FAIL because the importer service and job do not exist.

- [ ] **Step 3: Implement direct transaction creation**

Resolve `target_user`, create `models.Transaction` values with existing account/category IDs, and call `Transactions.CreateTransaction` directly. Before creation, query the same database for the exact `[ebk-mail:<digest>]` comment marker; no API token or secondary state database is used.

- [ ] **Step 4: Register native cron job**

Create a cron-expression job using the configured timezone and `gocron.WithSingletonMode`. Keep the importer disabled unless `[email_bill].enabled=true`.

- [ ] **Step 5: Run focused tests and confirm GREEN**

Run: `go test ./pkg/services ./pkg/cron -run 'TestEmailBill'`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/services pkg/cron
git commit -m "feat: import email bills inside ezBookkeeping"
```

### Task 4: Remove sidecar deployment and simplify delivery

**Files:**
- Delete: `integrations/email-bill-importer/`
- Delete: `.env.example`
- Modify: `compose.yml`
- Modify: `.github/workflows/docker-publish.yml`
- Modify: `README.md`
- Modify: `docs/email-bill-importer.md`
- Delete: `docs/superpowers/plans/2026-09-07-email-bill-importer.md`

- [ ] **Step 1: Replace Compose with the exact single-service shape**

```yaml
services:
  ezbookkeeping:
    image: ghcr.io/yaho7/ezbookkeeping:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/ezbookkeeping/data
      - ./storage:/ezbookkeeping/storage
      - ./conf/ezbookkeeping.ini:/ezbookkeeping/conf/ezbookkeeping.ini:ro
```

- [ ] **Step 2: Publish only the integrated image**

Keep push/workflow-dispatch triggers and multi-architecture GHCR publishing, but remove the matrix and importer image target.

- [ ] **Step 3: Rewrite operator documentation**

Document `[email_bill]` configuration, main-process cron execution, main-database marker idempotency, minimal Compose startup, logs, and `ezbookkeeping cron run EmailBillImport` for a manual run.

- [ ] **Step 4: Remove obsolete sidecar artifacts**

Delete Python code/tests/Dockerfile, the sidecar plan, and API-token `.env` sample so there is one supported architecture.

- [ ] **Step 5: Run static delivery checks**

Run: `docker compose config --quiet`, YAML parsing, `go test` for touched packages, and `git diff --check`.

Expected: all checks pass; no local production build is run.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: ship email import in one service"
```

### Task 5: Final verification and review

**Files:**
- Review all files changed since `main`

- [ ] **Step 1: Run focused Go tests**

Run: `go test ./pkg/emailbill ./pkg/settings ./pkg/services ./pkg/cron`

Expected: PASS.

- [ ] **Step 2: Run non-build delivery checks**

Run Compose expansion, workflow YAML parsing, `gofmt` check, `go vet` for touched packages if it does not require prohibited production packaging, and `git diff --check`.

- [ ] **Step 3: Request code review**

Review configuration safety, MIME parsing, authentication boundary checks, marker idempotency, transaction mapping, scheduler registration, and exact Compose scope. Resolve all Critical and Important findings before integration.

- [ ] **Step 4: Merge and push after approval**

Fast-forward `main`, rerun focused tests on the merged result, push `origin/main`, and confirm the push-triggered Docker workflow exists.
