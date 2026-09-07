from __future__ import annotations

import sys
import unittest
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.config import Settings
from bill_importer.ezbookkeeping import EzBookkeepingClient
from bill_importer.models import BankTransaction


def settings() -> Settings:
    return Settings.from_env(
        {
            "MAIL_USER": "user@qq.com",
            "MAIL_PASS": "mail-secret",
            "EBK_API_TOKEN": "api-secret",
            "CMB_CREDIT_ACCOUNT_ID": "101",
            "CMB_DEBIT_ACCOUNT_ID": "102",
            "EXPENSE_CATEGORY_ID": "201",
            "INCOME_CATEGORY_ID": "202",
        }
    )


class FakeTransport:
    def __init__(self, responses: list[dict]) -> None:
        self.responses = list(responses)
        self.calls: list[tuple[str, str, dict | None, dict | None]] = []

    def request(
        self,
        method: str,
        path: str,
        payload: dict | None = None,
        query: dict | None = None,
    ) -> dict:
        self.calls.append((method, path, payload, query))
        return self.responses.pop(0)


class EzBookkeepingClientTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.settings = settings()
        self.transaction = BankTransaction(
            source="cmb_credit",
            occurred_at=datetime(
                2026, 9, 7, 11, 3, 15, tzinfo=ZoneInfo("Asia/Shanghai")
            ),
            amount_minor=-153,
            merchant="麦当劳",
            description="尾号5460 消费 麦当劳",
        )

    def test_builds_expense_payload_with_stable_marker(self) -> None:
        client = EzBookkeepingClient(self.settings, FakeTransport([]))

        payload = client.build_payload(
            self.transaction, "ebk-mail:abc123", "session-abc123"
        )

        self.assertEqual(payload["type"], 3)
        self.assertEqual(payload["sourceAmount"], 153)
        self.assertEqual(payload["sourceAccountId"], "101")
        self.assertEqual(payload["categoryId"], "201")
        self.assertEqual(payload["clientSessionId"], "session-abc123")
        self.assertIn("ebk-mail:abc123", payload["comment"])

    def test_finds_existing_transaction_by_marker(self) -> None:
        transport = FakeTransport(
            [{"success": True, "result": [{"id": "9001", "comment": "ebk-mail:x"}]}]
        )
        client = EzBookkeepingClient(self.settings, transport)

        remote_id = client.find_transaction("ebk-mail:x", self.transaction.occurred_at)

        self.assertEqual(remote_id, "9001")
        self.assertEqual(transport.calls[0][0:2], ("GET", "transactions/list/all.json"))

    def test_adds_transaction_and_returns_remote_id(self) -> None:
        transport = FakeTransport(
            [{"success": True, "result": {"id": "9002"}}]
        )
        client = EzBookkeepingClient(self.settings, transport)

        remote_id = client.add_transaction(
            self.transaction, "ebk-mail:y", "session-y"
        )

        self.assertEqual(remote_id, "9002")
        self.assertEqual(transport.calls[0][0:2], ("POST", "transactions/add.json"))


if __name__ == "__main__":
    unittest.main()
