from __future__ import annotations

import sys
import unittest
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.models import money_to_minor
from bill_importer.parsers import CmbCreditCardParser, CmbDebitCardParser


SHANGHAI = ZoneInfo("Asia/Shanghai")


class MoneyConversionTestCase(unittest.TestCase):
    def test_rounds_decimal_amount_to_minor_units(self) -> None:
        self.assertEqual(money_to_minor("1.005"), 101)


class CmbCreditCardParserTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.parser = CmbCreditCardParser()
        self.received_at = datetime(2026, 9, 7, 9, 0, tzinfo=SHANGHAI)

    def test_matches_only_supported_sender_and_subject(self) -> None:
        self.assertTrue(
            self.parser.matches(
                "ccsvc@message.cmbchina.com", "招商银行每日信用管家"
            )
        )
        self.assertFalse(
            self.parser.matches("attacker@example.com", "招商银行每日信用管家")
        )
        self.assertFalse(
            self.parser.matches("ccsvc@message.cmbchina.com", "普通通知")
        )

    def test_parses_expense_and_cleans_card_prefix(self) -> None:
        text = """
        2026/09/06
        11:03:15
        CNY 1.53
        尾号5460 消费 麦当劳
        (每日邮件)
        """

        transactions = self.parser.parse(text, self.received_at)

        self.assertEqual(len(transactions), 1)
        self.assertEqual(transactions[0].amount_minor, -153)
        self.assertEqual(transactions[0].merchant, "麦当劳")
        self.assertEqual(
            transactions[0].occurred_at,
            datetime(2026, 9, 6, 11, 3, 15, tzinfo=SHANGHAI),
        )

    def test_turns_negative_card_amount_into_income(self) -> None:
        text = """
        2026/09/06
        12:10:00 CNY -5.00 尾号5460 退货 某商户
        (每日邮件)
        """

        transactions = self.parser.parse(text, self.received_at)

        self.assertEqual(transactions[0].amount_minor, 500)


class CmbDebitCardParserTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.parser = CmbDebitCardParser()
        self.received_at = datetime(2026, 9, 7, 9, 0, tzinfo=SHANGHAI)

    def test_parses_expense_notification(self) -> None:
        text = "您账户于09月07日11:52在支付宝-麦当劳快捷支付10.50元。"

        transactions = self.parser.parse(text, self.received_at)

        self.assertEqual(len(transactions), 1)
        self.assertEqual(transactions[0].amount_minor, -1050)
        self.assertEqual(transactions[0].merchant, "支付宝-麦当劳")
        self.assertEqual(transactions[0].occurred_at.hour, 11)
        self.assertEqual(transactions[0].occurred_at.minute, 52)

    def test_parses_income_notification(self) -> None:
        text = "您账户于09月07日08:30工资入账人民币1234.56元。"

        transactions = self.parser.parse(text, self.received_at)

        self.assertEqual(len(transactions), 1)
        self.assertEqual(transactions[0].amount_minor, 123456)
        self.assertEqual(transactions[0].merchant, "工资")

    def test_parses_funds_aggregation_with_postfixed_date(self) -> None:
        text = (
            "资金归集执行成功，已从其他账户向您尾号1234账户转账人民币500.00元，"
            "截至09月07日18:35。"
        )

        transactions = self.parser.parse(text, self.received_at)

        self.assertEqual(len(transactions), 1)
        self.assertEqual(transactions[0].amount_minor, 50000)
        self.assertEqual(transactions[0].occurred_at.hour, 18)
        self.assertIn("资金归集", transactions[0].merchant)


if __name__ == "__main__":
    unittest.main()
