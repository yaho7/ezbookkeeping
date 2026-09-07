from __future__ import annotations

import sys
import tempfile
import unittest
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.mailbox import MailMessage
from bill_importer.parsers import CmbCreditCardParser
from bill_importer.service import EmailBillService
from bill_importer.state import Outbox


class FakeMailbox:
    def __init__(self, messages: list[MailMessage]) -> None:
        self.messages = messages

    def fetch_recent(self, _limit: int) -> list[MailMessage]:
        return self.messages


class FakeApiClient:
    def __init__(self, existing_id: str | None = None) -> None:
        self.existing_id = existing_id
        self.added: list[tuple] = []

    def find_transaction(self, _marker: str, _occurred_at: datetime) -> str | None:
        return self.existing_id

    def add_transaction(self, transaction, marker: str, session_id: str) -> str:
        self.added.append((transaction, marker, session_id))
        return "new-9002"


def mail_message() -> MailMessage:
    return MailMessage(
        fingerprint="mail-fingerprint",
        sender="ccsvc@message.cmbchina.com",
        subject="招商银行每日信用管家",
        received_at=datetime(2026, 9, 7, 9, 0, tzinfo=ZoneInfo("Asia/Shanghai")),
        text="2026/09/06 11:03:15 CNY 1.53 尾号5460 消费 麦当劳 (每日邮件)",
    )


class EmailBillServiceTestCase(unittest.TestCase):
    def test_remote_marker_closes_outbox_without_posting_again(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with Outbox(Path(directory) / "importer.db") as outbox:
                api = FakeApiClient(existing_id="existing-9001")
                service = EmailBillService(
                    mailbox=FakeMailbox([mail_message()]),
                    outbox=outbox,
                    api_client=api,
                    parsers=(CmbCreditCardParser(),),
                    max_emails=50,
                )

                summary = service.run_once()

                self.assertEqual(summary.imported, 0)
                self.assertEqual(summary.recovered, 1)
                self.assertEqual(api.added, [])
                self.assertEqual(outbox.pending_transactions(), [])

    def test_posts_new_transaction_once_across_repeated_runs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with Outbox(Path(directory) / "importer.db") as outbox:
                api = FakeApiClient()
                service = EmailBillService(
                    mailbox=FakeMailbox([mail_message()]),
                    outbox=outbox,
                    api_client=api,
                    parsers=(CmbCreditCardParser(),),
                    max_emails=50,
                )

                first = service.run_once()
                second = service.run_once()

                self.assertEqual(first.imported, 1)
                self.assertEqual(second.imported, 0)
                self.assertEqual(len(api.added), 1)


if __name__ == "__main__":
    unittest.main()
