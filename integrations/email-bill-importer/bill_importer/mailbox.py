from __future__ import annotations

import hashlib
import imaplib
import ssl
from dataclasses import dataclass
from datetime import datetime
from email import policy
from email.message import Message
from email.parser import BytesParser
from email.utils import parsedate_to_datetime
from html.parser import HTMLParser
from zoneinfo import ZoneInfo

from .config import Settings


@dataclass(frozen=True, slots=True)
class MailMessage:
    fingerprint: str
    sender: str
    subject: str
    received_at: datetime
    text: str


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


class ImapMailbox:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings

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
            status, search_data = connection.search(None, "ALL")
            if status != "OK":
                raise RuntimeError("cannot search IMAP inbox")
            message_ids = search_data[0].split()[-limit:]
            messages: list[MailMessage] = []
            for message_id in reversed(message_ids):
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
                messages.append(
                    MailMessage(
                        fingerprint=_fingerprint(parsed, received_at, text),
                        sender=str(parsed.get("From", "")),
                        subject=str(parsed.get("Subject", "")),
                        received_at=received_at,
                        text=" ".join(text.split()),
                    )
                )
            return messages
        finally:
            try:
                connection.logout()
            except imaplib.IMAP4.error:
                pass
