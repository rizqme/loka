"""LOKA SDK type definitions."""

from __future__ import annotations
from dataclasses import dataclass, field
from typing import Any


@dataclass
class PortMapping:
    """Port forwarding from local machine to session VM."""
    local_port: int = 0
    remote_port: int = 0
    protocol: str = "tcp"


@dataclass
class StorageMount:
    """Object storage bucket mounted into a session VM."""
    provider: str = ""         # "s3", "gcs", "azure-blob", "local"
    bucket: str = ""
    mount_path: str = ""
    prefix: str = ""
    read_only: bool = False
    region: str = ""
    endpoint: str = ""         # For S3-compatible (MinIO, R2)
    credentials: dict[str, str] = field(default_factory=dict)
    # Git repository (provider="github" or "git")
    git_repo: str = ""        # "owner/repo" or full HTTPS URL
    git_ref: str = ""         # Branch, tag, or commit SHA
    # Host directory
    host_path: str = ""
    # Named volume
    name: str = ""

    @staticmethod
    def s3(bucket: str, mount_path: str, *, access_key_id: str = "", secret_access_key: str = "",
           prefix: str = "", read_only: bool = False, region: str = "", endpoint: str = "") -> "StorageMount":
        """Create an S3 mount."""
        creds = {}
        if access_key_id:
            creds["access_key_id"] = access_key_id
        if secret_access_key:
            creds["secret_access_key"] = secret_access_key
        return StorageMount(provider="s3", bucket=bucket, mount_path=mount_path,
                            prefix=prefix, read_only=read_only, region=region,
                            endpoint=endpoint, credentials=creds)

    @staticmethod
    def gcs(bucket: str, mount_path: str, *, service_account_json: str = "",
            prefix: str = "", read_only: bool = False) -> "StorageMount":
        """Create a GCS mount."""
        creds = {}
        if service_account_json:
            creds["service_account_json"] = service_account_json
        return StorageMount(provider="gcs", bucket=bucket, mount_path=mount_path,
                            prefix=prefix, read_only=read_only, credentials=creds)

    @staticmethod
    def azure(container: str, mount_path: str, *, account_name: str = "", account_key: str = "",
              sas_token: str = "", prefix: str = "", read_only: bool = False) -> "StorageMount":
        """Create an Azure Blob mount."""
        creds = {}
        if account_name:
            creds["account_name"] = account_name
        if account_key:
            creds["account_key"] = account_key
        if sas_token:
            creds["sas_token"] = sas_token
        return StorageMount(provider="azure-blob", bucket=container, mount_path=mount_path,
                            prefix=prefix, read_only=read_only, credentials=creds)

    @staticmethod
    def github(repo: str, mount_path: str, *, ref: str = "HEAD", credentials: str = "",
               read_only: bool = True) -> "StorageMount":
        """Mount a GitHub repository."""
        return StorageMount(provider="github", mount_path=mount_path, read_only=read_only,
                            git_repo=repo, git_ref=ref,
                            credentials={"token": credentials} if credentials else {})

    @staticmethod
    def local(host_path: str, mount_path: str, *, read_only: bool = False) -> "StorageMount":
        """Mount a host directory."""
        return StorageMount(provider="local", mount_path=mount_path, read_only=read_only,
                            host_path=host_path)

    @staticmethod
    def volume(name: str, mount_path: str, *, read_only: bool = False) -> "StorageMount":
        """Mount a named persistent volume."""
        return StorageMount(provider="volume", mount_path=mount_path, read_only=read_only,
                            name=name)

    @staticmethod
    def store(name: str, mount_path: str, *, read_only: bool = False) -> "StorageMount":
        """Mount a shared store volume (NFS-backed, cross-worker, lockable)."""
        return StorageMount(provider="store", mount_path=mount_path, read_only=read_only,
                            name=name)


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
    Mounts: list[Any] = field(default_factory=list)
    Ports: list[Any] = field(default_factory=list)
    Ready: bool = False
    StatusMessage: str = ""
    IdleTimeout: int = 0
    LastActivity: str = ""
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
class Artifact:
    """A file changed in a session."""
    id: str = ""
    session_id: str = ""
    checkpoint_id: str = ""
    path: str = ""
    size: int = 0
    hash: str = ""
    type: str = ""
    is_dir: bool = False
    created_at: str = ""


@dataclass
class Worker:
    ID: str = ""
    Hostname: str = ""
    Provider: str = ""
    Region: str = ""
    Status: str = ""
    Labels: dict[str, str] = field(default_factory=dict)


@dataclass
class Service:
    """A deployed long-running service."""
    ID: str = ""
    Name: str = ""
    Slug: str = ""
    Status: str = ""  # deploying, running, stopped, idle, error, terminated
    Image: str = ""
    Port: int = 0
    WorkerID: str = ""
    Env: dict[str, str] = field(default_factory=dict)
    Routes: list[Any] = field(default_factory=list)
    Autoscale: dict[str, Any] = field(default_factory=dict)
    Ready: bool = False
    StatusMessage: str = ""
    CreatedAt: str = ""
    UpdatedAt: str = ""


@dataclass
class ServiceRoute:
    Subdomain: str = ""
    CustomDomain: str = ""
    Port: int = 0
    Protocol: str = "http"


@dataclass
class VolumeRecord:
    """A named persistent volume."""
    Name: str = ""
    Type: str = ""
    Provider: str = ""
    MountCount: int = 0
    CreatedAt: str = ""
    UpdatedAt: str = ""


@dataclass
class WorkerToken:
    ID: str = ""
    Name: str = ""
    Token: str = ""
    ExpiresAt: str = ""
    CreatedAt: str = ""


@dataclass
class ObjectInfo:
    Key: str = ""
    Size: int = 0
    LastModified: str = ""
    ETag: str = ""
