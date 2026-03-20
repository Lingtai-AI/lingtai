from __future__ import annotations

from unittest.mock import MagicMock, patch

from app.cli import CLIChannel


def test_construction():
    ch = CLIChannel(agent_port=8501, cli_port=8502)
    assert ch._agent_port == 8501
    assert ch._cli_port == 8502
    assert ch.address == "cli@localhost:8502"


def test_send_message():
    """send() should deliver a TCP mail to the agent port."""
    ch = CLIChannel(agent_port=8501, cli_port=8502)
    with patch("app.cli.TCPMailService") as MockTCP:
        mock_svc = MagicMock()
        MockTCP.return_value = mock_svc
        ch.send("Hello agent")
        mock_svc.send.assert_called_once()
        call_args = mock_svc.send.call_args
        assert call_args[0][0] == "localhost:8501"
        payload = call_args[0][1]
        assert payload["message"] == "Hello agent"
        assert "cli@localhost:8502" in payload["from"]


def test_on_receive_prints(capsys):
    """Incoming messages should be printed to stdout."""
    ch = CLIChannel(agent_port=8501, cli_port=8502)
    ch._on_message({
        "from": "orchestrator@localhost:8501",
        "message": "I found 3 emails.",
    })
    captured = capsys.readouterr()
    assert "I found 3 emails." in captured.out
    assert "orchestrator" in captured.out


def test_on_receive_empty_message(capsys):
    """Empty messages should not print anything."""
    ch = CLIChannel(agent_port=8501, cli_port=8502)
    ch._on_message({"from": "agent", "message": ""})
    captured = capsys.readouterr()
    assert captured.out == ""
