from __future__ import annotations

import re
from abc import ABC, abstractmethod
from datetime import datetime, timedelta
from email.utils import parseaddr

from .models import BankTransaction, money_to_minor


class BillParser(ABC):
    source: str
    subject_keywords: tuple[str, ...]
    allowed_senders: frozenset[str]

    def matches(self, sender: str, subject: str) -> bool:
        sender_address = parseaddr(sender)[1].lower()
        return sender_address in self.allowed_senders and any(
            keyword in subject for keyword in self.subject_keywords
        )

    @abstractmethod
    def parse(self, text: str, received_at: datetime) -> list[BankTransaction]:
        raise NotImplementedError


def _transaction_datetime(
    date_text: str, time_text: str, received_at: datetime
) -> datetime:
    if received_at.tzinfo is None:
        raise ValueError("received_at must be timezone-aware")

    if "/" in date_text:
        date_value = datetime.strptime(date_text, "%Y/%m/%d").date()
    else:
        month, day = (int(part) for part in re.findall(r"\d+", date_text))
        date_value = received_at.date().replace(month=month, day=day)
        if datetime.combine(date_value, datetime.min.time(), received_at.tzinfo) > (
            received_at + timedelta(days=31)
        ):
            date_value = date_value.replace(year=date_value.year - 1)

    time_format = "%H:%M:%S" if time_text.count(":") == 2 else "%H:%M"
    time_value = datetime.strptime(time_text, time_format).time()
    return datetime.combine(date_value, time_value, received_at.tzinfo)


class CmbCreditCardParser(BillParser):
    source = "cmb_credit"
    subject_keywords = ("每日信用管家",)
    allowed_senders = frozenset(
        {"ccsvc@message.cmbchina.com", "95555@message.cmbchina.com"}
    )
    _date_pattern = re.compile(r"(\d{4}/\d{2}/\d{2})")
    _transaction_pattern = re.compile(
        r"(\d{2}:\d{2}:\d{2})\s+CNY\s+(-?\d+(?:\.\d+)?)\s+"
        r"(.*?)(?=\s*(?:\d{2}:\d{2}:\d{2}|\(每日邮件\)|\(Daily Email\)|$))",
        re.DOTALL,
    )
    _card_prefix_pattern = re.compile(r"尾号\d+\s*(?:消费|退货|预授权完成)\s*")

    def parse(self, text: str, received_at: datetime) -> list[BankTransaction]:
        date_match = self._date_pattern.search(text)
        date_text = (
            date_match.group(1) if date_match else received_at.strftime("%Y/%m/%d")
        )
        transactions: list[BankTransaction] = []

        for match in self._transaction_pattern.finditer(text):
            raw_description = " ".join(match.group(3).split())
            merchant = self._card_prefix_pattern.sub("", raw_description).strip()
            amount_minor = -money_to_minor(match.group(2))
            if not merchant or amount_minor == 0:
                continue
            transactions.append(
                BankTransaction(
                    source=self.source,
                    occurred_at=_transaction_datetime(
                        date_text, match.group(1), received_at
                    ),
                    amount_minor=amount_minor,
                    merchant=merchant,
                    description=raw_description,
                )
            )

        return transactions


class CmbDebitCardParser(BillParser):
    source = "cmb_debit"
    subject_keywords = ("通知",)
    allowed_senders = frozenset({"95555@message.cmbchina.com"})
    _rules = (
        (
            re.compile(
                r"于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?在(.*?)(?:快捷支付|支付|消费)"
                r"(?:人民币)?(\d+(?:\.\d+)?)元",
                re.DOTALL,
            ),
            {"date": 1, "time": 2, "merchant": 3, "amount": 4},
            -1,
            "支出",
        ),
        (
            re.compile(
                r"于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?向(.*?)(?:转账|汇款)"
                r"(?:人民币)?(\d+(?:\.\d+)?)元",
                re.DOTALL,
            ),
            {"date": 1, "time": 2, "merchant": 3, "amount": 4},
            -1,
            "转账支出",
        ),
        (
            re.compile(
                r"于(\d{2}月\d{2}日)(\d{2}:\d{2}).*?(?:在(.*?))?取出"
                r"(?:人民币)?(\d+(?:\.\d+)?)元",
                re.DOTALL,
            ),
            {"date": 1, "time": 2, "merchant": 3, "amount": 4},
            -1,
            "取款",
        ),
        (
            re.compile(
                r"于(\d{2}月\d{2}日)(\d{2}:\d{2})(.*?)(?:入账|存入|退款)"
                r"(?:人民币)?(\d+(?:\.\d+)?)元",
                re.DOTALL,
            ),
            {"date": 1, "time": 2, "merchant": 3, "amount": 4},
            1,
            "入账",
        ),
        (
            re.compile(
                r"(资金归集[^。；]*?).*?(?:向|从|由|转账).*?(?:人民币)?"
                r"(\d+(?:\.\d+)?)元.*?截至(\d{2}月\d{2}日)(\d{2}:\d{2})",
                re.DOTALL,
            ),
            {"merchant_prefix": 1, "amount": 2, "date": 3, "time": 4},
            1,
            "入账",
        ),
    )

    def parse(self, text: str, received_at: datetime) -> list[BankTransaction]:
        transactions: list[BankTransaction] = []
        matched_spans: list[tuple[int, int]] = []

        for pattern, groups, sign, label in self._rules:
            for match in pattern.finditer(text):
                if any(
                    match.start() < end and match.end() > start
                    for start, end in matched_spans
                ):
                    continue
                matched_spans.append(match.span())
                if "merchant_prefix" in groups:
                    prefix = match.group(groups["merchant_prefix"]).strip(" ，,")
                    merchant = f"招行-{prefix[:20]}"
                else:
                    merchant = (
                        match.group(groups["merchant"]) or "招商银行"
                    ).strip(" ，,")
                amount_minor = sign * money_to_minor(match.group(groups["amount"]))
                if amount_minor == 0:
                    continue
                transactions.append(
                    BankTransaction(
                        source=self.source,
                        occurred_at=_transaction_datetime(
                            match.group(groups["date"]),
                            match.group(groups["time"]),
                            received_at,
                        ),
                        amount_minor=amount_minor,
                        merchant=merchant,
                        description=f"{merchant} - {label}",
                    )
                )

        return transactions


DEFAULT_PARSERS: tuple[BillParser, ...] = (
    CmbCreditCardParser(),
    CmbDebitCardParser(),
)
