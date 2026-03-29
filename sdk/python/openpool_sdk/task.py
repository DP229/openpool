"""
Task models for OpenPool SDK
"""
from dataclasses import dataclass
from typing import Any, Dict, Optional
import json


@dataclass
class Task:
    """Represents a compute task to be executed on the network."""
    
    op: str
    arg: Any
    credits: int = 10
    timeout_sec: int = 30
    wasm_path: str = ""
    
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


@dataclass
class TaskResult:
    """Result of a task execution."""
    
    status: str
    result: Optional[str] = None
    error: Optional[str] = None
    credits_deducted: int = 0
    runtime: str = "unknown"
    raw: Optional[Dict] = None
    
    @classmethod
    def from_dict(cls, data: Dict) -> 'TaskResult':
        return cls(
            status=data.get("status", "unknown"),
            result=data.get("result"),
            error=data.get("error"),
            credits_deducted=data.get("credits_deducted", 0),
            runtime=data.get("runtime", "unknown"),
            raw=data
        )
    
    @property
    def success(self) -> bool:
        return self.status == "ok" and self.error is None


# Operation constants
OPS = {
    "fib": "Fibonacci sequence",
    "sumFib": "Sum of Fibonacci numbers",
    "sumSquares": "Sum of squares",
    "matrixTrace": "Matrix trace"
}