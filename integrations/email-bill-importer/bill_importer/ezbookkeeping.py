from __future__ import annotations

import json
from datetime import datetime, timedelta
from typing import Protocol
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

from .config import Settings
from .models import BankTransaction


class ApiError(RuntimeError):
    pass


class JsonTransport(Protocol):
    def request(
        self,
        method: str,
        path: str,
        payload: dict | None = None,
        query: dict | None = None,
    ) -> dict: ...


class UrllibJsonTransport:
    def __init__(self, settings: Settings) -> None:
        self.base_url = f"{settings.ebk_server_base_url}/api/v1"
        self.headers = {
            "Authorization": f"Bearer {settings.ebk_api_token}",
            "Content-Type": "application/json",
            "X-Timezone-Name": settings.timezone_name,
        }

    def request(
        self,
        method: str,
        path: str,
        payload: dict | None = None,
        query: dict | None = None,
    ) -> dict:
        url = f"{self.base_url}/{path}"
        if query:
            url = f"{url}?{urlencode(query)}"
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        request = Request(url, data=body, headers=self.headers, method=method)
        try:
            with urlopen(request, timeout=30) as response:
                return json.loads(response.read().decode("utf-8"))
        except HTTPError as exc:
            details = exc.read().decode("utf-8", errors="replace")
            raise ApiError(f"ezBookkeeping HTTP {exc.code}: {details}") from exc
        except (URLError, TimeoutError) as exc:
            raise ApiError(f"cannot reach ezBookkeeping: {exc}") from exc


def _result(response: dict):
    if not response.get("success"):
        error = response.get("error") or response.get("message") or "unknown API error"
        raise ApiError(f"ezBookkeeping API rejected request: {error}")
    return response.get("result")


def _comment_with_marker(transaction: BankTransaction, marker: str) -> str:
    prefix = " | ".join(
        part for part in (transaction.merchant, transaction.description) if part
    )
    suffix = f" [{marker}]"
    return f"{prefix[: 255 - len(suffix)]}{suffix}"


class EzBookkeepingClient:
    def __init__(
        self, settings: Settings, transport: JsonTransport | None = None
    ) -> None:
        self.settings = settings
        self.transport = transport or UrllibJsonTransport(settings)

    def build_payload(
        self, transaction: BankTransaction, marker: str, session_id: str
    ) -> dict:
        offset = transaction.occurred_at.utcoffset()
        if offset is None:
            raise ValueError("transaction time must have a UTC offset")
        return {
            "type": 2 if transaction.amount_minor > 0 else 3,
            "categoryId": self.settings.category_id_for(transaction.amount_minor),
            "time": int(transaction.occurred_at.timestamp()),
            "utcOffset": int(offset.total_seconds() // 60),
            "sourceAccountId": self.settings.account_id_for(transaction.source),
            "sourceAmount": abs(transaction.amount_minor),
            "destinationAccountId": "0",
            "destinationAmount": 0,
            "hideAmount": False,
            "tagIds": [],
            "pictureIds": [],
            "comment": _comment_with_marker(transaction, marker),
            "clientSessionId": session_id,
        }

    def find_transaction(
        self, marker: str, occurred_at: datetime
    ) -> str | None:
        response = self.transport.request(
            "GET",
            "transactions/list/all.json",
            query={
                "keyword": marker,
                "start_time": int((occurred_at - timedelta(days=1)).timestamp()),
                "end_time": int((occurred_at + timedelta(days=1)).timestamp()),
                "trim_account": "true",
                "trim_category": "true",
                "trim_tag": "true",
            },
        )
        transactions = _result(response) or []
        for transaction in transactions:
            if marker in transaction.get("comment", ""):
                return str(transaction["id"])
        return None

    def add_transaction(
        self, transaction: BankTransaction, marker: str, session_id: str
    ) -> str:
        response = self.transport.request(
            "POST",
            "transactions/add.json",
            payload=self.build_payload(transaction, marker, session_id),
        )
        result = _result(response)
        if not isinstance(result, dict) or not result.get("id"):
            raise ApiError("ezBookkeeping returned no transaction id")
        return str(result["id"])
