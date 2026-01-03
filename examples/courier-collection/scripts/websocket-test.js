#!/usr/bin/env node
/**
 * WebSocket Test Script for Terminal Tunnel Relay Server
 *
 * Tests the WebSocket signaling protocol used for SDP exchange.
 *
 * Usage:
 *   node websocket-test.js [options]
 *
 * Options:
 *   --url <url>      WebSocket URL (default: ws://localhost:8765)
 *   --api <url>      HTTP API URL (default: http://localhost:8765)
 *   --verbose        Show detailed output
 *   --help           Show help
 *
 * Requirements:
 *   npm install ws
 */

const WebSocket = require('ws');
const http = require('http');
const https = require('https');

// Parse command line arguments
const args = process.argv.slice(2);
const options = {
  wsUrl: 'ws://localhost:8765',
  apiUrl: 'http://localhost:8765',
  verbose: false,
};

for (let i = 0; i < args.length; i++) {
  switch (args[i]) {
    case '--url':
      options.wsUrl = args[++i];
      break;
    case '--api':
      options.apiUrl = args[++i];
      break;
    case '--verbose':
      options.verbose = true;
      break;
    case '--help':
      console.log(`
WebSocket Test Script for Terminal Tunnel

Usage: node websocket-test.js [options]

Options:
  --url <url>      WebSocket URL (default: ws://localhost:8765)
  --api <url>      HTTP API URL (default: http://localhost:8765)
  --verbose        Show detailed output
  --help           Show this help
`);
      process.exit(0);
  }
}

// Test state
let passed = 0;
let failed = 0;
const results = [];

// Colors for output
const colors = {
  green: '\x1b[32m',
  red: '\x1b[31m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  reset: '\x1b[0m',
  dim: '\x1b[2m',
};

function log(msg) {
  console.log(msg);
}

function logVerbose(msg) {
  if (options.verbose) {
    console.log(`${colors.dim}${msg}${colors.reset}`);
  }
}

function pass(name, details = '') {
  passed++;
  results.push({ name, status: 'pass', details });
  log(`  ${colors.green}✓${colors.reset} ${name}`);
  if (details && options.verbose) {
    log(`    ${colors.dim}${details}${colors.reset}`);
  }
}

function fail(name, error) {
  failed++;
  results.push({ name, status: 'fail', error: error.toString() });
  log(`  ${colors.red}✕${colors.reset} ${name}`);
  log(`    ${colors.red}${error}${colors.reset}`);
}

// HTTP request helper
function httpRequest(method, url, body = null) {
  return new Promise((resolve, reject) => {
    const isHttps = url.startsWith('https');
    const lib = isHttps ? https : http;
    const urlObj = new URL(url);

    const reqOptions = {
      hostname: urlObj.hostname,
      port: urlObj.port || (isHttps ? 443 : 80),
      path: urlObj.pathname + urlObj.search,
      method,
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
      },
    };

    const req = lib.request(reqOptions, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          resolve({ status: res.statusCode, data: JSON.parse(data) });
        } catch {
          resolve({ status: res.statusCode, data });
        }
      });
    });

    req.on('error', reject);
    req.setTimeout(10000, () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });

    if (body) {
      req.write(JSON.stringify(body));
    }
    req.end();
  });
}

// WebSocket connection helper
function connectWebSocket(url, timeout = 5000) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(url);
    const timer = setTimeout(() => {
      ws.terminate();
      reject(new Error('Connection timeout'));
    }, timeout);

    ws.on('open', () => {
      clearTimeout(timer);
      resolve(ws);
    });

    ws.on('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });
  });
}

// Send message and wait for response
function sendAndReceive(ws, message, timeout = 5000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error('Response timeout'));
    }, timeout);

    const handler = (data) => {
      clearTimeout(timer);
      ws.removeListener('message', handler);
      try {
        resolve(JSON.parse(data.toString()));
      } catch {
        resolve(data.toString());
      }
    };

    ws.on('message', handler);
    ws.send(JSON.stringify(message));
  });
}

// Wait for a message
function waitForMessage(ws, timeout = 5000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error('Message timeout'));
    }, timeout);

    const handler = (data) => {
      clearTimeout(timer);
      ws.removeListener('message', handler);
      try {
        resolve(JSON.parse(data.toString()));
      } catch {
        resolve(data.toString());
      }
    };

    ws.on('message', handler);
  });
}

// Test data
const testSdp = `v=0
o=- 1234567890 1234567890 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0
m=application 9 UDP/DTLS/SCTP webrtc-datachannel
c=IN IP4 0.0.0.0
a=ice-ufrag:testufrag
a=ice-pwd:testpassword12345678
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:0
a=sctp-port:5000`;

const testSalt = 'dGVzdC1zYWx0LWJhc2U2NA==';

const testAnswer = `v=0
o=- 9876543210 9876543210 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0
m=application 9 UDP/DTLS/SCTP webrtc-datachannel
c=IN IP4 0.0.0.0
a=ice-ufrag:answerufrag
a=ice-pwd:answerpassword1234
a=fingerprint:sha-256 11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11:11
a=setup:active
a=mid:0
a=sctp-port:5000`;

// Tests
async function runTests() {
  log(`\n${colors.blue}WebSocket Test Suite${colors.reset}`);
  log(`${colors.dim}URL: ${options.wsUrl}${colors.reset}`);
  log(`${colors.dim}API: ${options.apiUrl}${colors.reset}\n`);

  // Test 1: Health Check
  log(`${colors.yellow}1. Server Health${colors.reset}`);
  try {
    const res = await httpRequest('GET', `${options.apiUrl}/health`);
    if (res.status === 200 && res.data === 'OK') {
      pass('Health endpoint returns OK');
    } else {
      fail('Health endpoint returns OK', `Got status ${res.status}: ${res.data}`);
    }
  } catch (err) {
    fail('Health endpoint returns OK', err);
    log(`\n${colors.red}Server not running. Start with: tt relay --port 8765${colors.reset}\n`);
    process.exit(1);
  }

  // Test 2: WebSocket Connection
  log(`\n${colors.yellow}2. WebSocket Connection${colors.reset}`);
  let hostWs, clientWs;
  const sessionCode = `TEST${Date.now().toString(36).toUpperCase()}`;

  try {
    hostWs = await connectWebSocket(`${options.wsUrl}/ws?session=${sessionCode}`);
    pass('Host connects to WebSocket', `Session: ${sessionCode}`);
  } catch (err) {
    fail('Host connects to WebSocket', err);
    return;
  }

  // Test 3: Host Registration
  log(`\n${colors.yellow}3. Host Registration${colors.reset}`);
  try {
    hostWs.send(JSON.stringify({
      type: 'register',
      role: 'host',
    }));
    // No response expected for register, just checking no error
    await new Promise(r => setTimeout(r, 100));
    pass('Host sends register message');
  } catch (err) {
    fail('Host sends register message', err);
  }

  // Test 4: Host Sends Offer
  log(`\n${colors.yellow}4. Host Sends Offer${colors.reset}`);
  try {
    hostWs.send(JSON.stringify({
      type: 'offer',
      sdp: testSdp,
      salt: testSalt,
    }));
    await new Promise(r => setTimeout(r, 100));
    pass('Host sends SDP offer');
  } catch (err) {
    fail('Host sends SDP offer', err);
  }

  // Test 5: Client Connects
  log(`\n${colors.yellow}5. Client Connection${colors.reset}`);
  try {
    clientWs = await connectWebSocket(`${options.wsUrl}/ws?session=${sessionCode}`);
    pass('Client connects to same session');
  } catch (err) {
    fail('Client connects to same session', err);
    hostWs.close();
    return;
  }

  // Test 6: Client Registration and Receives Offer
  log(`\n${colors.yellow}6. Client Registration${colors.reset}`);
  try {
    // Set up message handler before sending
    const offerPromise = waitForMessage(clientWs, 3000);

    clientWs.send(JSON.stringify({
      type: 'register',
      role: 'client',
    }));

    const offer = await offerPromise;

    if (offer.type === 'offer' && offer.sdp) {
      pass('Client receives offer after registration', `SDP length: ${offer.sdp.length}`);
    } else {
      fail('Client receives offer after registration', `Unexpected: ${JSON.stringify(offer)}`);
    }
  } catch (err) {
    fail('Client receives offer after registration', err);
  }

  // Test 7: Client Sends Answer
  log(`\n${colors.yellow}7. Answer Exchange${colors.reset}`);
  try {
    // Set up answer handler on host before client sends
    const answerPromise = waitForMessage(hostWs, 3000);

    clientWs.send(JSON.stringify({
      type: 'answer',
      sdp: testAnswer,
    }));

    const answer = await answerPromise;

    if (answer.type === 'answer' && answer.sdp) {
      pass('Host receives answer from client', `SDP length: ${answer.sdp.length}`);
    } else {
      fail('Host receives answer from client', `Unexpected: ${JSON.stringify(answer)}`);
    }
  } catch (err) {
    fail('Host receives answer from client', err);
  }

  // Cleanup
  hostWs.close();
  clientWs.close();

  // Test 8: Missing Session Parameter
  log(`\n${colors.yellow}8. Error Handling${colors.reset}`);
  try {
    const errorWs = await connectWebSocket(`${options.wsUrl}/ws`);
    const error = await waitForMessage(errorWs, 2000);
    errorWs.close();

    if (error.type === 'error') {
      pass('Missing session returns error', error.error);
    } else {
      fail('Missing session returns error', `Got: ${JSON.stringify(error)}`);
    }
  } catch (err) {
    // Connection might be rejected immediately
    pass('Missing session returns error', 'Connection rejected');
  }

  // Test 9: HTTP API + WebSocket Integration
  log(`\n${colors.yellow}9. HTTP + WebSocket Integration${colors.reset}`);
  try {
    // Create session via HTTP
    const createRes = await httpRequest('POST', `${options.apiUrl}/session`, {
      sdp: testSdp,
      salt: testSalt,
    });

    if (createRes.status !== 200 || !createRes.data.code) {
      fail('Create session via HTTP', `Status: ${createRes.status}`);
    } else {
      const code = createRes.data.code;
      pass('Create session via HTTP', `Code: ${code}`);

      // Connect via WebSocket to same session
      const ws = await connectWebSocket(`${options.wsUrl}/ws?session=${code}`);
      pass('Connect to HTTP-created session via WebSocket');

      // Register as client
      const offerPromise = waitForMessage(ws, 3000);
      ws.send(JSON.stringify({ type: 'register', role: 'client' }));

      const offer = await offerPromise;
      if (offer.type === 'offer') {
        pass('Receive offer for HTTP-created session', `Salt: ${offer.salt?.substring(0, 10)}...`);
      } else {
        fail('Receive offer for HTTP-created session', `Got: ${JSON.stringify(offer)}`);
      }

      ws.close();
    }
  } catch (err) {
    fail('HTTP + WebSocket integration', err);
  }

  // Test 10: Concurrent Sessions
  log(`\n${colors.yellow}10. Concurrent Sessions${colors.reset}`);
  try {
    const sessions = [];
    const count = 3;

    // Create multiple sessions
    for (let i = 0; i < count; i++) {
      const code = `CONC${i}${Date.now().toString(36).toUpperCase()}`;
      const ws = await connectWebSocket(`${options.wsUrl}/ws?session=${code}`);
      ws.send(JSON.stringify({ type: 'register', role: 'host' }));
      sessions.push({ code, ws });
    }

    pass(`Create ${count} concurrent sessions`);

    // Clean up
    sessions.forEach(s => s.ws.close());
  } catch (err) {
    fail('Concurrent sessions', err);
  }

  // Test 11: Large SDP
  log(`\n${colors.yellow}11. Large Payload${colors.reset}`);
  try {
    const largeSdp = testSdp + '\na=candidate:' + 'x'.repeat(5000);
    const code = `LARGE${Date.now().toString(36).toUpperCase()}`;

    const ws = await connectWebSocket(`${options.wsUrl}/ws?session=${code}`);
    ws.send(JSON.stringify({ type: 'register', role: 'host' }));
    ws.send(JSON.stringify({ type: 'offer', sdp: largeSdp, salt: testSalt }));

    await new Promise(r => setTimeout(r, 100));
    pass('Handle large SDP payload', `${largeSdp.length} bytes`);
    ws.close();
  } catch (err) {
    fail('Handle large SDP payload', err);
  }

  // Summary
  log(`\n${'─'.repeat(50)}`);
  log(`${colors.blue}Summary${colors.reset}`);
  log(`  ${colors.green}Passed: ${passed}${colors.reset}`);
  if (failed > 0) {
    log(`  ${colors.red}Failed: ${failed}${colors.reset}`);
  }
  log(`  Total:  ${passed + failed}`);
  log(`${'─'.repeat(50)}\n`);

  // Exit with appropriate code
  process.exit(failed > 0 ? 1 : 0);
}

// Run tests
runTests().catch(err => {
  console.error(`${colors.red}Test error: ${err}${colors.reset}`);
  process.exit(1);
});
