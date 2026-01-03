# Terminal Tunnel API Collection

A comprehensive Bruno/Courier API collection demonstrating all features of the Terminal Tunnel relay server API.

## Features Demonstrated

This collection showcases:

- **Pre-request Scripts (Preflight)** - Validation, logging, and setup before each request
- **Post-response Scripts (Postflight)** - Response processing, variable extraction, logging
- **Tests** - Assertions using Chai expect syntax
- **Assertions** - Simple declarative assertions
- **Variables** - Request chaining via variable extraction
- **Environments** - Local and Production configurations
- **Documentation** - Comprehensive docs for each endpoint
- **Error Handling** - Tests for 400 and 404 responses
- **WebSocket** - Protocol documentation and test scripts

## Prerequisites

### Install Bruno

```bash
# macOS
brew install bruno

# Or download from https://www.usebruno.com/
```

### Start Local Relay Server (Optional)

```bash
# From terminal-tunnel directory
tt relay --port 8765
```

## Collection Structure

```
courier-collection/
├── bruno.json              # Collection manifest
├── collection.bru          # Collection-level scripts
├── README.md               # This file
├── environments/
│   ├── Local.bru           # localhost:8765
│   └── Production.bru      # Cloudflare Worker
├── health/
│   └── Health Check.bru    # GET /health
├── session/
│   ├── 1. Create Session.bru
│   ├── 2. Get Session.bru
│   ├── 3. Update Session (Reconnect).bru
│   ├── 4. Session Heartbeat.bru
│   ├── 5. Submit Answer.bru
│   ├── 6. Poll Answer.bru
│   ├── 7. Get Session Not Found.bru
│   └── 8. Create Session Invalid.bru
└── websocket/
    ├── WebSocket Connection.bru   # Protocol docs
    └── WebSocket Test Script.bru  # Test scripts
```

## Quick Start

1. **Open in Bruno**
   ```bash
   # Open Bruno and select "Open Collection"
   # Navigate to this directory
   ```

2. **Select Environment**
   - Click the environment dropdown (top right)
   - Select "Local" or "Production"

3. **Run Requests**
   - Start with "Health Check" to verify connectivity
   - Run "1. Create Session" to get a session code
   - Subsequent requests use the session code automatically

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /health | Health check |
| POST | /session | Create new session |
| GET | /session/{code} | Get session SDP |
| PUT | /session/{code} | Update session (reconnect) |
| PATCH | /session/{code} | Heartbeat (keep alive) |
| POST | /session/{code}/answer | Submit answer SDP |
| GET | /session/{code}/answer | Poll for answer |
| WS | /ws?session={code} | WebSocket connection |

## Script Examples

### Pre-request Script (Preflight)
```javascript
// Runs before the request
const code = bru.getVar("sessionCode");
if (!code) {
  throw new Error("sessionCode is not set");
}
console.log(`Processing session: ${code}`);
```

### Post-response Script (Postflight)
```javascript
// Runs after the response
if (res.getStatus() === 200) {
  const body = res.getBody();
  bru.setVar("sessionCode", body.code);
  console.log(`Session created: ${body.code}`);
}
```

### Tests
```javascript
test("Status is 200", function() {
  expect(res.getStatus()).to.equal(200);
});

test("Response has session code", function() {
  const body = res.getBody();
  expect(body).to.have.property("code");
  expect(body.code).to.match(/^[A-Z0-9]{8}$/);
});
```

### Assertions
```
res.status: eq 200
res.body.code: isDefined
res.responseTime: lte 3000
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `baseUrl` | API base URL (http://localhost:8765) |
| `wsUrl` | WebSocket URL (ws://localhost:8765) |
| `sessionCode` | Current session code (set by Create Session) |
| `testSdp` | Sample SDP for testing |
| `testSalt` | Sample salt for testing |
| `testAnswer` | Sample answer SDP |

## Running Tests

### All Tests
```bash
# Run entire collection
bruno run --env Local
```

### Single Request
```bash
# Run specific request
bruno run session/1.\ Create\ Session.bru --env Local
```

### CI/CD Integration
```bash
# With JUnit output
bruno run --env Local --output results.xml
```

## WebSocket Testing

Bruno doesn't natively support WebSocket. Use these tools:

```bash
# Using wscat
npm install -g wscat
wscat -c "ws://localhost:8765/ws?session=TEST1234"

# Using websocat
brew install websocat
websocat ws://localhost:8765/ws?session=TEST1234
```

See `websocket/WebSocket Connection.bru` for full protocol documentation.

## Complete Test Flow

1. **Health Check** → Verify server is running
2. **Create Session** → Get session code (stored in `sessionCode` variable)
3. **Get Session** → Verify session data
4. **Session Heartbeat** → Keep session alive
5. **Submit Answer** → Simulate client response
6. **Poll Answer** → Retrieve the answer

## Error Test Cases

- **7. Get Session Not Found** - Tests 404 response for invalid code
- **8. Create Session Invalid** - Tests 400 response for missing SDP

## Collection-Level Scripts

The `collection.bru` file contains scripts that run for every request:

- **Pre-request**: Logs request start, sets timestamp
- **Post-response**: Logs completion time and status

## Contributing

To add new requests:

1. Create a new `.bru` file in the appropriate folder
2. Include `meta`, docs, scripts, and tests
3. Follow the naming convention: `N. Request Name.bru`
4. Update this README if adding new endpoints

## License

MIT - Same as Terminal Tunnel
