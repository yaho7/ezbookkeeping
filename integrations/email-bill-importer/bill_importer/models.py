from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal, ROUND_HALF_UP


def money_to_minor(value: str | int | float | Decimal) -> int:
    """Convert a decimal currency amount to integer minor units."""
    amount = Decimal(str(value)).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
    return int(amount * 100)


@dataclass(frozen=True, slots=True)
class BankTransaction:
    source: str
    occurred_at: datetime
    amount_minor: int
    merchant: str
    description: str

    def __post_init__(self) -> None:
        if self.occurred_at.tzinfo is None:
            raise ValueError("occurred_at must be timezone-aware")
        if self.amount_minor == 0:
            raise ValueError("amount_minor must not be zero")
