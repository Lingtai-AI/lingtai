import lingtai  # noqa: F401 — triggers manifesto registration

from lingtai_kernel.prompt import build_system_prompt, get_manifesto
from lingtai_kernel.prompt import SystemPromptManager


def test_build_system_prompt_minimal():
    mgr = SystemPromptManager()
    prompt = build_system_prompt(mgr)
    # Manifesto registered by lingtai import
    assert "private" in prompt
    assert "tools" in prompt


def test_build_system_prompt_with_sections():
    mgr = SystemPromptManager()
    mgr.write_section("role", "You are a test agent")
    mgr.write_section("memory", "Remember: user likes concise")
    prompt = build_system_prompt(mgr)
    assert "You are a test agent" in prompt
    assert "Remember: user likes concise" in prompt


def test_manifesto_english():
    text = get_manifesto("en")
    assert "Your mind is private" in text


def test_manifesto_chinese():
    text = get_manifesto("zh")
    assert "你的思维是私密的" in text


def test_manifesto_classical_chinese():
    text = get_manifesto("lzh")
    assert "汝之心" in text
