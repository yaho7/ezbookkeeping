# Email Bill Importer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import supported China Merchants Bank email transactions directly into ezBookkeeping with durable deduplication, then publish both runtime images automatically from every pushed branch.

**Architecture:** A small Python sidecar reads IMAP mail, applies the two parsers currently enabled by `../billCheckFormMail`, and writes normalized transactions through ezBookkeeping's API. A local SQLite outbox persists message and transaction state; a stable marker in each transaction comment lets retries detect a remote write even if the importer stopped before recording success. Docker Compose runs ezBookkeeping and the importer as two services, while GitHub Actions builds both images and publishes default-branch tags to GHCR.

**Tech Stack:** Python 3.13 standard library, SQLite, IMAP, ezBookkeeping HTTP API, Docker Compose, GitHub Actions, GHCR.

---

### Task 1: Parse and normalize supported CMB email bills

**Files:**
- Create: `integrations/email-bill-importer/bill_importer/models.py`
- Create: `integrations/email-bill-importer/bill_importer/parsers.py`
- Create: `integrations/email-bill-importer/tests/test_parsers.py`

- [ ] **Step 1: Write failing parser tests**

Cover credit-card sign inversion, debit expense parsing, debit income parsing, sender/subject matching, and cent rounding:

```python
transactions = CmbCreditCardParser().parse(text, received_at)
self.assertEqual(transactions[0].amount_minor, -153)
self.assertEqual(transactions[0].merchant, "麦当劳")
```

- [ ] **Step 2: Run tests and confirm RED**

Run: `python3 -m unittest integrations/email-bill-importer/tests/test_parsers.py -v`

Expected: import failure because `bill_importer.parsers` does not exist.

- [ ] **Step 3: Implement immutable transaction models and parsers**

Use `Decimal` for amounts, timezone-aware `datetime`, longest non-overlapping matches, strict sender allowlists, and the exact parser scope currently enabled in the reference project: CMB daily credit-card mail and CMB debit-card notification mail.

- [ ] **Step 4: Run tests and confirm GREEN**

Run: `python3 -m unittest integrations/email-bill-importer/tests/test_parsers.py -v`

Expected: all parser tests pass.

- [ ] **Step 5: Commit**

```bash
git add integrations/email-bill-importer/bill_importer integrations/email-bill-importer/tests/test_parsers.py
git commit -m "feat: parse CMB email bill transactions"
```

### Task 2: Add durable IMAP-to-ezBookkeeping synchronization

**Files:**
- Create: `integrations/email-bill-importer/bill_importer/config.py`
- Create: `integrations/email-bill-importer/bill_importer/mailbox.py`
- Create: `integrations/email-bill-importer/bill_importer/ezbookkeeping.py`
- Create: `integrations/email-bill-importer/bill_importer/state.py`
- Create: `integrations/email-bill-importer/bill_importer/service.py`
- Create: `integrations/email-bill-importer/bill_importer/__main__.py`
- Create: `integrations/email-bill-importer/tests/test_config.py`
- Create: `integrations/email-bill-importer/tests/test_ezbookkeeping.py`
- Create: `integrations/email-bill-importer/tests/test_state.py`
- Create: `integrations/email-bill-importer/tests/test_service.py`

- [ ] **Step 1: Write failing configuration, API payload, state, and retry tests**

The tests must prove that required secrets/IDs are validated, an expense/income is mapped to the correct transaction type and category, pending rows survive restart, completed rows do not resend, and a remote marker found after a simulated crash closes the local outbox item without posting twice.

```python
payload = client.build_payload(transaction, settings)
self.assertEqual(payload["type"], 3)
self.assertEqual(payload["sourceAmount"], 153)
self.assertEqual(payload["categoryId"], settings.expense_category_id)
```

- [ ] **Step 2: Run tests and confirm RED**

Run: `python3 -m unittest discover -s integrations/email-bill-importer/tests -v`

Expected: import failures for the missing modules.

- [ ] **Step 3: Implement configuration, IMAP decoding, SQLite outbox, API client, and polling service**

Required environment variables are `MAIL_USER`, `MAIL_PASS`, `EBK_API_TOKEN`, `CMB_CREDIT_ACCOUNT_ID`, `CMB_DEBIT_ACCOUNT_ID`, `EXPENSE_CATEGORY_ID`, and `INCOME_CATEGORY_ID`. `IMAP_SERVER` is inferred for common providers when absent. The service fetches recent messages, records parsed work transactionally, checks an `ebk-mail:<id>` marker before POST, sends a stable `clientSessionId`, and only marks the local row complete after a confirmed remote transaction.

- [ ] **Step 4: Run all importer tests and confirm GREEN**

Run: `python3 -m unittest discover -s integrations/email-bill-importer/tests -v`

Expected: all importer tests pass.

- [ ] **Step 5: Commit**

```bash
git add integrations/email-bill-importer
git commit -m "feat: sync email bills into ezBookkeeping"
```

### Task 3: Add the minimal container deployment surface

**Files:**
- Create: `integrations/email-bill-importer/Dockerfile`
- Create: `integrations/email-bill-importer/.dockerignore`
- Create: `compose.yml`
- Create: `.env.example`
- Create: `docs/email-bill-importer.md`
- Modify: `README.md`

- [ ] **Step 1: Add a dependency-free importer image and two-service Compose file**

Use named volumes for `/ezbookkeeping/data`, `/ezbookkeeping/storage`, and `/data/importer.db`; use an ezBookkeeping health check before starting the importer; load secrets from `.env`; avoid optional reverse proxy/database services.

- [ ] **Step 2: Document first-run configuration**

Document API-token creation, account/category ID discovery with `skills/ezbookkeeping/scripts/ebktools.sh`, supported mail types, durable state, logs, and one-command startup:

```bash
cp .env.example .env
docker compose up -d
```

- [ ] **Step 3: Run static configuration checks**

Run: `python3 -m compileall -q integrations/email-bill-importer`

Run when Docker Compose is available: `docker compose --env-file .env.example config --quiet`

Expected: both commands exit 0.

- [ ] **Step 4: Commit**

```bash
git add compose.yml .env.example README.md docs/email-bill-importer.md integrations/email-bill-importer/Dockerfile integrations/email-bill-importer/.dockerignore
git commit -m "feat: add simple email importer compose stack"
```

### Task 4: Build and publish on every branch push

**Files:**
- Create: `.github/workflows/docker-publish.yml`
- Delete: `.github/workflows/build-snapshot.yml`
- Delete: `.github/workflows/build-non-main-branch.yml`

- [ ] **Step 1: Replace fork-unfriendly push workflows**

Use `permissions: contents: read, packages: write`, GitHub's automatic registry token, Buildx, one matrix entry per image, branch and SHA tags for every pushed branch, and `latest` only on the repository default branch. Keep the existing tagged release workflow separate.

- [ ] **Step 2: Validate workflow syntax and triggers**

Parse every workflow with a YAML parser when available and inspect that `docker-publish.yml` has a branch push trigger, two matrix images, GHCR login, and `push: true`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows
git commit -m "ci: publish Docker images on every push"
```

### Task 5: Final focused verification

**Files:**
- Verify only; no production files should change.

- [ ] **Step 1: Run the importer test suite**

Run: `python3 -m unittest discover -s integrations/email-bill-importer/tests -v`

Expected: all tests pass with no warnings.

- [ ] **Step 2: Run static checks**

Run: `python3 -m compileall -q integrations/email-bill-importer`

Run: `git diff --check main...HEAD`

Run when available: `docker compose --env-file .env.example config --quiet`

- [ ] **Step 3: Audit requirements and Git state**

Confirm the reference parser scope, durable state, API synchronization, Compose simplicity, every-push trigger, GHCR tags, atomic commits, and a clean worktree.
