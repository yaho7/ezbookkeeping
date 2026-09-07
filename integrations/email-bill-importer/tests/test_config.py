from __future__ import annotations

import sys
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from bill_importer.config import Settings


REQUIRED_ENV = {
    "MAIL_USER": "user@qq.com",
    "MAIL_PASS": "mail-secret",
    "EBK_API_TOKEN": "api-secret",
    "CMB_CREDIT_ACCOUNT_ID": "101",
    "CMB_DEBIT_ACCOUNT_ID": "102",
    "EXPENSE_CATEGORY_ID": "201",
    "INCOME_CATEGORY_ID": "202",
}


class SettingsTestCase(unittest.TestCase):
    def test_infers_imap_server_and_applies_defaults(self) -> None:
        settings = Settings.from_env(REQUIRED_ENV)

        self.assertEqual(settings.imap_server, "imap.qq.com")
        self.assertEqual(settings.ebk_server_base_url, "http://ezbookkeeping:8080")
        self.assertEqual(settings.timezone_name, "Asia/Shanghai")
        self.assertTrue(settings.require_authentication_results)
        self.assertEqual(settings.account_id_for("cmb_credit"), "101")
        self.assertEqual(settings.category_id_for(-1), "201")
        self.assertEqual(settings.category_id_for(1), "202")

    def test_reports_all_missing_required_values(self) -> None:
        with self.assertRaisesRegex(
            ValueError, "MAIL_PASS.*EBK_API_TOKEN.*CMB_CREDIT_ACCOUNT_ID"
        ):
            Settings.from_env({"MAIL_USER": "user@qq.com"})

    def test_rejects_unknown_mail_provider_without_explicit_server(self) -> None:
        values = dict(REQUIRED_ENV, MAIL_USER="user@private.example")

        with self.assertRaisesRegex(ValueError, "IMAP_SERVER"):
            Settings.from_env(values)

    def test_rejects_non_numeric_account_or_category_ids(self) -> None:
        values = dict(REQUIRED_ENV, CMB_CREDIT_ACCOUNT_ID="credit-card")

        with self.assertRaisesRegex(ValueError, "CMB_CREDIT_ACCOUNT_ID"):
            Settings.from_env(values)


if __name__ == "__main__":
    unittest.main()
