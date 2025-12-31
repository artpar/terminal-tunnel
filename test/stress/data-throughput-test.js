#!/usr/bin/env node
/**
 * Data Throughput Test for Terminal Tunnel
 * Tests terminal I/O under various data load conditions
 */

const { chromium } = require('playwright');
const { spawn } = require('child_process');
const path = require('path');

const CONFIG = {
    password: 'throughputtest123',
    webClientUrl: 'http://localhost:8888',
};

class ThroughputTest {
    constructor() {
        this.serverProcess = null;
        this.httpServer = null;
        this.browser = null;
        this.page = null;
        this.sessionCode = null;
    }

    log(msg) {
        console.log(`[${new Date().toISOString()}] ${msg}`);
    }

    async startHttpServer() {
        const staticDir = path.join(__dirname, '../../internal/web/static');
        this.httpServer = spawn('python3', ['-m', 'http.server', '8888'], {
            cwd: staticDir,
            stdio: ['ignore', 'pipe', 'pipe'],
        });
        await new Promise(r => setTimeout(r, 1000));
        this.log('HTTP server started on port 8888');
    }

    async startTerminalTunnel() {
        const ttPath = path.join(__dirname, '../../tt');

        return new Promise((resolve, reject) => {
            this.serverProcess = spawn(ttPath, ['start', '-p', CONFIG.password], {
                stdio: ['pipe', 'pipe', 'pipe'],
            });

            let output = '';
            const timeout = setTimeout(() => reject(new Error('Timeout')), 30000);

            this.serverProcess.stdout.on('data', (data) => {
                output += data.toString();
                const match = output.match(/Code:\s+([A-Z0-9]{8})/);
                if (match && !this.sessionCode) {
                    this.sessionCode = match[1];
                    clearTimeout(timeout);
                    this.log(`Session code: ${this.sessionCode}`);
                    resolve();
                }
            });

            this.serverProcess.on('error', reject);
        });
    }

    async launchBrowser() {
        this.browser = await chromium.launch({ headless: true });
        const context = await this.browser.newContext({ bypassCSP: true });
        this.page = await context.newPage();

        this.page.on('console', msg => {
            if (msg.type() === 'error') {
                this.log(`Browser Error: ${msg.text()}`);
            }
        });
    }

    async connect() {
        await this.page.goto(`${CONFIG.webClientUrl}/?c=${this.sessionCode}`);
        await this.page.waitForTimeout(2000);

        await this.page.waitForSelector('.password-input', { timeout: 10000 });
        await this.page.fill('.password-input', CONFIG.password);
        await this.page.click('.connect-btn');

        // Wait for connection
        for (let i = 0; i < 30; i++) {
            await this.page.waitForTimeout(500);
            const connected = await this.page.evaluate(() => {
                return window.session && window.session.status === 'connected' &&
                       window.session.dc && window.session.dc.readyState === 'open';
            });
            if (connected) {
                this.log('Connected');
                return true;
            }
        }
        return false;
    }

    async sendCommand(cmd) {
        await this.page.evaluate((command) => {
            if (window.session && window.session.dc && window.session.dc.readyState === 'open') {
                const encoder = new TextEncoder();
                window.session.dc.send(encoder.encode(command + '\n'));
            }
        }, cmd);
    }

    async testSmallCommands() {
        this.log('\n=== Test: Small Commands (100 quick commands) ===');
        const startTime = Date.now();
        let successCount = 0;

        for (let i = 0; i < 100; i++) {
            const testId = `SMALL_${Date.now()}_${i}`;
            await this.sendCommand(`echo "${testId}"`);
            await this.page.waitForTimeout(50);
            successCount++;
        }

        const elapsed = Date.now() - startTime;
        this.log(`Sent 100 commands in ${elapsed}ms (${(100000/elapsed).toFixed(1)} cmd/s)`);
        return { commands: 100, time: elapsed, rate: 100000/elapsed };
    }

    async testLargeOutput() {
        this.log('\n=== Test: Large Output (generate 10KB data) ===');
        const startTime = Date.now();

        // Generate output using dd
        await this.sendCommand('dd if=/dev/zero bs=1024 count=10 2>/dev/null | base64');
        await this.page.waitForTimeout(3000);

        const elapsed = Date.now() - startTime;
        this.log(`Large output test completed in ${elapsed}ms`);

        // Check data channel state
        const state = await this.page.evaluate(() => {
            if (window.session && window.session.dc) {
                return {
                    readyState: window.session.dc.readyState,
                    bufferedAmount: window.session.dc.bufferedAmount
                };
            }
            return null;
        });

        this.log(`Data channel state: ${JSON.stringify(state)}`);
        return { time: elapsed, state };
    }

    async testRapidInput() {
        this.log('\n=== Test: Rapid Input (typing simulation) ===');
        const text = 'echo "The quick brown fox jumps over the lazy dog"';
        const startTime = Date.now();

        // Simulate typing character by character
        for (const char of text) {
            await this.page.evaluate((c) => {
                if (window.session && window.session.dc && window.session.dc.readyState === 'open') {
                    const encoder = new TextEncoder();
                    window.session.dc.send(encoder.encode(c));
                }
            }, char);
            await this.page.waitForTimeout(10); // 100 chars/sec typing speed
        }

        await this.sendCommand('');  // Enter
        await this.page.waitForTimeout(500);

        const elapsed = Date.now() - startTime;
        this.log(`Typed ${text.length} characters in ${elapsed}ms`);
        return { characters: text.length, time: elapsed };
    }

    async testConcurrentIO() {
        this.log('\n=== Test: Concurrent I/O (simultaneous read/write) ===');
        const startTime = Date.now();

        // Start a process that generates continuous output
        await this.sendCommand('for i in $(seq 1 20); do echo "Line $i at $(date +%s%N)"; sleep 0.1; done &');

        // While it's running, send additional commands
        for (let i = 0; i < 5; i++) {
            await this.page.waitForTimeout(200);
            await this.sendCommand(`echo "Input ${i}"`);
        }

        await this.page.waitForTimeout(3000);

        const elapsed = Date.now() - startTime;

        // Verify connection still stable
        const state = await this.page.evaluate(() => {
            if (window.session && window.session.dc) {
                return window.session.dc.readyState;
            }
            return 'unknown';
        });

        this.log(`Concurrent I/O test completed in ${elapsed}ms, state: ${state}`);
        return { time: elapsed, state };
    }

    async testInteractiveApp() {
        this.log('\n=== Test: Interactive Application (vim simulation) ===');

        // Check if vim is available
        await this.sendCommand('which vim || which vi || echo "NO_EDITOR"');
        await this.page.waitForTimeout(500);

        const startTime = Date.now();

        // Use a simpler interactive test with cat
        await this.sendCommand('cat > /tmp/test_input.txt << "ENDTEST"\nLine 1\nLine 2\nLine 3\nENDTEST');
        await this.page.waitForTimeout(500);

        // Verify file was created
        await this.sendCommand('cat /tmp/test_input.txt');
        await this.page.waitForTimeout(500);

        const elapsed = Date.now() - startTime;

        const state = await this.page.evaluate(() => {
            if (window.session && window.session.dc) {
                return window.session.dc.readyState;
            }
            return 'unknown';
        });

        this.log(`Interactive test completed in ${elapsed}ms, state: ${state}`);
        return { time: elapsed, state };
    }

    async cleanup() {
        if (this.browser) await this.browser.close();
        if (this.serverProcess) this.serverProcess.kill('SIGTERM');
        if (this.httpServer) this.httpServer.kill('SIGTERM');
    }

    async run() {
        this.log('========================================');
        this.log('  DATA THROUGHPUT TEST - Terminal Tunnel');
        this.log('========================================\n');

        const results = {};

        try {
            await this.startHttpServer();
            await this.startTerminalTunnel();
            await this.launchBrowser();

            if (!await this.connect()) {
                throw new Error('Failed to connect');
            }

            await this.page.waitForTimeout(1000);

            results.smallCommands = await this.testSmallCommands();
            results.largeOutput = await this.testLargeOutput();
            results.rapidInput = await this.testRapidInput();
            results.concurrentIO = await this.testConcurrentIO();
            results.interactive = await this.testInteractiveApp();

        } catch (err) {
            this.log(`Error: ${err.message}`);
            results.error = err.message;
        } finally {
            await this.cleanup();
        }

        console.log('\n========================================');
        console.log('           THROUGHPUT RESULTS');
        console.log('========================================\n');

        if (results.smallCommands) {
            console.log(`Small Commands: ${results.smallCommands.rate.toFixed(1)} cmd/s`);
        }
        if (results.largeOutput) {
            console.log(`Large Output: ${results.largeOutput.state?.readyState || 'unknown'}`);
        }
        if (results.rapidInput) {
            console.log(`Rapid Input: ${results.rapidInput.characters} chars in ${results.rapidInput.time}ms`);
        }
        if (results.concurrentIO) {
            console.log(`Concurrent I/O: ${results.concurrentIO.state}`);
        }
        if (results.interactive) {
            console.log(`Interactive: ${results.interactive.state}`);
        }

        const allPassed = !results.error &&
            results.largeOutput?.state?.readyState === 'open' &&
            results.concurrentIO?.state === 'open' &&
            results.interactive?.state === 'open';

        console.log('\n----------------------------------------');
        console.log(`Overall: ${allPassed ? 'PASS' : 'FAIL'}`);
        console.log('----------------------------------------');

        process.exit(allPassed ? 0 : 1);
    }
}

new ThroughputTest().run();
