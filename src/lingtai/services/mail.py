"""Re-export kernel mail services."""
from lingtai_kernel.services.mail import MailService, FilesystemMailService

__all__ = ["MailService", "FilesystemMailService"]
