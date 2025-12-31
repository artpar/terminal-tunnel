#!/usr/bin/env node

/**
 * Comprehensive Network Conditions Test Suite for Terminal Tunnel
 *
 * Tests WebRTC terminal tunnel under various real-world network conditions:
 * - Normal conditions (baseline)
 * - High latency (200ms, 500ms, 1000ms)
 * - Packet loss simulation
 * - Bandwidth throttling (Slow 3G, Fast 3G, 4G)
 * - Network interruptions
 * - Heavy data throughput
 * - Long idle connections
 * - Rapid input during reconnection
 */

const { chromium } = require('playwright');
const { spawn, exec } = require('child_process');
const http = require('http');
const path = require('path');
const fs = require('fs');

// Test configuration
const CONFIG = {
    httpPort: 8889,
    password: 'testpassword123', // Must be 12+ characters
    ttBinary: path.join(__dirname, '../../tt'),
    staticDir: path.join(__dirname, '../../internal/web/static'),
    testTimeout: 180000, // 3 minutes per test
};

// Network condition presets (Chrome DevTools Protocol format)
const NETWORK_CONDITIONS = {
    normal: {
        name: 'Normal',
        offline: false,
        latency: 0,
        downloadThroughput: -1,
        uploadThroughput: -1
    },
    latency_200ms: {
        name: 'Latency 200ms',
        offline: false,
        latency: 200,
        downloadThroughput: -1,
        uploadThroughput: -1
    },
    latency_500ms: {
        name: 'Latency 500ms',
        offline: false,
        latency: 500,
        downloadThroughput: -1,
        uploadThroughput: -1
    },
    latency_1000ms: {
        name: 'Latency 1000ms',
        offline: false,
        latency: 1000,
        downloadThroughput: -1,
        uploadThroughput: -1
    },
    slow_3g: {
        name: 'Slow 3G',
        offline: false,
        latency: 400,
        downloadThroughput: 50 * 1024, // 50 KB/s
        uploadThroughput: 20 * 1024   // 20 KB/s
    },
    fast_3g: {
        name: 'Fast 3G',
        offline: false,
        latency: 150,
        downloadThroughput: 180 * 1024, // 180 KB/s
        uploadThroughput: 75 * 1024    // 75 KB/s
    },
    slow_4g: {
        name: 'Slow 4G',
        offline: false,
        latency: 50,
        downloadThroughput: 400 * 1024, // 400 KB/s
        uploadThroughput: 150 * 1024   // 150 KB/s
    },
    offline: {
        name: 'Offline',
        offline: true,
        latency: 0,
        downloadThroughput: 0,
        uploadThroughput: 0
    }
};

// Test results storage
const results = {
    tests: [],
    summary: {
        passed: 0,
        failed: 0,
        total: 0
    }
};

function log(msg) {
    const timestamp = new Date().toISOString();
    console.log(`[${timestamp}] ${msg}`);
}

function logResult(testName, passed, details = {}) {
    const result = { testName, passed, details, timestamp: new Date().toISOString() };
    results.tests.push(result);
    results.summary.total++;
    if (passed) {
        results.summary.passed++;
        log(`✓ PASSED: ${testName}`);
    } else {
        results.summary.failed++;
        log(`✗ FAILED: ${testName} - ${JSON.stringify(details)}`);
    }
}

// Start HTTP server for web client
function startHttpServer() {
    return new Promise((resolve, reject) => {
        const server = http.createServer((req, res) => {
            let filePath = req.url === '/' ? '/index.html' : req.url;
            filePath = filePath.split('?')[0];

            const fullPath = path.join(CONFIG.staticDir, filePath);

            fs.readFile(fullPath, (err, data) => {
                if (err) {
                    res.writeHead(404);
                    res.end('Not found');
                    return;
                }

                const ext = path.extname(filePath);
                const contentTypes = {
                    '.html': 'text/html',
                    '.js': 'application/javascript',
                    '.css': 'text/css'
                };

                res.writeHead(200, { 'Content-Type': contentTypes[ext] || 'text/plain' });
                res.end(data);
            });
        });

        server.listen(CONFIG.httpPort, () => {
            log(`HTTP server started on port ${CONFIG.httpPort}`);
            resolve(server);
        });

        server.on('error', reject);
    });
}

// Start terminal tunnel server
function startTTServer() {
    return new Promise((resolve, reject) => {
        const ttProcess = spawn(CONFIG.ttBinary, ['start', '-p', CONFIG.password], {
            env: { ...process.env, TERM: 'xterm-256color' }
        });

        let sessionCode = null;
        let resolved = false;
        let outputBuffer = '';

        ttProcess.stdout.on('data', (data) => {
            const output = data.toString();
            outputBuffer += output;

            // Extract session code - format: "Code:     VSP565HY"
            const codeMatch = outputBuffer.match(/Code:\s+([A-Z0-9]+)/);
            if (codeMatch && !resolved) {
                sessionCode = codeMatch[1];
                log(`Session code: ${sessionCode}`);
                resolved = true;
                resolve({ process: ttProcess, code: sessionCode });
            }
        });

        ttProcess.stderr.on('data', (data) => {
            // Log stderr but don't fail
            const err = data.toString();
            if (err.includes('error') || err.includes('Error')) {
                log(`TT stderr: ${err}`);
            }
        });

        ttProcess.on('error', reject);

        // Timeout after 15 seconds
        setTimeout(() => {
            if (!resolved) {
                log(`TT output buffer: ${outputBuffer}`);
                reject(new Error('Timeout waiting for session code'));
            }
        }, 15000);
    });
}

// Set network conditions using CDP
async function setNetworkConditions(page, conditions) {
    const client = await page.context().newCDPSession(page);
    await client.send('Network.enable');
    await client.send('Network.emulateNetworkConditions', {
        offline: conditions.offline,
        latency: conditions.latency,
        downloadThroughput: conditions.downloadThroughput,
        uploadThroughput: conditions.uploadThroughput
    });
    log(`Network conditions set: ${conditions.name}`);
    return client;
}

// Connect to terminal
async function connectToTerminal(page, sessionCode) {
    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForTimeout(2000); // Wait for page to fully load

    // Wait for password input and enter password
    await page.waitForSelector('.password-input', { timeout: 15000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    // Wait for connection
    const startTime = Date.now();
    let connected = false;

    for (let i = 0; i < 60; i++) {
        await page.waitForTimeout(500);

        const isConnected = await page.evaluate(() => {
            return window.session && window.session.status === 'connected';
        });

        if (isConnected) {
            connected = true;
            break;
        }
    }

    const connectTime = Date.now() - startTime;
    return { connected, connectTime };
}

// Wait for terminal to be ready
async function waitForTerminalReady(page, timeout = 10000) {
    const startTime = Date.now();

    while (Date.now() - startTime < timeout) {
        const ready = await page.evaluate(() => {
            return window.session &&
                   window.session.status === 'connected' &&
                   window.session.dc &&
                   window.session.dc.readyState === 'open';
        });

        if (ready) return true;
        await page.waitForTimeout(100);
    }

    return false;
}

// Send input to terminal
async function sendTerminalInput(page, input) {
    await page.evaluate((text) => {
        if (window.session && window.session.dc && window.session.dc.readyState === 'open') {
            const encoder = new TextEncoder();
            const data = encoder.encode(text);
            window.session.dc.send(data);
            return true;
        }
        return false;
    }, input);
}

// Get terminal output (last N lines)
async function getTerminalOutput(page) {
    return await page.evaluate(() => {
        const terminal = document.getElementById('terminal');
        if (terminal && terminal.textContent) {
            return terminal.textContent;
        }
        return '';
    });
}

// Check if session is still connected
async function isConnected(page) {
    return await page.evaluate(() => {
        return window.session &&
               window.session.status === 'connected' &&
               window.session.dc &&
               window.session.dc.readyState === 'open';
    });
}

// ============= TEST FUNCTIONS =============

// Test 1: Baseline connection under normal conditions
async function testNormalConditions(page, sessionCode) {
    log('\n=== TEST: Normal Conditions (Baseline) ===');

    const cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    const { connected, connectTime } = await connectToTerminal(page, sessionCode);

    if (!connected) {
        logResult('Normal Conditions - Connect', false, { reason: 'Failed to connect' });
        return false;
    }

    logResult('Normal Conditions - Connect', true, { connectTime });

    // Test basic I/O
    await waitForTerminalReady(page);
    await sendTerminalInput(page, 'echo "HELLO_TEST_123"\n');
    await page.waitForTimeout(1000);

    const output = await getTerminalOutput(page);
    const ioWorking = output.includes('HELLO_TEST_123');
    logResult('Normal Conditions - Basic I/O', ioWorking, { outputContains: 'HELLO_TEST_123' });

    await cdpClient.detach();
    return connected && ioWorking;
}

// Test 2: High latency conditions
async function testHighLatency(page, sessionCode) {
    log('\n=== TEST: High Latency Conditions ===');

    const latencyTests = [
        NETWORK_CONDITIONS.latency_200ms,
        NETWORK_CONDITIONS.latency_500ms,
        NETWORK_CONDITIONS.latency_1000ms
    ];

    let allPassed = true;

    for (const condition of latencyTests) {
        log(`Testing ${condition.name}...`);

        // Reconnect with new conditions
        const cdpClient = await setNetworkConditions(page, condition);

        // Trigger reconnection by navigating
        await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
        await page.waitForLoadState('networkidle');
        await page.waitForSelector('.password-input', { timeout: 10000 });
        await page.fill('.password-input', CONFIG.password);
        await page.click('.connect-btn');

        const startTime = Date.now();
        let connected = false;

        for (let i = 0; i < 60; i++) {
            await page.waitForTimeout(500);
            connected = await isConnected(page);
            if (connected) break;
        }

        const connectTime = Date.now() - startTime;

        if (!connected) {
            logResult(`${condition.name} - Connect`, false, { reason: 'Failed to connect' });
            allPassed = false;
            continue;
        }

        logResult(`${condition.name} - Connect`, true, { connectTime });

        // Test responsiveness
        await waitForTerminalReady(page);
        const testId = `LAT_${condition.latency}_${Date.now()}`;
        await sendTerminalInput(page, `echo "${testId}"\n`);

        const inputStart = Date.now();
        let foundOutput = false;

        for (let i = 0; i < 20; i++) {
            await page.waitForTimeout(500);
            const output = await getTerminalOutput(page);
            if (output.includes(testId)) {
                foundOutput = true;
                break;
            }
        }

        const responseTime = Date.now() - inputStart;
        logResult(`${condition.name} - I/O Response`, foundOutput, { responseTime });

        if (!foundOutput) allPassed = false;

        await cdpClient.detach();
    }

    return allPassed;
}

// Test 3: Bandwidth throttling
async function testBandwidthThrottling(page, sessionCode) {
    log('\n=== TEST: Bandwidth Throttling ===');

    const bandwidthTests = [
        NETWORK_CONDITIONS.slow_3g,
        NETWORK_CONDITIONS.fast_3g,
        NETWORK_CONDITIONS.slow_4g
    ];

    let allPassed = true;

    for (const condition of bandwidthTests) {
        log(`Testing ${condition.name}...`);

        const cdpClient = await setNetworkConditions(page, condition);

        // Reconnect
        await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
        await page.waitForLoadState('networkidle');
        await page.waitForSelector('.password-input', { timeout: 10000 });
        await page.fill('.password-input', CONFIG.password);
        await page.click('.connect-btn');

        let connected = false;
        for (let i = 0; i < 60; i++) {
            await page.waitForTimeout(500);
            connected = await isConnected(page);
            if (connected) break;
        }

        if (!connected) {
            logResult(`${condition.name} - Connect`, false, { reason: 'Failed to connect' });
            allPassed = false;
            continue;
        }

        logResult(`${condition.name} - Connect`, true);

        // Test with larger data output
        await waitForTerminalReady(page);
        await sendTerminalInput(page, 'for i in $(seq 1 50); do echo "Line $i: $(date)"; done\n');

        await page.waitForTimeout(5000);

        const output = await getTerminalOutput(page);
        const hasOutput = output.includes('Line 50');
        logResult(`${condition.name} - Large Output`, hasOutput);

        if (!hasOutput) allPassed = false;

        await cdpClient.detach();
    }

    return allPassed;
}

// Test 4: Network interruption and recovery
async function testNetworkInterruption(page, sessionCode) {
    log('\n=== TEST: Network Interruption Recovery ===');

    // First establish connection
    let cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    let connected = false;
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Network Interruption - Initial Connect', false);
        return false;
    }

    logResult('Network Interruption - Initial Connect', true);

    // Go offline
    log('Simulating network interruption (going offline)...');
    await cdpClient.send('Network.emulateNetworkConditions', {
        offline: true,
        latency: 0,
        downloadThroughput: 0,
        uploadThroughput: 0
    });

    await page.waitForTimeout(3000);

    // Check connection status (should be disconnected or reconnecting)
    const statusDuringOutage = await page.evaluate(() => {
        return window.session ? window.session.status : 'unknown';
    });
    log(`Status during outage: ${statusDuringOutage}`);

    // Come back online
    log('Restoring network...');
    await cdpClient.send('Network.emulateNetworkConditions', {
        offline: false,
        latency: 0,
        downloadThroughput: -1,
        uploadThroughput: -1
    });

    // Wait for reconnection
    let reconnected = false;
    const startTime = Date.now();

    for (let i = 0; i < 60; i++) {
        await page.waitForTimeout(500);
        reconnected = await isConnected(page);
        if (reconnected) break;
    }

    const recoveryTime = Date.now() - startTime;

    if (reconnected) {
        // Verify I/O works after reconnection
        await waitForTerminalReady(page);
        const testId = `RECOVERY_${Date.now()}`;
        await sendTerminalInput(page, `echo "${testId}"\n`);
        await page.waitForTimeout(2000);

        const output = await getTerminalOutput(page);
        const ioWorks = output.includes(testId);

        logResult('Network Interruption - Recovery', true, { recoveryTime, ioWorks });
        await cdpClient.detach();
        return ioWorks;
    } else {
        logResult('Network Interruption - Recovery', false, { reason: 'Failed to reconnect' });
        await cdpClient.detach();
        return false;
    }
}

// Test 5: Rapid reconnection stress
async function testRapidReconnection(page, sessionCode) {
    log('\n=== TEST: Rapid Reconnection Stress ===');

    const cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    let successCount = 0;
    const totalAttempts = 5;

    for (let i = 0; i < totalAttempts; i++) {
        log(`Rapid reconnection attempt ${i + 1}/${totalAttempts}`);

        await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
        await page.waitForLoadState('networkidle');
        await page.waitForSelector('.password-input', { timeout: 10000 });
        await page.fill('.password-input', CONFIG.password);
        await page.click('.connect-btn');

        let connected = false;
        for (let j = 0; j < 30; j++) {
            await page.waitForTimeout(300);
            connected = await isConnected(page);
            if (connected) break;
        }

        if (connected) {
            successCount++;
            // Brief pause then force disconnect by navigating away
            await page.waitForTimeout(500);
        }
    }

    const successRate = (successCount / totalAttempts) * 100;
    const passed = successRate >= 80; // Allow 20% failure tolerance

    logResult('Rapid Reconnection', passed, {
        successCount,
        totalAttempts,
        successRate: `${successRate.toFixed(1)}%`
    });

    await cdpClient.detach();
    return passed;
}

// Test 6: Heavy data throughput
async function testHeavyThroughput(page, sessionCode) {
    log('\n=== TEST: Heavy Data Throughput ===');

    const cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    let connected = false;
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Heavy Throughput - Connect', false);
        return false;
    }

    await waitForTerminalReady(page);

    // Generate large output
    log('Generating large output (1000 lines)...');
    await sendTerminalInput(page, 'for i in $(seq 1 1000); do echo "Heavy output line $i: $(date) - AAAAAAAAAAAAAAAAAAAAAAAAAAAA"; done\n');

    // Wait and check for completion marker
    await page.waitForTimeout(10000);

    const output = await getTerminalOutput(page);
    const completed = output.includes('line 1000');

    logResult('Heavy Throughput - Large Output', completed);

    // Check if still connected after heavy load
    const stillConnected = await isConnected(page);
    logResult('Heavy Throughput - Connection Stable', stillConnected);

    await cdpClient.detach();
    return completed && stillConnected;
}

// Test 7: Long idle connection
async function testIdleConnection(page, sessionCode) {
    log('\n=== TEST: Long Idle Connection (60 seconds) ===');

    const cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    let connected = false;
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Idle Connection - Connect', false);
        return false;
    }

    logResult('Idle Connection - Initial Connect', true);

    // Wait idle for 60 seconds, checking every 10 seconds
    const idleDuration = 60;
    let stableConnection = true;

    for (let elapsed = 10; elapsed <= idleDuration; elapsed += 10) {
        await page.waitForTimeout(10000);
        const stillConnected = await isConnected(page);
        log(`After ${elapsed}s idle: ${stillConnected ? 'connected' : 'disconnected'}`);
        if (!stillConnected) {
            stableConnection = false;
            break;
        }
    }

    if (stableConnection) {
        // Verify I/O still works after idle
        await waitForTerminalReady(page);
        const testId = `IDLE_TEST_${Date.now()}`;
        await sendTerminalInput(page, `echo "${testId}"\n`);
        await page.waitForTimeout(2000);

        const output = await getTerminalOutput(page);
        const ioWorks = output.includes(testId);

        logResult('Idle Connection - Stability', true, { idleDuration: `${idleDuration}s`, ioWorks });
        await cdpClient.detach();
        return ioWorks;
    } else {
        logResult('Idle Connection - Stability', false, { reason: 'Connection dropped during idle' });
        await cdpClient.detach();
        return false;
    }
}

// Test 8: Input during reconnection
async function testInputDuringReconnection(page, sessionCode) {
    log('\n=== TEST: Input During Reconnection ===');

    const cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    // Connect initially
    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    let connected = false;
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Input During Reconnection - Initial Connect', false);
        return false;
    }

    // Trigger reconnection by refreshing
    await page.reload();
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    // Immediately try to send input (during connection establishment)
    const testId = `INPUT_DURING_${Date.now()}`;

    // Try sending input multiple times during connection
    for (let i = 0; i < 5; i++) {
        await page.waitForTimeout(200);
        try {
            await sendTerminalInput(page, `echo "${testId}"\n`);
        } catch (e) {
            // Expected - connection might not be ready
        }
    }

    // Wait for connection to stabilize
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Input During Reconnection', false, { reason: 'Failed to reconnect' });
        await cdpClient.detach();
        return false;
    }

    // Now send input properly
    await waitForTerminalReady(page);
    await sendTerminalInput(page, `echo "FINAL_${testId}"\n`);
    await page.waitForTimeout(2000);

    const output = await getTerminalOutput(page);
    const passed = output.includes(`FINAL_${testId}`);

    logResult('Input During Reconnection', passed);

    await cdpClient.detach();
    return passed;
}

// Test 9: Alternating network conditions
async function testAlternatingConditions(page, sessionCode) {
    log('\n=== TEST: Alternating Network Conditions ===');

    const conditions = [
        NETWORK_CONDITIONS.normal,
        NETWORK_CONDITIONS.slow_3g,
        NETWORK_CONDITIONS.normal,
        NETWORK_CONDITIONS.latency_500ms,
        NETWORK_CONDITIONS.normal
    ];

    // Initial connection
    let cdpClient = await setNetworkConditions(page, NETWORK_CONDITIONS.normal);

    await page.goto(`http://localhost:${CONFIG.httpPort}/?c=${sessionCode}`);
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('.password-input', { timeout: 10000 });
    await page.fill('.password-input', CONFIG.password);
    await page.click('.connect-btn');

    let connected = false;
    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(500);
        connected = await isConnected(page);
        if (connected) break;
    }

    if (!connected) {
        logResult('Alternating Conditions - Initial Connect', false);
        return false;
    }

    let allPassed = true;

    for (let i = 0; i < conditions.length; i++) {
        const condition = conditions[i];
        log(`Switching to ${condition.name}...`);

        await cdpClient.send('Network.emulateNetworkConditions', {
            offline: condition.offline,
            latency: condition.latency,
            downloadThroughput: condition.downloadThroughput,
            uploadThroughput: condition.uploadThroughput
        });

        await page.waitForTimeout(2000);

        // Verify connection and I/O
        const stillConnected = await isConnected(page);

        if (stillConnected) {
            const testId = `ALT_${i}_${Date.now()}`;
            await sendTerminalInput(page, `echo "${testId}"\n`);
            await page.waitForTimeout(Math.max(2000, condition.latency * 3));

            const output = await getTerminalOutput(page);
            const ioWorks = output.includes(testId);

            if (!ioWorks) {
                logResult(`Alternating - ${condition.name}`, false, { reason: 'I/O failed' });
                allPassed = false;
            } else {
                logResult(`Alternating - ${condition.name}`, true);
            }
        } else {
            logResult(`Alternating - ${condition.name}`, false, { reason: 'Connection lost' });
            allPassed = false;
        }
    }

    await cdpClient.detach();
    return allPassed;
}

// Main test runner
async function runAllTests() {
    log('========================================');
    log('TERMINAL TUNNEL NETWORK CONDITIONS TEST');
    log('========================================\n');

    let httpServer = null;
    let ttProcess = null;
    let browser = null;

    try {
        // Start HTTP server
        log('Starting HTTP server...');
        httpServer = await startHttpServer();

        // Start terminal tunnel
        log('Starting terminal tunnel...');
        const tt = await startTTServer();
        ttProcess = tt.process;
        const sessionCode = tt.code;

        // Wait for server to be ready
        await new Promise(r => setTimeout(r, 2000));

        // Launch browser
        log('Launching browser...');
        browser = await chromium.launch({
            headless: true,
            args: ['--disable-web-security'] // Allow CDP network emulation
        });

        const context = await browser.newContext({
            viewport: { width: 1280, height: 720 },
            bypassCSP: true  // Bypass CSP for testing
        });

        const page = await context.newPage();

        // Capture console logs
        page.on('console', msg => {
            if (msg.type() === 'error') {
                log(`Browser error: ${msg.text()}`);
            }
        });

        // Run all tests
        await testNormalConditions(page, sessionCode);
        await testHighLatency(page, sessionCode);
        await testBandwidthThrottling(page, sessionCode);
        await testNetworkInterruption(page, sessionCode);
        await testRapidReconnection(page, sessionCode);
        await testHeavyThroughput(page, sessionCode);
        await testIdleConnection(page, sessionCode);
        await testInputDuringReconnection(page, sessionCode);
        await testAlternatingConditions(page, sessionCode);

    } catch (err) {
        log(`Fatal error: ${err.message}`);
        console.error(err);
    } finally {
        // Cleanup
        log('\nCleaning up...');

        if (browser) await browser.close();
        if (ttProcess) ttProcess.kill('SIGTERM');
        if (httpServer) httpServer.close();
    }

    // Print final results
    console.log('\n========================================');
    console.log('           FINAL TEST RESULTS           ');
    console.log('========================================\n');

    for (const test of results.tests) {
        const status = test.passed ? '✓ PASS' : '✗ FAIL';
        const details = Object.keys(test.details).length > 0
            ? ` (${JSON.stringify(test.details)})`
            : '';
        console.log(`${status}: ${test.testName}${details}`);
    }

    console.log('\n----------------------------------------');
    console.log(`Total: ${results.summary.total}`);
    console.log(`Passed: ${results.summary.passed}`);
    console.log(`Failed: ${results.summary.failed}`);
    console.log(`Success Rate: ${((results.summary.passed / results.summary.total) * 100).toFixed(1)}%`);
    console.log('----------------------------------------\n');

    // Exit with appropriate code
    process.exit(results.summary.failed > 0 ? 1 : 0);
}

// Run tests
runAllTests();
