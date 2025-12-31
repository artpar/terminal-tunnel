#!/usr/bin/env node
/**
 * WebRTC Connection Stress Test using Playwright
 * Tests connection reliability by simulating real user scenarios
 */

const { chromium } = require('playwright');
const { spawn } = require('child_process');
const path = require('path');

const CONFIG = {
    password: 'stresstestpassword123',
    webClientUrl: 'http://localhost:8888',
    reconnectCycles: 5,
};

class StressTest {
    constructor() {
        this.serverProcess = null;
        this.httpServer = null;
        this.browser = null;
        this.page = null;
        this.sessionCode = null;
        this.results = {
            connectAttempts: 0,
            connectSuccesses: 0,
            connectFailures: 0,
            disconnects: 0,
            reconnects: 0,
            errors: [],
            latencies: [],
        };
    }

    log(msg) {
        console.log(`[${new Date().toISOString()}] ${msg}`);
    }

    async startHttpServer() {
        this.log('Starting HTTP server for web client...');
        const staticDir = path.join(__dirname, '../../internal/web/static');
        this.httpServer = spawn('python3', ['-m', 'http.server', '8888'], {
            cwd: staticDir,
            stdio: ['ignore', 'pipe', 'pipe'],
        });
        await new Promise(r => setTimeout(r, 1000));
        this.log('HTTP server started on port 8888');
    }

    async startTerminalTunnel() {
        this.log('Starting terminal-tunnel server...');
        const ttPath = path.join(__dirname, '../../tt');
        this.log(`Using tt binary at: ${ttPath}`);

        return new Promise((resolve, reject) => {
            this.serverProcess = spawn(ttPath, ['start', '-p', CONFIG.password], {
                stdio: ['pipe', 'pipe', 'pipe'],
            });

            let output = '';
            const timeout = setTimeout(() => {
                this.log(`Server output so far: ${output}`);
                reject(new Error('Timeout waiting for session code'));
            }, 30000);

            this.serverProcess.stdout.on('data', (data) => {
                const chunk = data.toString();
                output += chunk;
                const match = output.match(/Code:\s+([A-Z0-9]{8})/);
                if (match && !this.sessionCode) {
                    this.sessionCode = match[1];
                    clearTimeout(timeout);
                    this.log(`Session code: ${this.sessionCode}`);
                    resolve();
                }
            });

            this.serverProcess.stderr.on('data', (data) => {
                const msg = data.toString().trim();
                if (msg) this.log(`Server: ${msg}`);
            });

            this.serverProcess.on('error', reject);
            this.serverProcess.on('exit', (code, signal) => {
                if (code !== 0 && code !== null) {
                    this.log(`Server exited: code=${code}, signal=${signal}`);
                }
            });
        });
    }

    async launchBrowser() {
        this.log('Launching browser...');
        this.browser = await chromium.launch({
            headless: true,
            args: ['--disable-web-security'],  // Bypass CSP for WASM
        });
        this.log('Browser launched');

        const context = await this.browser.newContext({
            bypassCSP: true,  // Bypass CSP
        });
        this.page = await context.newPage();
        this.log('Page created');

        // Capture console logs
        this.page.on('console', msg => {
            const text = msg.text();
            if (text.includes('[PC]') || text.includes('[DC]') || text.includes('[ICE]') || text.includes('Reconnect')) {
                this.log(`Browser: ${text}`);
            }
        });

        this.page.on('pageerror', err => {
            this.results.errors.push(err.message);
            this.log(`Page error: ${err.message}`);
        });
    }

    async connect() {
        this.results.connectAttempts++;
        const url = `${CONFIG.webClientUrl}/?c=${this.sessionCode}`;
        this.log(`Connecting to ${url}`);

        await this.page.goto(url);
        await this.page.waitForTimeout(2000);

        // Wait for password input and enter password
        try {
            await this.page.waitForSelector('.password-input', { timeout: 10000 });
            await this.page.fill('.password-input', CONFIG.password);
            this.log('Password entered');

            await this.page.click('.connect-btn');
            this.log('Connect button clicked');
        } catch (err) {
            this.log(`Input error: ${err.message}`);
            this.results.errors.push(err.message);
            return false;
        }

        // Wait for connection
        const connected = await this.waitForConnection(25000);
        if (connected) {
            this.results.connectSuccesses++;
            this.log('Connected successfully');
            return true;
        } else {
            this.results.connectFailures++;
            this.log('Connection failed');
            return false;
        }
    }

    async waitForConnection(timeout) {
        const start = Date.now();
        while (Date.now() - start < timeout) {
            const hasTerminal = await this.page.evaluate(() => {
                const terminal = document.querySelector('.terminal-container .xterm');
                return !!terminal;
            });

            if (hasTerminal) {
                this.results.latencies.push(Date.now() - start);
                return true;
            }

            // Check for error state
            const hasError = await this.page.evaluate(() => {
                const status = document.querySelector('.status-text');
                return status?.textContent?.includes('error') || status?.textContent?.includes('failed');
            });

            if (hasError) return false;

            await this.page.waitForTimeout(200);
        }
        return false;
    }

    // Test 1: Basic connect/disconnect cycle
    async testReconnectCycles() {
        this.log('\n=== Test: Reconnect Cycles ===');

        for (let i = 0; i < CONFIG.reconnectCycles; i++) {
            this.log(`Cycle ${i + 1}/${CONFIG.reconnectCycles}`);

            // Refresh the page to trigger disconnect/reconnect
            await this.page.reload();
            this.results.disconnects++;
            await this.page.waitForTimeout(2000);

            // Re-enter password and connect
            try {
                await this.page.waitForSelector('.password-input', { timeout: 10000 });
                await this.page.fill('.password-input', CONFIG.password);
                await this.page.click('.connect-btn');

                const connected = await this.waitForConnection(25000);
                if (connected) {
                    this.results.reconnects++;
                    this.log(`Reconnect ${i + 1} successful`);
                } else {
                    this.log(`Reconnect ${i + 1} failed`);
                }
            } catch (err) {
                this.log(`Reconnect ${i + 1} error: ${err.message}`);
                this.results.errors.push(err.message);
            }

            await this.page.waitForTimeout(2000);
        }
    }

    // Test 2: Rapid refresh stress test
    async testRapidRefresh() {
        this.log('\n=== Test: Rapid Refresh ===');

        for (let i = 0; i < 3; i++) {
            this.log(`Rapid refresh batch ${i + 1}/3`);

            // Rapid refreshes
            await this.page.reload();
            await this.page.waitForTimeout(300);
            await this.page.reload();
            await this.page.waitForTimeout(300);
            await this.page.reload();

            this.results.disconnects += 3;
            await this.page.waitForTimeout(2000);

            // Now wait for stable connection
            try {
                await this.page.waitForSelector('.password-input', { timeout: 10000 });
                await this.page.fill('.password-input', CONFIG.password);
                await this.page.click('.connect-btn');

                const connected = await this.waitForConnection(25000);
                if (connected) {
                    this.results.reconnects++;
                    this.log('Recovered after rapid refresh');
                } else {
                    this.log('Failed to recover after rapid refresh');
                }
            } catch (err) {
                this.log(`Failed to recover: ${err.message}`);
                this.results.errors.push(err.message);
            }

            await this.page.waitForTimeout(3000);
        }
    }

    // Test 3: Check connection state after idle
    async testIdleConnection() {
        this.log('\n=== Test: Idle Connection ===');

        // First ensure we're connected
        const hasTerminal = await this.page.evaluate(() => {
            const terminal = document.querySelector('.terminal-container .xterm');
            return !!terminal;
        });

        if (!hasTerminal) {
            this.log('Not connected, skipping idle test');
            return;
        }

        this.log('Waiting 30 seconds to test idle connection...');
        for (let i = 0; i < 6; i++) {
            await this.page.waitForTimeout(5000);

            // Check if still connected
            const state = await this.page.evaluate(() => {
                if (window.manager) {
                    const session = window.manager.getActiveSession?.();
                    return {
                        status: session?.status,
                        latency: session?.latency,
                    };
                }
                return null;
            });
            this.log(`After ${(i + 1) * 5}s idle: ${JSON.stringify(state)}`);
        }
    }

    async runAllTests() {
        try {
            await this.startHttpServer();
            await this.startTerminalTunnel();
            await this.launchBrowser();

            // Initial connection
            const connected = await this.connect();
            if (!connected) {
                this.log('Initial connection failed, aborting tests');
                return;
            }

            // Run tests
            await this.testReconnectCycles();
            await this.testRapidRefresh();
            await this.testIdleConnection();

        } catch (err) {
            this.log(`Test error: ${err.message}`);
            console.error(err.stack);
            this.results.errors.push(err.message);
        } finally {
            await this.cleanup();
            this.printResults();
        }
    }

    async cleanup() {
        this.log('Cleaning up...');
        if (this.browser) await this.browser.close();
        if (this.serverProcess) this.serverProcess.kill();
        if (this.httpServer) this.httpServer.kill();
    }

    printResults() {
        console.log('\n========== TEST RESULTS ==========');
        console.log(`Connect Attempts: ${this.results.connectAttempts}`);
        console.log(`Connect Successes: ${this.results.connectSuccesses}`);
        console.log(`Connect Failures: ${this.results.connectFailures}`);
        console.log(`Disconnects: ${this.results.disconnects}`);
        console.log(`Successful Reconnects: ${this.results.reconnects}`);
        console.log(`Errors: ${this.results.errors.length}`);
        if (this.results.errors.length > 0) {
            console.log('Error details:');
            this.results.errors.slice(0, 5).forEach((e, i) => console.log(`  ${i + 1}. ${e}`));
        }
        if (this.results.latencies.length > 0) {
            const avg = this.results.latencies.reduce((a, b) => a + b, 0) / this.results.latencies.length;
            const max = Math.max(...this.results.latencies);
            console.log(`Avg Connection Latency: ${avg.toFixed(0)}ms (max: ${max}ms)`);
        }
        console.log('==================================\n');

        // Exit with error code if failures
        const successRate = this.results.connectAttempts > 0
            ? this.results.connectSuccesses / this.results.connectAttempts
            : 0;
        const reconnectRate = this.results.disconnects > 0
            ? this.results.reconnects / this.results.disconnects
            : 1;

        console.log(`Success Rate: ${(successRate * 100).toFixed(1)}%`);
        console.log(`Reconnect Rate: ${(reconnectRate * 100).toFixed(1)}%`);

        const success = successRate >= 0.8 && reconnectRate >= 0.7;
        process.exit(success ? 0 : 1);
    }
}

// Run tests
const test = new StressTest();
test.runAllTests();
