"""IMAP addon — real email via IMAP/SMTP.

Adds an `imap` tool with its own mailbox (working_dir/imap/).
An internal TCP bridge port lets other agents relay messages outward.

Single-account usage:
    agent = Agent(
        capabilities=["email", "file"],
        addons={"imap": {
            "email_address": "agent@example.com",
            "email_password": "xxxx xxxx xxxx xxxx",
            "imap_host": "imap.gmail.com",
            "smtp_host": "smtp.gmail.com",
        }},
    )

Multi-account usage:
    agent = Agent(
        capabilities=["email", "file"],
        addons={"imap": {
            "accounts": [
                {"email_address": "a@gmail.com", "email_password": "xxxx"},
                {"email_address": "b@outlook.com", "email_password": "yyyy",
                 "imap_host": "imap.outlook.com", "smtp_host": "smtp.outlook.com"},
            ],
        }},
    )
"""
from __future__ import annotations

import logging
from pathlib import Path
from typing import TYPE_CHECKING

from stoai_kernel.services.mail import TCPMailService
from .manager import IMAPMailManager, SCHEMA, DESCRIPTION
from .service import IMAPMailService

if TYPE_CHECKING:
    from stoai_kernel.base_agent import BaseAgent

log = logging.getLogger(__name__)


def setup(
    agent: "BaseAgent",
    *,
    # Single-account shorthand
    email_address: str | None = None,
    email_password: str | None = None,
    imap_host: str = "imap.gmail.com",
    imap_port: int = 993,
    smtp_host: str = "smtp.gmail.com",
    smtp_port: int = 587,
    allowed_senders: list[str] | None = None,
    poll_interval: int = 30,
    # Multi-account
    accounts: list[dict] | None = None,
    # Addon-level
    bridge_port: int = 8399,
) -> IMAPMailManager:
    """Set up IMAP addon — registers imap tool, creates services.

    Accepts either a flat single-account config or a list of account dicts.

    Listeners are NOT started here — they start in IMAPMailManager.start(),
    which is called by Agent.start() via the addon lifecycle.
    """
    if accounts is not None:
        account_list = accounts
    elif email_address is not None:
        account_list = [{
            "email_address": email_address,
            "email_password": email_password,
            "imap_host": imap_host,
            "imap_port": imap_port,
            "smtp_host": smtp_host,
            "smtp_port": smtp_port,
            "allowed_senders": allowed_senders,
            "poll_interval": poll_interval,
        }]
    else:
        raise ValueError(
            "imap addon requires either 'accounts' (list) or 'email_address' (str)"
        )

    working_dir = Path(agent._working_dir)
    tcp_alias = f"127.0.0.1:{bridge_port}"

    imap_svc = IMAPMailService(
        accounts=account_list,
        working_dir=working_dir,
    )

    bridge = TCPMailService(listen_port=bridge_port)

    mgr = IMAPMailManager(agent, service=imap_svc, tcp_alias=tcp_alias)
    mgr._bridge = bridge

    # Build system prompt listing all configured accounts
    addr_lines = "\n".join(
        f"  - {acct['email_address']}"
        for acct in account_list
    )
    system_prompt = (
        f"IMAP email accounts:\n{addr_lines}\n"
        f"Internal TCP alias: {tcp_alias} "
        f"(other agents can send to this address to relay via IMAP/SMTP)\n"
        f"Use imap(action=...) for external email. "
        f"Use email(action=...) for inter-agent communication."
    )

    agent.add_tool(
        "imap", schema=SCHEMA, handler=mgr.handle, description=DESCRIPTION,
        system_prompt=system_prompt,
    )

    log.info(
        "IMAP addon configured: %d account(s) (bridge: %s)",
        len(account_list), tcp_alias,
    )
    return mgr
