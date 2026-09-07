from __future__ import annotations

import sys
import unittest
from email.message import EmailMessage
from pathlib import Path
from unittest.mock import patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.config import Settings
from bill_importer.mailbox import ImapMailbox, _has_bank_authentication_result
from bill_importer.parsers import CmbCreditCardParser


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


class FakeImapConnection:
    def __init__(self, *_args, **_kwargs) -> None:
        message = EmailMessage()
        message["From"] = "ccsvc@message.cmbchina.com"
        message["Subject"] = "招商银行每日信用管家"
        message["Date"] = "Mon, 07 Sep 2026 09:00:00 +0800"
        message["Message-ID"] = "<bill@example.com>"
        message["Authentication-Results"] = (
            "mx.qq.com; dkim=pass header.d=message.cmbchina.com"
        )
        message.set_content(
            "2026/09/06 11:03:15 CNY 1.53 尾号5460 消费 麦当劳 (每日邮件)"
        )
        self.raw_message = message.as_bytes()
        self.search_calls: list[tuple] = []

    def login(self, *_args) -> None:
        return None

    def select(self, *_args, **_kwargs):
        return "OK", [b""]

    def search(self, *args):
        self.search_calls.append(args)
        return "OK", [b"41"]

    def fetch(self, *_args):
        return "OK", [(b"41 (RFC822)", self.raw_message)]

    def logout(self) -> None:
        return None


class ImapMailboxTestCase(unittest.TestCase):
    def test_searches_supported_sender_and_subject_instead_of_entire_inbox(self) -> None:
        fake = FakeImapConnection()
        with patch("bill_importer.mailbox.imaplib.IMAP4_SSL", return_value=fake):
            mailbox = ImapMailbox(settings(), parsers=(CmbCreditCardParser(),))

            messages = mailbox.fetch_recent(60)

        self.assertEqual(len(messages), 1)
        flattened = repr(fake.search_calls)
        self.assertIn("ccsvc@message.cmbchina.com", flattened)
        self.assertIn("每日信用管家".encode("utf-8"), fake.search_calls[0])
        self.assertNotIn("ALL", flattened)
        self.assertTrue(messages[0].authenticated)

    def test_rejects_untrusted_authserv_id_and_bank_domain_suffix_attack(self) -> None:
        untrusted = EmailMessage()
        untrusted["Authentication-Results"] = (
            "attacker.example; dkim=pass header.d=message.cmbchina.com"
        )
        suffix_attack = EmailMessage()
        suffix_attack["Authentication-Results"] = (
            "mx.qq.com; dkim=pass header.d=message.cmbchina.com.evil; "
            "spf=pass smtp.mailfrom=notice@message.cmbchina.com.evil"
        )

        self.assertFalse(_has_bank_authentication_result(untrusted, ("qq.com",)))
        self.assertFalse(
            _has_bank_authentication_result(suffix_attack, ("qq.com",))
        )


class FallbackImapConnection(FakeImapConnection):
    def __init__(self, *_args, **_kwargs) -> None:
        super().__init__()
        unrelated = EmailMessage()
        unrelated["From"] = "ccsvc@message.cmbchina.com"
        unrelated["Subject"] = "招商银行其他通知"
        unrelated["Date"] = "Mon, 07 Sep 2026 10:00:00 +0800"
        unrelated["Message-ID"] = "<unrelated@example.com>"
        unrelated.set_content("not a bill")
        self.messages = {b"40": self.raw_message, b"41": unrelated.as_bytes()}

    def search(self, *args):
        self.search_calls.append(args)
        if args[0] == "UTF-8":
            raise UnicodeEncodeError("ascii", "主题", 0, 1, "unsupported")
        return "OK", [b"40 41"]

    def fetch(self, message_id, *_args):
        return "OK", [(message_id + b" (RFC822)", self.messages[message_id])]


class ImapFallbackTestCase(unittest.TestCase):
    def test_filters_subject_before_applying_limit_in_sender_only_fallback(self) -> None:
        fake = FallbackImapConnection()
        with patch("bill_importer.mailbox.imaplib.IMAP4_SSL", return_value=fake):
            mailbox = ImapMailbox(settings(), parsers=(CmbCreditCardParser(),))

            messages = mailbox.fetch_recent(1)

        self.assertEqual(
            [message.subject for message in messages],
            ["招商银行每日信用管家"],
        )


if __name__ == "__main__":
    unittest.main()
