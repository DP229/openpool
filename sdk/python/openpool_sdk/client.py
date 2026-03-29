"""
OpenPool - Python SDK Client
"""
import json
import time
from typing import Optional, Dict, Any, List
import requests


class Task:
    """Represents a compute task to be executed on the network."""
    
    def __init__(
        self,
        op: str,
        arg: Any,
        credits: int = 10,
        timeout_sec: int = 30,
        wasm_path: str = ""
    ):
        self.op = op
        self.arg = arg
        self.credits = credits
        self.timeout_sec = timeout_sec
        self.wasm_path = wasm_path
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "op": self.op,
            "arg": self.arg,
            "credits": self.credits,
            "timeout_sec": self.timeout_sec,
            "wasm_path": self.wasm_path
        }
    
    def to_json(self) -> str:
        return json.dumps(self.to_dict())


class TaskResult:
    """Result of a task execution."""
    
    def __init__(self, data: Dict[str, Any]):
        self.raw = data
        self.status = data.get("status", "unknown")
        self.result = data.get("result")
        self.error = data.get("error")
        self.credits_deducted = data.get("credits_deducted", 0)
        self.runtime = data.get("runtime", "unknown")
    
    @property
    def success(self) -> bool:
        return self.status == "ok" and self.error is None


class OpenPoolClient:
    """
    Python client for OpenPool network.
    
    Usage:
        client = OpenPoolClient("http://localhost:8080")
        result = client.submit_task(Task(op="fib", arg=30))
        print(result.result)
    """
    
    def __init__(
        self,
        api_url: str = "http://localhost:8080",
        timeout: int = 60
    ):
        self.api_url = api_url.rstrip("/")
        self.timeout = timeout
        self.session = requests.Session()
    
    def _request(self, method: str, path: str, **kwargs) -> requests.Response:
        """Make an HTTP request to the API."""
        url = f"{self.api_url}{path}"
        kwargs.setdefault("timeout", self.timeout)
        return self.session.request(method, url, **kwargs)
    
    def status(self) -> Dict[str, Any]:
        """Get node status."""
        resp = self._request("GET", "/status")
        resp.raise_for_status()
        return resp.json()
    
    def ledger(self) -> List[Dict[str, Any]]:
        """Get credit ledger."""
        resp = self._request("GET", "/ledger")
        resp.raise_for_status()
        return resp.json()
    
    def credits(self) -> int:
        """Get current node's credit balance."""
        status = self.status()
        return status.get("credits", 0)
    
    def connect(self, address: str) -> Dict[str, str]:
        """Connect to a peer."""
        resp = self._request("POST", "/connect", json={"address": address})
        resp.raise_for_status()
        return resp.json()
    
    def discover(self, max_peers: int = 10) -> Dict[str, Any]:
        """Discover peers via DHT."""
        resp = self._request("GET", f"/discover?max={max_peers}")
        resp.raise_for_status()
        return resp.json()
    
    def submit_task(self, task: Task) -> TaskResult:
        """Submit a task via P2P network."""
        resp = self._request("POST", "/submit", json=task.to_dict())
        resp.raise_for_status()
        return TaskResult(resp.json())
    
    def run_local(self, op: str, arg: Any) -> TaskResult:
        """Run a task locally (no P2P)."""
        resp = self._request("POST", "/run", json={"op": op, "arg": arg})
        resp.raise_for_status()
        return TaskResult(resp.json())
    
    # Convenience methods for common operations
    def fib(self, n: int) -> TaskResult:
        """Calculate Fibonacci(n) locally."""
        return self.run_local("fib", n)
    
    def sum_fib(self, n: int) -> TaskResult:
        """Calculate sum of Fibonacci(0..n) locally."""
        return self.run_local("sumFib", n)
    
    def sum_squares(self, n: int) -> TaskResult:
        """Calculate sum of squares 1²..n² locally."""
        return self.run_local("sumSquares", n)
    
    def matrix_trace(self, n: int) -> TaskResult:
        """Calculate matrix trace for n×n matrix locally."""
        return self.run_local("matrixTrace", n)
    
    def wait_for_peers(self, min_peers: int = 1, timeout: int = 30) -> bool:
        """Wait for peers to connect."""
        start = time.time()
        while time.time() - start < timeout:
            status = self.status()
            connected = status.get("connected_peers", 0)
            if connected >= min_peers:
                return True
            time.sleep(1)
        return False
    
    def __repr__(self) -> str:
        return f"OpenPoolClient({self.api_url})"