/**
 * OpenPool SDK - Node.js Client
 * 
 * Usage:
 *   import { OpenPoolClient, Task } from 'openpool-sdk';
 *   const client = new OpenPoolClient('http://localhost:8080');
 *   const result = await client.fib(30);
 *   console.log(result.result);
 */

export class Task {
  constructor(op, arg, options = {}) {
    this.op = op;
    this.arg = arg;
    this.credits = options.credits ?? 10;
    this.timeout_sec = options.timeout_sec ?? 30;
    this.wasm_path = options.wasm_path ?? '';
  }

  toJSON() {
    return {
      op: this.op,
      arg: this.arg,
      credits: this.credits,
      timeout_sec: this.timeout_sec,
      wasm_path: this.wasm_path
    };
  }
}

export class TaskResult {
  constructor(data) {
    this.raw = data;
    this.status = data.status ?? 'unknown';
    this.result = data.result;
    this.error = data.error;
    this.credits_deducted = data.credits_deducted ?? 0;
    this.runtime = data.runtime ?? 'unknown';
  }

  get success() {
    return this.status === 'ok' && !this.error;
  }
}

export class OpenPoolClient {
  constructor(apiUrl = 'http://localhost:8080', options = {}) {
    this.apiUrl = apiUrl.replace(/\/$/, '');
    this.timeout = options.timeout ?? 60000;
  }

  async _request(method, path, body = null) {
    const url = `${this.apiUrl}${path}`;
    const fetchOptions = {
      method,
      headers: { 'Content-Type': 'application/json' },
      signal: AbortSignal.timeout(this.timeout)
    };
    if (body) fetchOptions.body = JSON.stringify(body);

    const res = await fetch(url, fetchOptions);
    if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
    return res.json();
  }

  async status() {
    return this._request('GET', '/status');
  }

  async ledger() {
    return this._request('GET', '/ledger');
  }

  async credits() {
    const s = await this.status();
    return s.credits ?? 0;
  }

  async connect(address) {
    return this._request('POST', '/connect', { address });
  }

  async discover(maxPeers = 10) {
    return this._request('GET', `/discover?max=${maxPeers}`);
  }

  async submitTask(task) {
    const data = await this._request('POST', '/submit', task.toJSON ? task.toJSON() : task);
    return new TaskResult(data);
  }

  async runLocal(op, arg) {
    const data = await this._request('POST', '/run', { op, arg });
    return new TaskResult(data);
  }

  // Convenience methods
  async fib(n) { return this.runLocal('fib', n); }
  async sumFib(n) { return this.runLocal('sumFib', n); }
  async sumSquares(n) { return this.runLocal('sumSquares', n); }
  async matrixTrace(n) { return this.runLocal('matrixTrace', n); }

  async waitForPeers(minPeers = 1, timeout = 30000) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      const s = await this.status();
      if ((s.connected_peers ?? 0) >= minPeers) return true;
      await new Promise(r => setTimeout(r, 1000));
    }
    return false;
  }
}
