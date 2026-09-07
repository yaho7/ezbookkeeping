from __future__ import annotations

import hashlib
import logging
from dataclasses import dataclass
from typing import Protocol, Sequence

from .ezbookkeeping import EzBookkeepingClient
from .mailbox import MailMessage
from .parsers import BillParser
from .state import Outbox


LOGGER = logging.getLogger(__name__)
PARSER_SCHEMA_VERSION = "1"


class Mailbox(Protocol):
    def fetch_recent(self, limit: int) -> list[MailMessage]: ...


@dataclass(frozen=True, slots=True)
class RunSummary:
    fetched: int = 0
    matched: int = 0
    queued: int = 0
    imported: int = 0
    recovered: int = 0
    failed: int = 0


class EmailBillService:
    def __init__(
        self,
        mailbox: Mailbox,
        outbox: Outbox,
        api_client: EzBookkeepingClient,
        parsers: Sequence[BillParser],
        max_emails: int,
    ) -> None:
        self.mailbox = mailbox
        self.outbox = outbox
        self.api_client = api_client
        self.parsers = parsers
        self.max_emails = max_emails

    def run_once(self) -> RunSummary:
        messages = self.mailbox.fetch_recent(self.max_emails)
        matched = queued = imported = recovered = failed = 0

        for message in messages:
            parser = next(
                (
                    candidate
                    for candidate in self.parsers
                    if candidate.matches(message.sender, message.subject)
                ),
                None,
            )
            if parser is None:
                continue
            matched += 1
            message_key = hashlib.sha256(
                f"{PARSER_SCHEMA_VERSION}:{message.fingerprint}".encode("utf-8")
            ).hexdigest()
            if self.outbox.has_message(message_key):
                continue
            try:
                transactions = parser.parse(message.text, message.received_at)
                queued += self.outbox.record_message(
                    message_key, message.subject, transactions
                )
            except Exception as exc:
                failed += 1
                self.outbox.record_message_failure(
                    message_key, message.subject, str(exc)
                )
                LOGGER.exception("failed to parse mail %s", message.fingerprint[:12])

        for item in self.outbox.pending_transactions():
            try:
                remote_id = self.api_client.find_transaction(
                    item.marker, item.transaction.occurred_at
                )
                if remote_id:
                    recovered += 1
                else:
                    remote_id = self.api_client.add_transaction(
                        item.transaction, item.marker, item.idempotency_key
                    )
                    imported += 1
                self.outbox.mark_completed(item.idempotency_key, remote_id)
            except Exception as exc:
                failed += 1
                self.outbox.mark_failed(item.idempotency_key, str(exc))
                LOGGER.exception("failed to synchronize %s", item.marker)

        return RunSummary(
            fetched=len(messages),
            matched=matched,
            queued=queued,
            imported=imported,
            recovered=recovered,
            failed=failed,
        )
