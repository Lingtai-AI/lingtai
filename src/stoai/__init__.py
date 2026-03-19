"""stoai — generic AI agent framework with intrinsic tools, composable capabilities, and pluggable services."""
import sys

# Alias kernel modules so that ``from stoai.X import Y`` and
# ``from stoai_kernel.X import Y`` return the same objects during
# the transition period (before old local modules are deleted).
import stoai_kernel.base_agent
import stoai_kernel.config
import stoai_kernel.state
import stoai_kernel.types
import stoai_kernel.message
import stoai_kernel.services.mail
import stoai_kernel.services.logging
import stoai_kernel.logging
import stoai_kernel.loop_guard
import stoai_kernel.tool_executor
import stoai_kernel.intrinsics.mail

sys.modules.setdefault("stoai.base_agent", stoai_kernel.base_agent)
sys.modules.setdefault("stoai.config", stoai_kernel.config)
sys.modules.setdefault("stoai.state", stoai_kernel.state)
sys.modules.setdefault("stoai.types", stoai_kernel.types)
sys.modules.setdefault("stoai.message", stoai_kernel.message)
sys.modules.setdefault("stoai.services.mail", stoai_kernel.services.mail)
sys.modules.setdefault("stoai.services.logging", stoai_kernel.services.logging)
sys.modules.setdefault("stoai.logging", stoai_kernel.logging)
sys.modules.setdefault("stoai.loop_guard", stoai_kernel.loop_guard)
sys.modules.setdefault("stoai.tool_executor", stoai_kernel.tool_executor)
sys.modules.setdefault("stoai.intrinsics.mail", stoai_kernel.intrinsics.mail)

from stoai_kernel.types import UnknownToolError
from stoai_kernel.config import AgentConfig
from stoai_kernel.base_agent import BaseAgent
from .agent import Agent
from stoai_kernel.state import AgentState
from stoai_kernel.message import Message, MSG_REQUEST, MSG_USER_INPUT

# Capabilities
from .capabilities import setup_capability
from .capabilities.bash import BashManager
from .capabilities.delegate import DelegateManager
from .capabilities.email import EmailManager

# Services
from .services.file_io import FileIOService, LocalFileIOService, GrepMatch
from stoai_kernel.services.mail import MailService, TCPMailService
from .services.vision import VisionService, LLMVisionService
from .services.search import SearchService, LLMSearchService, SearchResult
from stoai_kernel.services.logging import LoggingService, JSONLLoggingService

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
    "DelegateManager",
    "EmailManager",
    # Services
    "FileIOService",
    "LocalFileIOService",
    "GrepMatch",
    "MailService",
    "TCPMailService",
    "VisionService",
    "LLMVisionService",
    "SearchService",
    "LLMSearchService",
    "SearchResult",
    "LoggingService",
    "JSONLLoggingService",
]
