from __future__ import annotations

import hashlib
import json
import sqlite3
import time
import uuid
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path

from .models import BankTransaction


@dataclass(frozen=True, slots=True)
class PendingTransaction:
    idempotency_key: str
    marker: str
    transaction: BankTransaction


def _serialize(transaction: BankTransaction) -> str:
    data = asdict(transaction)
    data["occurred_at"] = transaction.occurred_at.isoformat()
    return json.dumps(data, ensure_ascii=False, sort_keys=True)


def _deserialize(value: str) -> BankTransaction:
    data = json.loads(value)
    data["occurred_at"] = datetime.fromisoformat(data["occurred_at"])
    return BankTransaction(**data)


class Outbox:
    def __init__(self, path: Path, worker_id: str | None = None) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        self.worker_id = worker_id or uuid.uuid4().hex
        self.connection = sqlite3.connect(path)
        self.connection.row_factory = sqlite3.Row
        self.connection.execute("PRAGMA foreign_keys = ON")
        self.connection.execute("PRAGMA busy_timeout = 5000")
        self.connection.execute("PRAGMA journal_mode = WAL")
        self.connection.executescript(
            """
            CREATE TABLE IF NOT EXISTS messages (
                message_key TEXT PRIMARY KEY,
                subject TEXT NOT NULL,
                status TEXT NOT NULL,
                last_error TEXT NOT NULL DEFAULT '',
                updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
            );
            CREATE TABLE IF NOT EXISTS transactions (
                idempotency_key TEXT PRIMARY KEY,
                message_key TEXT NOT NULL REFERENCES messages(message_key),
                marker TEXT NOT NULL UNIQUE,
                transaction_json TEXT NOT NULL,
                status TEXT NOT NULL,
                remote_id TEXT NOT NULL DEFAULT '',
                last_error TEXT NOT NULL DEFAULT '',
                lease_owner TEXT NOT NULL DEFAULT '',
                lease_until INTEGER NOT NULL DEFAULT 0,
                updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
            );
            """
        )
        self._ensure_column(
            "transactions", "lease_owner", "TEXT NOT NULL DEFAULT ''"
        )
        self._ensure_column(
            "transactions", "lease_until", "INTEGER NOT NULL DEFAULT 0"
        )

    def _ensure_column(self, table: str, column: str, definition: str) -> None:
        columns = {
            row["name"]
            for row in self.connection.execute(f"PRAGMA table_info({table})").fetchall()
        }
        if column not in columns:
            self.connection.execute(
                f"ALTER TABLE {table} ADD COLUMN {column} {definition}"
            )

    def __enter__(self) -> "Outbox":
        return self

    def __exit__(self, *_args) -> None:
        self.close()

    def close(self) -> None:
        self.connection.close()

    def has_message(self, message_key: str) -> bool:
        row = self.connection.execute(
            "SELECT status FROM messages WHERE message_key = ?", (message_key,)
        ).fetchone()
        return row is not None and row["status"] != "parse_failed"

    def record_message(
        self,
        message_key: str,
        subject: str,
        transactions: list[BankTransaction],
    ) -> int:
        with self.connection:
            cursor = self.connection.execute(
                "INSERT OR IGNORE INTO messages(message_key, subject, status) "
                "VALUES (?, ?, ?)",
                (message_key, subject, "pending" if transactions else "ignored"),
            )
            if cursor.rowcount == 0:
                cursor = self.connection.execute(
                    "UPDATE messages SET subject = ?, status = ?, last_error = '', "
                    "updated_at = CURRENT_TIMESTAMP "
                    "WHERE message_key = ? AND status = 'parse_failed'",
                    (
                        subject,
                        "pending" if transactions else "ignored",
                        message_key,
                    ),
                )
                if cursor.rowcount == 0:
                    return 0
            for index, transaction in enumerate(transactions):
                serialized = _serialize(transaction)
                digest = hashlib.sha256(
                    f"{message_key}:{index}:{serialized}".encode("utf-8")
                ).hexdigest()
                self.connection.execute(
                    "INSERT INTO transactions("
                    "idempotency_key, message_key, marker, transaction_json, status"
                    ") VALUES (?, ?, ?, ?, 'pending')",
                    (digest, message_key, f"ebk-mail:{digest[:20]}", serialized),
                )
        return len(transactions)

    def record_message_failure(
        self, message_key: str, subject: str, error: str
    ) -> None:
        with self.connection:
            self.connection.execute(
                "INSERT INTO messages("
                "message_key, subject, status, last_error"
                ") VALUES (?, ?, 'parse_failed', ?) "
                "ON CONFLICT(message_key) DO UPDATE SET "
                "subject = excluded.subject, status = 'parse_failed', "
                "last_error = excluded.last_error, updated_at = CURRENT_TIMESTAMP",
                (message_key, subject, error[:2000]),
            )

    def pending_transactions(self) -> list[PendingTransaction]:
        rows = self.connection.execute(
            "SELECT idempotency_key, marker, transaction_json FROM transactions "
            "WHERE status IN ('pending', 'failed') ORDER BY rowid"
        ).fetchall()
        return [
            PendingTransaction(
                idempotency_key=row["idempotency_key"],
                marker=row["marker"],
                transaction=_deserialize(row["transaction_json"]),
            )
            for row in rows
        ]

    def claim_next(
        self, now: int | None = None, lease_seconds: int = 300
    ) -> PendingTransaction | None:
        claimed_at = int(time.time()) if now is None else now
        self.connection.execute("BEGIN IMMEDIATE")
        try:
            row = self.connection.execute(
                "SELECT idempotency_key, marker, transaction_json "
                "FROM transactions WHERE status IN ('pending', 'failed') "
                "OR (status = 'processing' AND lease_until <= ?) "
                "ORDER BY rowid LIMIT 1",
                (claimed_at,),
            ).fetchone()
            if row is None:
                self.connection.commit()
                return None
            self.connection.execute(
                "UPDATE transactions SET status = 'processing', lease_owner = ?, "
                "lease_until = ?, updated_at = CURRENT_TIMESTAMP "
                "WHERE idempotency_key = ?",
                (
                    self.worker_id,
                    claimed_at + lease_seconds,
                    row["idempotency_key"],
                ),
            )
            self.connection.commit()
        except Exception:
            self.connection.rollback()
            raise
        return PendingTransaction(
            idempotency_key=row["idempotency_key"],
            marker=row["marker"],
            transaction=_deserialize(row["transaction_json"]),
        )

    def mark_completed(self, idempotency_key: str, remote_id: str) -> None:
        with self.connection:
            row = self.connection.execute(
                "SELECT message_key FROM transactions WHERE idempotency_key = ?",
                (idempotency_key,),
            ).fetchone()
            if row is None:
                raise KeyError(idempotency_key)
            self.connection.execute(
                "UPDATE transactions SET status = 'completed', remote_id = ?, "
                "last_error = '', lease_owner = '', lease_until = 0, "
                "updated_at = CURRENT_TIMESTAMP "
                "WHERE idempotency_key = ?",
                (remote_id, idempotency_key),
            )
            self.connection.execute(
                "UPDATE messages SET status = 'completed', last_error = '', "
                "updated_at = CURRENT_TIMESTAMP WHERE message_key = ? AND NOT EXISTS ("
                "SELECT 1 FROM transactions WHERE message_key = ? "
                "AND status != 'completed')",
                (row["message_key"], row["message_key"]),
            )

    def mark_failed(self, idempotency_key: str, error: str) -> None:
        with self.connection:
            self.connection.execute(
                "UPDATE transactions SET status = 'failed', last_error = ?, "
                "lease_owner = '', lease_until = 0, updated_at = CURRENT_TIMESTAMP "
                "WHERE idempotency_key = ?",
                (error[:2000], idempotency_key),
            )
