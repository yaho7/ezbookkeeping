from __future__ import annotations

import hashlib
import imaplib
import re
import ssl
from dataclasses import dataclass
from datetime import datetime
from email import policy
from email.message import Message
from email.parser import BytesParser
from email.utils import parsedate_to_datetime
from html.parser import HTMLParser
from typing import Sequence
from zoneinfo import ZoneInfo

from .config import Settings
from .parsers import BillParser


@dataclass(frozen=True, slots=True)
class MailMessage:
    fingerprint: str
    sender: str
    subject: str
    received_at: datetime
    text: str
    authenticated: bool


class _TextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.parts: list[str] = []

    def handle_data(self, data: str) -> None:
        self.parts.append(data)

    def text(self) -> str:
        return "\n".join(part.strip() for part in self.parts if part.strip())


def _message_text(message: Message) -> str:
    plain_parts: list[str] = []
    html_parts: list[str] = []
    parts = message.walk() if message.is_multipart() else (message,)

    for part in parts:
        if part.get_content_disposition() == "attachment":
            continue
        content_type = part.get_content_type()
        if content_type not in {"text/plain", "text/html"}:
            continue
        try:
            content = part.get_content()
        except (LookupError, UnicodeError):
            payload = part.get_payload(decode=True) or b""
            content = payload.decode(part.get_content_charset() or "utf-8", errors="replace")
        if content_type == "text/plain":
            plain_parts.append(str(content))
        else:
            html_parts.append(str(content))

    if plain_parts:
        return "\n".join(plain_parts)
    extractor = _TextExtractor()
    extractor.feed("\n".join(html_parts))
    return extractor.text()


def _fingerprint(message: Message, received_at: datetime, text: str) -> str:
    message_id = str(message.get("Message-ID", "")).strip()
    identity = message_id or "\n".join(
        (received_at.isoformat(), str(message.get("Subject", "")), text)
    )
    return hashlib.sha256(identity.encode("utf-8", errors="replace")).hexdigest()


def _has_bank_authentication_result(
    message: Message, trusted_authserv_domains: Sequence[str]
) -> bool:
    authentication_results = message.get_all("Authentication-Results", [])
    if not authentication_results:
        return False
    result = " ".join(str(authentication_results[0]).lower().split())
    authserv_tokens = result.partition(";")[0].strip().split()
    if not authserv_tokens:
        return False
    authserv_id = authserv_tokens[0].rstrip(".")
    if not any(
        authserv_id == domain or authserv_id.endswith(f".{domain}")
        for domain in trusted_authserv_domains
    ):
        return False
    bank_domain = r"(?:message\.)?cmbchina\.com(?=[\s;]|$)"
    dkim_passed = re.search(
        rf"\bdkim=pass\b.*?\bheader\.(?:d|i)=(?:@)?{bank_domain}",
        result,
    )
    spf_passed = re.search(
        rf"\bspf=pass\b.*?\bsmtp\.mailfrom=(?:[^@\s;]+@)?{bank_domain}",
        result,
    )
    return dkim_passed is not None or spf_passed is not None


class ImapMailbox:
    def __init__(self, settings: Settings, parsers: Sequence[BillParser]) -> None:
        self.settings = settings
        self.parsers = parsers

    def _search_message_ids(
        self, connection: imaplib.IMAP4_SSL, limit: int
    ) -> list[bytes]:
        message_ids: set[bytes] = set()
        for parser in self.parsers:
            for sender in parser.allowed_senders:
                for subject_keyword in parser.subject_keywords:
                    try:
                        status, search_data = connection.search(
                            "UTF-8",
                            "FROM",
                            sender,
                            "SUBJECT",
                            subject_keyword.encode("utf-8"),
                        )
                    except (imaplib.IMAP4.error, UnicodeEncodeError):
                        status = "NO"
                        search_data = []
                    if status != "OK":
                        status, search_data = connection.search(None, "FROM", sender)
                    if status != "OK":
                        raise RuntimeError("cannot search IMAP inbox")
                    message_ids.update(search_data[0].split())
        return sorted(message_ids, key=lambda value: int(value), reverse=True)

    def fetch_recent(self, limit: int) -> list[MailMessage]:
        timezone = ZoneInfo(self.settings.timezone_name)
        context = ssl.create_default_context()
        connection = imaplib.IMAP4_SSL(
            self.settings.imap_server,
            self.settings.imap_port,
            ssl_context=context,
            timeout=30,
        )
        try:
            connection.login(self.settings.mail_user, self.settings.mail_pass)
            status, _ = connection.select("INBOX", readonly=True)
            if status != "OK":
                raise RuntimeError("cannot select IMAP inbox")
            message_ids = self._search_message_ids(connection, limit)
            messages: list[MailMessage] = []
            for message_id in message_ids:
                status, fetched = connection.fetch(message_id, "(RFC822)")
                if status != "OK" or not fetched or not isinstance(fetched[0], tuple):
                    continue
                parsed = BytesParser(policy=policy.default).parsebytes(fetched[0][1])
                date_header = str(parsed.get("Date", ""))
                received_at = (
                    parsedate_to_datetime(date_header)
                    if date_header
                    else datetime.now(timezone)
                )
                if received_at.tzinfo is None:
                    received_at = received_at.replace(tzinfo=timezone)
                received_at = received_at.astimezone(timezone)
                text = _message_text(parsed)
                sender = str(parsed.get("From", ""))
                subject = str(parsed.get("Subject", ""))
                if not any(
                    parser.matches(sender, subject) for parser in self.parsers
                ):
                    continue
                messages.append(
                    MailMessage(
                        fingerprint=_fingerprint(parsed, received_at, text),
                        sender=sender,
                        subject=subject,
                        received_at=received_at,
                        text=" ".join(text.split()),
                        authenticated=_has_bank_authentication_result(
                            parsed, self.settings.trusted_authserv_domains
                        ),
                    )
                )
                if len(messages) >= limit:
                    break
            return messages
        finally:
            try:
                connection.logout()
            except (imaplib.IMAP4.error, OSError):
                pass
