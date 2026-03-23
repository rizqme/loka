"""LOKA SDK type definitions."""

from __future__ import annotations
from dataclasses import dataclass, field
from typing import Any


@dataclass
class Session:
    ID: str = ""
    Name: str = ""
    Status: str = ""
    Mode: str = ""
    WorkerID: str = ""
    ImageRef: str = ""
    ImageID: str = ""
    SnapshotID: str = ""
    VCPUs: int = 0
    MemoryMB: int = 0
    Labels: dict[str, str] = field(default_factory=dict)
    CreatedAt: str = ""
    UpdatedAt: str = ""


@dataclass
class CommandResult:
    CommandID: str = ""
    ExitCode: int = 0
    Stdout: str = ""
    Stderr: str = ""
    StartedAt: str = ""
    EndedAt: str = ""


@dataclass
class Execution:
    ID: str = ""
    SessionID: str = ""
    Status: str = ""
    Parallel: bool = False
    Commands: list[dict[str, Any]] = field(default_factory=list)
    Results: list[CommandResult] = field(default_factory=list)
    CreatedAt: str = ""
    UpdatedAt: str = ""


@dataclass
class Checkpoint:
    ID: str = ""
    SessionID: str = ""
    ParentID: str = ""
    Type: str = ""
    Status: str = ""
    Label: str = ""
    CreatedAt: str = ""


@dataclass
class Image:
    ID: str = ""
    Reference: str = ""
    Digest: str = ""
    SizeMB: int = 0
    Status: str = ""
    CreatedAt: str = ""


@dataclass
class StreamEvent:
    """A single event from a streaming execution."""
    event: str = ""   # output, status, result, approval_required, error, done
    data: dict = field(default_factory=dict)

    @property
    def is_output(self) -> bool: return self.event == "output"
    @property
    def is_done(self) -> bool: return self.event == "done"
    @property
    def text(self) -> str: return self.data.get("text", "")
    @property
    def stream_name(self) -> str: return self.data.get("stream", "")


@dataclass
class Worker:
    ID: str = ""
    Hostname: str = ""
    Provider: str = ""
    Region: str = ""
    Status: str = ""
    Labels: dict[str, str] = field(default_factory=dict)
