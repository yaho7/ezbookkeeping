from __future__ import annotations

import sys
import tempfile
import unittest
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.models import BankTransaction
from bill_importer.state import Outbox


class OutboxTestCase(unittest.TestCase):
    def test_pending_transaction_survives_reopen_and_completed_item_does_not(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "importer.db"
            transaction = BankTransaction(
                source="cmb_debit",
                occurred_at=datetime(
                    2026, 9, 7, 8, 30, tzinfo=ZoneInfo("Asia/Shanghai")
                ),
                amount_minor=123456,
                merchant="工资",
                description="工资 - 入账",
            )
            with Outbox(path) as outbox:
                outbox.record_message("message-key", "工资到账", [transaction])

            with Outbox(path) as reopened:
                pending = reopened.pending_transactions()
                self.assertEqual(len(pending), 1)
                self.assertEqual(pending[0].transaction, transaction)
                reopened.mark_completed(pending[0].idempotency_key, "9001")
                self.assertEqual(reopened.pending_transactions(), [])
                self.assertTrue(reopened.has_message("message-key"))

    def test_recording_same_message_twice_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            transaction = BankTransaction(
                source="cmb_credit",
                occurred_at=datetime(
                    2026, 9, 7, 9, 0, tzinfo=ZoneInfo("Asia/Shanghai")
                ),
                amount_minor=-100,
                merchant="商户",
                description="消费",
            )
            with Outbox(Path(directory) / "importer.db") as outbox:
                outbox.record_message("same", "通知", [transaction])
                outbox.record_message("same", "通知", [transaction])

                self.assertEqual(len(outbox.pending_transactions()), 1)

    def test_pending_transaction_can_only_be_claimed_by_one_worker(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "importer.db"
            transaction = BankTransaction(
                source="cmb_credit",
                occurred_at=datetime(
                    2026, 9, 7, 9, 0, tzinfo=ZoneInfo("Asia/Shanghai")
                ),
                amount_minor=-100,
                merchant="商户",
                description="消费",
            )
            with Outbox(path, worker_id="worker-one") as first, Outbox(
                path, worker_id="worker-two"
            ) as second:
                first.record_message("message", "通知", [transaction])

                first_claim = first.claim_next(now=1000, lease_seconds=300)
                second_claim = second.claim_next(now=1000, lease_seconds=300)

                self.assertIsNotNone(first_claim)
                self.assertIsNone(second_claim)


if __name__ == "__main__":
    unittest.main()
