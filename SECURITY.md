# OpenPool Security Implementation - Sprint 1

## Overview

This document describes the security features implemented in Sprint 1 as part of the OpenPool security hardening effort.

## Features Implemented

### 1. Input Validation and Sanitization (`pkg/security/`)

**Path Validation:**
- Validates WASM file paths to prevent path traversal attacks
- Restricts files to allowed directories
- Only allows whitelisted file extensions (.wasm, .py, .json)
- Checks file size limits (100MB max)

**String Sanitization:**
- Removes null bytes from input
- Strips dangerous HTML tags (script, javascript:, onerror, onload)
- Truncates strings to maximum length

**Input Validation:**
- API key format validation
- Task ID validation
- Credits validation (non-negative, within limits)

### 2. Rate Limiting (`pkg/ratelimit/`)

**Token Bucket Algorithm:**
- Configurable rate (requests per minute)
- Burst capacity for temporary spikes
- Per-client rate limiting
- Automatic cleanup of inactive clients

**Features:**
- Rate: 100 requests/minute (configurable)
- Burst: 20 requests (configurable)
- Graceful degradation
- Distributed rate limiting support

### 3. Authentication & Authorization (`pkg/auth/`)

**API Key Management:**
- Secure random key generation (op_<random>)
- SQLite-backed persistence
- Key expiration support (default 1 year)
- Credit tracking per key
- Scope-based permissions (submit, query, admin)

**Endpoints:**
- `POST /auth/apikey` - Generate new API key (admin only)
- `POST /auth/revoke` - Revoke API key (admin only)
- `GET /auth/keys` - List API keys for authenticated user

### 4. Security Middleware (`pkg/middleware/`)

**HTTP Middleware Chain:**
1. Rate limiting
2. Authentication
3. Scope checking
4. Input validation
5. Logging

**Features:**
- Configurable authentication requirement
- Scope-based authorization
- Request logging with status codes
- IP-based client identification
- X-Forwarded-For support

### 5. TLS Support

**Configuration:**
- TLS 1.2 minimum
- Strong cipher suites (ECDHE_RSA, AES-256)
- Certificate and key file support
- HTTPS enforcement

**Command-line Flags:**
- `--tls-cert <path>` - TLS certificate file
- `--tls-key <path>` - TLS private key file

## Usage

### Starting with Authentication Enabled

```bash
# Generate an admin secret first
export ADMIN_SECRET=$(openssl rand -hex 32)

# Start with authentication required
./openpool-secure \
  --http 8080 \
  --require-auth \
  --admin-secret $ADMIN_SECRET \
  --rate-limit 100
```

### Generating API Keys

```bash
# Create a new API key
curl -X POST http://localhost:8080/auth/apikey \
  -H "X-Admin-Secret: $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "owner_name": "John Doe",
    "owner_email": "john@example.com",
    "credits": 1000,
    "scopes": ["submit", "query"]
  }'
```

### Using API Keys

```bash
# Submit a task with API key
curl -X POST http://localhost:8080/submit \
  -H "X-API-Key: op_<your-key-here>" \
  -H "Content-Type: application/json" \
  -d '{"wasm_path": "test.wasm"}'
```

### Starting with TLS

```bash
# Generate self-signed certificate for testing
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes

# Start with TLS
./openpool-secure \
  --http 8080 \
  --tls-cert cert.pem \
  --tls-key key.pem
```

## Security Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| AUTH_DB | openpool_auth.db | Auth database path |
| ADMIN_SECRET | (none) | Secret for admin operations |
| REQUIRE_AUTH | false | Force authentication |
| RATE_LIMIT | 100 | Requests per minute |

### Rate Limits

| Endpoint | Rate Limit | Burst |
|----------|-----------|-------|
| /submit | 100/min | 20 |
| /status | 100/min | 20 |
| /ledger | 100/min | 20 |
| /auth/* | 100/min | 20 |

### Input Constraints

| Field | Constraint |
|-------|-----------|
| Task ID | Max 256 chars |
| Credits | 0 - 10,000 |
| WASM path | Whitelisted dirs only |
| JSON input | Max 10MB |

## Testing

Run security tests:

```bash
# Unit tests
go test ./pkg/security -v
go test ./pkg/ratelimit -v
go test ./pkg/auth -v
go test ./pkg/middleware -v

# Integration tests
go test ./... -v

# Security scan
gosec ./...
```

## Next Steps (Sprint 2)

1. **Worker Pool Implementation**
   - Concurrent task execution
   - Task queue management
   - Resource limits

2. **Circuit Breakers**
   - Prevent cascade failures
   - Timeout handling
   - Retry logic

3. **Graceful Shutdown**
   - Drain in-flight tasks
   - Save state before exit
   - Connection cleanup

4. **DDoS Protection**
   - Connection limits per peer
   - Stream limits
   - Bandwidth throttling

## Security Audit Checklist

- [x] Path traversal prevention
- [x] Rate limiting
- [x] API key authentication
- [x] TLS support
- [x] Input validation
- [x] Scope-based authorization
- [ ] SQL injection prevention (not applicable - using SQLite with parameterized queries)
- [ ] XSS prevention (not applicable - no web UI)
- [ ] CSRF protection (not applicable - API only)
- [ ] Command injection (needs review of task execution)

## Known Issues

1. **Command Injection Risk**: Task execution may still be vulnerable. Will be addressed in Sprint 2.

2. **Memory Leaks**: Rate limiter cleanup may retain inactive clients. Mitigated by 10-minute cleanup interval.

3. **Missing Rate Limit on P2P**: Rate limiting only applies to HTTP. P2P connections need similar protection.

## Support

For security issues, please email: security@openpool.dev

For general questions: support@openpool.dev

Documentation: https://docs.openpool.dev