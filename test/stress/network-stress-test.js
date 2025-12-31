#!/usr/bin/env node

/**
 * Network Stress Test for Terminal Tunnel
 *
 * Tests WebRTC terminal connection under various network conditions.
 * Uses the proven webrtc-stress-test.js as foundation.
 */

const { chromium } = require('playwright');
const { spawn } = require('child_process');
const http = require('http');
const path = require('path');
const fs = require('fs');

const CONFIG = {
    httpPort: 8888,
    password: 'networkstress123',
    webClientUrl: 'http://localhost:8888',
    ttPath: path.join(__dirname, '../../tt'),
    staticDir: path.join(__dirname, '../../internal/web/static'),
};

// Network conditions (CDP format)
const CONDITIONS = {
    normal: { name: 'Normal', offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 },
    latency_200ms: { name: 'Latency 200ms', offline: false, latency: 200, downloadThroughput: -1, uploadThroughput: -1 },
    latency_500ms: { name: 'Latency 500ms', offline: false, latency: 500, downloadThroughput: -1, uploadThroughput: -1 },
    slow_3g: { name: 'Slow 3G', offline: false, latency: 400, downloadThroughput: 50 * 1024, uploadThroughput: 20 * 1024 },
    fast_3g: { name: 'Fast 3G', offline: false, latency: 150, downloadThroughput: 180 * 1024, uploadThroughput: 75 * 1024 },
};

let results = [];

function log(msg) {
    console.log(`[${new Date().toISOString()}] ${msg}`);
}

function startHttpServer() {
    return new Promise((resolve, reject) => {
        // Use Python's HTTP server for reliable static file serving
        const proc = spawn('python3', ['-m', 'http.server', String(CONFIG.httpPort)], {
            cwd: CONFIG.staticDir,
            stdio: ['ignore', 'pipe', 'pipe'],
        });

        proc.on('error', reject);

        // Wait a moment for server to start
        setTimeout(() => {
            log(`HTTP server on port ${CONFIG.httpPort}`);
            resolve(proc);
        }, 1000);
    });
}

function startTTServer() {
    return new Promise((resolve, reject) => {
        const proc = spawn(CONFIG.ttPath, ['start', '-p', CONFIG.password], {
            env: { ...process.env, TERM: 'xterm-256color' }
        });

        let output = '';
        let resolved = false;

        proc.stdout.on('data', data => {
            output += data.toString();
            const match = output.match(/Code:\s+([A-Z0-9]+)/);
            if (match && !resolved) {
                resolved = true;
                log(`Session code: ${match[1]}`);
                resolve({ process: proc, code: match[1] });
            }
        });

        proc.on('error', reject);
        setTimeout(() => !resolved && reject(new Error('Timeout')), 15000);
    });
}

async function setNetworkCondition(page, condition) {
    const client = await page.context().newCDPSession(page);
    await client.send('Network.enable');
    await client.send('Network.emulateNetworkConditions', {
        offline: condition.offline,
        latency: condition.latency,
        downloadThroughput: condition.downloadThroughput,
        uploadThroughput: condition.uploadThroughput
    });
    log(`Network: ${condition.name}`);
    return client;
}

async function testCondition(browser, sessionCode, condition) {
    log(`\n=== Testing: ${condition.name} ===`);

    const context = await browser.newContext({ bypassCSP: true });
    const page = await context.newPage();

    // Set network condition
    const cdp = await setNetworkCondition(page, condition);

    // Navigate and connect
    await page.goto(`${CONFIG.webClientUrl}/?c=${sessionCode}`);
    await page.waitForTimeout(2000);

    let testResult = {
        condition: condition.name,
        connected: false,
        connectTime: 0,
        ioWorks: false,
        reconnectWorks: false,
        errors: []
    };

    try {
        // Connect
        await page.waitForSelector('.password-input', { timeout: 10000 });
        await page.fill('.password-input', CONFIG.password);
        await page.click('.connect-btn');

        const connectStart = Date.now();

        // Wait for connection
        for (let i = 0; i < 60; i++) {
            await page.waitForTimeout(500);
            const connected = await page.evaluate(() => {
                return window.session && window.session.status === 'connected' &&
                       window.session.dc && window.session.dc.readyState === 'open';
            });
            if (connected) {
                testResult.connected = true;
                testResult.connectTime = Date.now() - connectStart;
                log(`Connected in ${testResult.connectTime}ms`);
                break;
            }
        }

        if (!testResult.connected) {
            log(`Connection FAILED`);
            testResult.errors.push('Connection timeout');
        } else {
            // Test I/O - send command and check for response
            const testId = `TEST_${Date.now()}`;
            await page.evaluate((id) => {
                if (window.session && window.session.dc && window.session.dc.readyState === 'open') {
                    const encoder = new TextEncoder();
                    window.session.dc.send(encoder.encode(`echo "${id}"\n`));
                }
            }, testId);

            await page.waitForTimeout(Math.max(3000, condition.latency * 4));

            const output = await page.evaluate(() => {
                const terminal = document.querySelector('.terminal-container');
                return terminal ? terminal.textContent : '';
            });

            testResult.ioWorks = output.includes(testId);
            log(`I/O test: ${testResult.ioWorks ? 'PASS' : 'FAIL'}`);

            // Test reconnection
            await page.reload();
            await page.waitForTimeout(2000);

            await page.waitForSelector('.password-input', { timeout: 10000 });
            await page.fill('.password-input', CONFIG.password);
            await page.click('.connect-btn');

            for (let i = 0; i < 60; i++) {
                await page.waitForTimeout(500);
                const reconnected = await page.evaluate(() => {
                    return window.session && window.session.status === 'connected' &&
                           window.session.dc && window.session.dc.readyState === 'open';
                });
                if (reconnected) {
                    testResult.reconnectWorks = true;
                    log(`Reconnection: PASS`);
                    break;
                }
            }

            if (!testResult.reconnectWorks) {
                log(`Reconnection: FAIL`);
                testResult.errors.push('Reconnection timeout');
            }
        }
    } catch (err) {
        log(`Error: ${err.message}`);
        testResult.errors.push(err.message);
    }

    await cdp.detach();
    await context.close();

    results.push(testResult);
    return testResult;
}

async function runTests() {
    log('========================================');
    log('  NETWORK STRESS TEST - Terminal Tunnel');
    log('========================================\n');

    let httpServer, ttProcess, browser;

    try {
        httpServer = await startHttpServer();
        const tt = await startTTServer();
        ttProcess = tt.process;

        await new Promise(r => setTimeout(r, 2000));

        browser = await chromium.launch({ headless: true });

        // Test each network condition
        for (const key of Object.keys(CONDITIONS)) {
            await testCondition(browser, tt.code, CONDITIONS[key]);
        }

    } catch (err) {
        log(`Fatal: ${err.message}`);
    } finally {
        if (browser) await browser.close();
        if (ttProcess) ttProcess.kill('SIGTERM');
        if (httpServer) httpServer.kill('SIGTERM');
    }

    // Print results
    console.log('\n========================================');
    console.log('           TEST RESULTS');
    console.log('========================================\n');

    let passed = 0, failed = 0;

    for (const r of results) {
        const status = r.connected && r.ioWorks && r.reconnectWorks ? '✓ PASS' : '✗ FAIL';
        if (r.connected && r.ioWorks && r.reconnectWorks) passed++; else failed++;

        console.log(`${status}: ${r.condition}`);
        console.log(`  Connected: ${r.connected} (${r.connectTime}ms)`);
        console.log(`  I/O Works: ${r.ioWorks}`);
        console.log(`  Reconnect: ${r.reconnectWorks}`);
        if (r.errors.length) console.log(`  Errors: ${r.errors.join(', ')}`);
        console.log('');
    }

    console.log('----------------------------------------');
    console.log(`Passed: ${passed} / ${passed + failed}`);
    console.log(`Success Rate: ${((passed / (passed + failed)) * 100).toFixed(1)}%`);
    console.log('----------------------------------------');

    process.exit(failed > 0 ? 1 : 0);
}

runTests();
