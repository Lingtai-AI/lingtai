"""lingtai — generic AI agent framework with intrinsic tools, composable capabilities, and pluggable services."""

# Register manifestos with the kernel (en, zh, lzh)
from pathlib import Path as _Path
from lingtai_kernel.prompt import set_manifesto as _set_manifesto
_base = _Path(__file__).parent
for _lang, _file in [("en", "manifesto.md"), ("zh", "manifesto_zh.md"), ("lzh", "manifesto_lzh.md")]:
    _p = _base / _file
    if _p.is_file():
        _set_manifesto(_lang, _p.read_text().strip())
del _Path, _set_manifesto, _base, _lang, _file, _p

from lingtai_kernel.types import UnknownToolError
from lingtai_kernel.config import AgentConfig
from lingtai_kernel.base_agent import BaseAgent
from .agent import Agent
from lingtai_kernel.state import AgentState
from lingtai_kernel.message import Message, MSG_REQUEST, MSG_USER_INPUT

# Capabilities
from .capabilities import setup_capability
from .capabilities.bash import BashManager
from .capabilities.avatar import AvatarManager
from .capabilities.email import EmailManager

# Services
from .services.file_io import FileIOService, LocalFileIOService, GrepMatch
from lingtai_kernel.services.mail import MailService, FilesystemMailService
from .services.vision import VisionService, LLMVisionService
from .services.search import SearchService, LLMSearchService, SearchResult
from lingtai_kernel.services.logging import LoggingService, JSONLLoggingService

__all__ = [
    # Core
    "BaseAgent",
    "Agent",
    "Message",
    "AgentState",
    "MSG_REQUEST",
    "MSG_USER_INPUT",
    "AgentConfig",
    "UnknownToolError",
    # Capabilities
    "setup_capability",
    "BashManager",
    "AvatarManager",
    "EmailManager",
    # Services
    "FileIOService",
    "LocalFileIOService",
    "GrepMatch",
    "MailService",
    "FilesystemMailService",
    "VisionService",
    "LLMVisionService",
    "SearchService",
    "LLMSearchService",
    "SearchResult",
    "LoggingService",
    "JSONLLoggingService",
]
