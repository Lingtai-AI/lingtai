# tests/test_addon_accounts.py
from __future__ import annotations

import json
from pathlib import Path

import pytest


def test_list_empty(tmp_path, monkeypatch):
    """No addon dir -> empty list."""
    monkeypatch.setenv("HOME", str(tmp_path))
    from lingtai.addons.accounts import list_addon_accounts

    assert list_addon_accounts("imap") == []


def test_list_with_accounts(tmp_path, monkeypatch):
    """Accounts are listed by alias (folder name)."""
    monkeypatch.setenv("HOME", str(tmp_path))
    addons = tmp_path / ".lingtai" / "addons" / "imap"
    (addons / "personal").mkdir(parents=True)
    (addons / "personal" / "config.json").write_text('{"email_address": "a@gmail.com"}')
    (addons / "work").mkdir(parents=True)
    (addons / "work" / "config.json").write_text('{"email_address": "b@outlook.com"}')

    from lingtai.addons.accounts import list_addon_accounts

    result = list_addon_accounts("imap")
    assert sorted(result) == ["personal", "work"]


def test_list_skips_dirs_without_config(tmp_path, monkeypatch):
    """Directories without config.json are ignored."""
    monkeypatch.setenv("HOME", str(tmp_path))
    addons = tmp_path / ".lingtai" / "addons" / "telegram"
    (addons / "broken").mkdir(parents=True)  # no config.json
    (addons / "good").mkdir(parents=True)
    (addons / "good" / "config.json").write_text('{"bot_token_env": "TG_TOKEN"}')

    from lingtai.addons.accounts import list_addon_accounts

    assert list_addon_accounts("telegram") == ["good"]


def test_load_existing(tmp_path, monkeypatch):
    """Load returns parsed config dict."""
    monkeypatch.setenv("HOME", str(tmp_path))
    cfg_dir = tmp_path / ".lingtai" / "addons" / "imap" / "personal"
    cfg_dir.mkdir(parents=True)
    data = {"email_address": "a@gmail.com", "email_password_env": "IMAP_PW", "imap_host": "imap.gmail.com"}
    (cfg_dir / "config.json").write_text(json.dumps(data))

    from lingtai.addons.accounts import load_addon_account

    result = load_addon_account("imap", "personal")
    assert result == data


def test_load_missing_raises(tmp_path, monkeypatch):
    """Loading a non-existent alias raises FileNotFoundError."""
    monkeypatch.setenv("HOME", str(tmp_path))
    from lingtai.addons.accounts import load_addon_account

    with pytest.raises(FileNotFoundError):
        load_addon_account("imap", "nonexistent")


def test_save_creates_new(tmp_path, monkeypatch):
    """Save creates alias dir and config.json."""
    monkeypatch.setenv("HOME", str(tmp_path))
    from lingtai.addons.accounts import save_addon_account

    cfg = {"bot_token_env": "TG_TOKEN"}
    result = save_addon_account("telegram", "my-bot", cfg)

    expected = tmp_path / ".lingtai" / "addons" / "telegram" / "my-bot" / "config.json"
    assert result == expected
    assert expected.is_file()
    assert json.loads(expected.read_text()) == cfg


def test_save_overwrites_existing(tmp_path, monkeypatch):
    """Save overwrites existing config.json."""
    monkeypatch.setenv("HOME", str(tmp_path))
    cfg_dir = tmp_path / ".lingtai" / "addons" / "imap" / "personal"
    cfg_dir.mkdir(parents=True)
    (cfg_dir / "config.json").write_text('{"old": true}')

    from lingtai.addons.accounts import save_addon_account

    new_cfg = {"email_address": "new@gmail.com", "email_password_env": "PW"}
    save_addon_account("imap", "personal", new_cfg)

    assert json.loads((cfg_dir / "config.json").read_text()) == new_cfg


def test_delete_removes_folder(tmp_path, monkeypatch):
    """Delete removes the alias folder entirely."""
    monkeypatch.setenv("HOME", str(tmp_path))
    cfg_dir = tmp_path / ".lingtai" / "addons" / "imap" / "personal"
    cfg_dir.mkdir(parents=True)
    (cfg_dir / "config.json").write_text('{"x": 1}')

    from lingtai.addons.accounts import delete_addon_account

    delete_addon_account("imap", "personal")
    assert not cfg_dir.exists()


def test_delete_missing_raises(tmp_path, monkeypatch):
    """Deleting a non-existent alias raises FileNotFoundError."""
    monkeypatch.setenv("HOME", str(tmp_path))
    from lingtai.addons.accounts import delete_addon_account

    with pytest.raises(FileNotFoundError):
        delete_addon_account("imap", "ghost")
