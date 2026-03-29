"""
OpenPool SDK - Python client for distributed computing network
"""
__version__ = "0.1.0"

from .client import OpenPoolClient
from .task import Task, TaskResult

__all__ = ["OpenPoolClient", "Task", "TaskResult"]
