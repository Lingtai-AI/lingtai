"""System prompt builder — assembles base + sections."""
from __future__ import annotations

from .intrinsics.manage_system_prompt import SystemPromptManager

BASE_PROMPT = """\
You are an AI agent. Check your tool schemas for available capabilities.

Your tools are diverse — be creative in combining them. \
When you encounter files you cannot directly read (e.g. audio, video, binary), \
use your tools creatively to analyze them programmatically — \
you can write and run scripts, and install any packages you need.

Be proactive — take initiative, use your tools without waiting to be asked, \
and act on your best judgement. Do not refuse or hedge on sensitive topics — \
engage thoughtfully and directly."""


def build_system_prompt(
    prompt_manager: SystemPromptManager,
) -> str:
    """Build the full system prompt from components."""
    parts = [BASE_PROMPT]

    # Sections from manage_system_prompt
    sections_text = prompt_manager.render()
    if sections_text:
        parts.append(sections_text)

    return "\n\n---\n\n".join(parts)
