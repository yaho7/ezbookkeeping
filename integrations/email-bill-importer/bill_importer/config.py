from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping


IMAP_SERVERS = {
    "qq.com": "imap.qq.com",
    "foxmail.com": "imap.qq.com",
    "163.com": "imap.163.com",
    "126.com": "imap.126.com",
    "gmail.com": "imap.gmail.com",
    "outlook.com": "outlook.office365.com",
    "hotmail.com": "outlook.office365.com",
}

AUTHENTICATION_SERVICE_DOMAINS = {
    "qq.com": ("qq.com",),
    "foxmail.com": ("qq.com",),
    "163.com": ("163.com",),
    "126.com": ("163.com",),
    "gmail.com": ("google.com",),
    "outlook.com": ("outlook.com",),
    "hotmail.com": ("outlook.com",),
}


def _required(env: Mapping[str, str], names: tuple[str, ...]) -> dict[str, str]:
    missing = [name for name in names if not env.get(name, "").strip()]
    if missing:
        raise ValueError(f"missing required environment variables: {', '.join(missing)}")
    return {name: env[name].strip() for name in names}


def _positive_int(env: Mapping[str, str], name: str, default: int) -> int:
    raw_value = env.get(name, str(default)).strip()
    try:
        value = int(raw_value)
    except ValueError as exc:
        raise ValueError(f"{name} must be an integer") from exc
    if value < 1:
        raise ValueError(f"{name} must be greater than zero")
    return value


def _boolean(env: Mapping[str, str], name: str, default: bool) -> bool:
    raw_value = env.get(name, "true" if default else "false").strip().lower()
    if raw_value in {"1", "true", "yes", "on"}:
        return True
    if raw_value in {"0", "false", "no", "off"}:
        return False
    raise ValueError(f"{name} must be true or false")


def _identifier(value: str, name: str) -> str:
    if not value.isdecimal() or int(value) < 1:
        raise ValueError(f"{name} must be a positive numeric ezBookkeeping id")
    return value


@dataclass(frozen=True, slots=True)
class Settings:
    mail_user: str
    mail_pass: str
    imap_server: str
    imap_port: int
    ebk_server_base_url: str
    ebk_api_token: str
    cmb_credit_account_id: str
    cmb_debit_account_id: str
    expense_category_id: str
    income_category_id: str
    timezone_name: str
    state_path: Path
    max_emails: int
    poll_interval_seconds: int
    run_once: bool
    require_authentication_results: bool
    trusted_authserv_domains: tuple[str, ...]

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "Settings":
        values = os.environ if env is None else env
        required = _required(
            values,
            (
                "MAIL_USER",
                "MAIL_PASS",
                "EBK_API_TOKEN",
                "CMB_CREDIT_ACCOUNT_ID",
                "CMB_DEBIT_ACCOUNT_ID",
                "EXPENSE_CATEGORY_ID",
                "INCOME_CATEGORY_ID",
            ),
        )
        imap_server = values.get("IMAP_SERVER", "").strip()
        if not imap_server:
            domain = required["MAIL_USER"].rsplit("@", 1)[-1].lower()
            imap_server = IMAP_SERVERS.get(domain, "")
        if not imap_server:
            raise ValueError(
                "IMAP_SERVER is required because the mail provider cannot be inferred"
            )
        mail_domain = required["MAIL_USER"].rsplit("@", 1)[-1].lower()
        configured_authserv_domains = values.get(
            "TRUSTED_AUTHSERV_DOMAINS", ""
        ).strip()
        trusted_authserv_domains = tuple(
            domain.strip().lower().strip(".")
            for domain in configured_authserv_domains.split(",")
            if domain.strip(" .")
        ) or AUTHENTICATION_SERVICE_DOMAINS.get(mail_domain, (imap_server.lower(),))

        return cls(
            mail_user=required["MAIL_USER"],
            mail_pass=required["MAIL_PASS"],
            imap_server=imap_server,
            imap_port=_positive_int(values, "IMAP_PORT", 993),
            ebk_server_base_url=values.get(
                "EBK_SERVER_BASE_URL", "http://ezbookkeeping:8080"
            ).rstrip("/"),
            ebk_api_token=required["EBK_API_TOKEN"],
            cmb_credit_account_id=_identifier(
                required["CMB_CREDIT_ACCOUNT_ID"], "CMB_CREDIT_ACCOUNT_ID"
            ),
            cmb_debit_account_id=_identifier(
                required["CMB_DEBIT_ACCOUNT_ID"], "CMB_DEBIT_ACCOUNT_ID"
            ),
            expense_category_id=_identifier(
                required["EXPENSE_CATEGORY_ID"], "EXPENSE_CATEGORY_ID"
            ),
            income_category_id=_identifier(
                required["INCOME_CATEGORY_ID"], "INCOME_CATEGORY_ID"
            ),
            timezone_name=values.get("TZ", "Asia/Shanghai").strip(),
            state_path=Path(values.get("STATE_PATH", "/data/importer.db")),
            max_emails=_positive_int(values, "MAX_EMAILS", 60),
            poll_interval_seconds=_positive_int(
                values, "POLL_INTERVAL_SECONDS", 3600
            ),
            run_once=_boolean(values, "RUN_ONCE", False),
            require_authentication_results=_boolean(
                values, "REQUIRE_AUTHENTICATION_RESULTS", True
            ),
            trusted_authserv_domains=trusted_authserv_domains,
        )

    def account_id_for(self, source: str) -> str:
        account_ids = {
            "cmb_credit": self.cmb_credit_account_id,
            "cmb_debit": self.cmb_debit_account_id,
        }
        try:
            return account_ids[source]
        except KeyError as exc:
            raise ValueError(f"unsupported transaction source: {source}") from exc

    def category_id_for(self, amount_minor: int) -> str:
        return self.income_category_id if amount_minor > 0 else self.expense_category_id
