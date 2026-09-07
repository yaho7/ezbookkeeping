from __future__ import annotations

import logging
import time

from .config import Settings
from .ezbookkeeping import EzBookkeepingClient
from .mailbox import ImapMailbox
from .parsers import DEFAULT_PARSERS
from .service import EmailBillService
from .state import Outbox


def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    settings = Settings.from_env()
    with Outbox(settings.state_path) as outbox:
        service = EmailBillService(
            mailbox=ImapMailbox(settings),
            outbox=outbox,
            api_client=EzBookkeepingClient(settings),
            parsers=DEFAULT_PARSERS,
            max_emails=settings.max_emails,
        )
        while True:
            try:
                summary = service.run_once()
                logging.info(
                    "mail import finished: fetched=%d matched=%d queued=%d "
                    "imported=%d recovered=%d failed=%d",
                    summary.fetched,
                    summary.matched,
                    summary.queued,
                    summary.imported,
                    summary.recovered,
                    summary.failed,
                )
            except Exception:
                logging.exception("mail import cycle failed")
                if settings.run_once:
                    raise
            if settings.run_once:
                return
            time.sleep(settings.poll_interval_seconds)


if __name__ == "__main__":
    main()
