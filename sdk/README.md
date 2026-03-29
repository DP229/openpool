# OpenPool SDK

Client libraries for interacting with the OpenPool distributed computing network.

## Python SDK

```bash
cd sdk/python
pip install -r requirements.txt
```

```python
from openpool_sdk import OpenPoolClient, Task

client = OpenPoolClient("http://localhost:8080")

# Local execution (no P2P needed)
result = client.fib(30)
print(result.result)  # 832040

# Submit via P2P network
result = client.submit_task(Task(op="fib", arg=30))
```

## Node.js SDK

```bash
cd sdk/js
npm install
```

```javascript
import { OpenPoolClient } from './index.js';

const client = new OpenPoolClient('http://localhost:8080');
const result = await client.fib(30);
console.log(result.result);
```

## API Reference

### OpenPoolClient

| Method | Description |
|--------|-------------|
| `status()` | Get node status, peer info, credits |
| `ledger()` | Get all credit balances |
| `credits()` | Get current node's credits |
| `connect(address)` | Connect to a peer |
| `discover(maxPeers)` | Discover peers via DHT |
| `submit_task(task)` | Submit task via P2P |
| `run_local(op, arg)` | Run task locally |

### Convenience Methods

- `fib(n)` - Fibonacci(n)
- `sum_fib(n)` - Sum of Fibonacci(0..n)
- `sum_squares(n)` - Sum of squares 1²..n²
- `matrix_trace(n)` - Matrix trace

### Task

```python
task = Task(
    op="fib",
    arg=30,
    credits=10,
    timeout_sec=30,
    wasm_path=""
)
result = client.submit_task(task)
```