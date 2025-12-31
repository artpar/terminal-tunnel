#!/usr/bin/env node
/**
 * OS-Level Network Condition Stress Test for Terminal Tunnel
 *
 * Uses macOS dummynet (dnctl/pfctl) to simulate real network conditions
 * that affect WebRTC at the OS level - much more realistic than browser emulation.
 *
 * REQUIRES: sudo access to configure packet filter rules
 *
 * Usage: sudo node network-conditioner-test.js
 */

const { chromium } = require('playwright');
const { spawn, execSync } = require('child_process');
const path = require('path');

const CONFIG = {
    password: 'networkcondtest123',
    webClientUrl: 'http://localhost:8888',
    ttPath: path.join(__dirname, '../../tt'),
    staticDir: path.join(__dirname, '../../internal/web/static'),
};

// Network condition profiles using dummynet
// These affect ALL traffic on the loopback interface (realistic WebRTC testing)
const NETWORK_PROFILES = {
    // Baseline - no conditioning
    normal: {
        name: 'Normal (baseline)',
        dnctl: null, // No rules
    },

    // Latency tests
    latency_50ms: {
        name: 'Latency 50ms',
        dnctl: 'pipe 1 config delay 50ms',
    },
    latency_100ms: {
        name: 'Latency 100ms',
        dnctl: 'pipe 1 config delay 100ms',
    },
    latency_200ms: {
        name: 'Latency 200ms',
        dnctl: 'pipe 1 config delay 200ms',
    },
    latency_500ms: {
        name: 'Latency 500ms (harsh)',
        dnctl: 'pipe 1 config delay 500ms',
    },
    latency_1000ms: {
        name: 'Latency 1000ms (extreme)',
        dnctl: 'pipe 1 config delay 1000ms',
    },

    // Packet loss tests
    loss_1pct: {
        name: 'Packet Loss 1%',
        dnctl: 'pipe 1 config plr 0.01',
    },
    loss_5pct: {
        name: 'Packet Loss 5%',
        dnctl: 'pipe 1 config plr 0.05',
    },
    loss_10pct: {
        name: 'Packet Loss 10% (harsh)',
        dnctl: 'pipe 1 config plr 0.10',
    },
    loss_20pct: {
        name: 'Packet Loss 20% (extreme)',
        dnctl: 'pipe 1 config plr 0.20',
    },

    // Bandwidth throttling
    bw_1mbps: {
        name: 'Bandwidth 1 Mbps',
        dnctl: 'pipe 1 config bw 1Mbit/s',
    },
    bw_512kbps: {
        name: 'Bandwidth 512 Kbps',
        dnctl: 'pipe 1 config bw 512Kbit/s',
    },
    bw_256kbps: {
        name: 'Bandwidth 256 Kbps (3G)',
        dnctl: 'pipe 1 config bw 256Kbit/s',
    },
    bw_128kbps: {
        name: 'Bandwidth 128 Kbps (edge)',
        dnctl: 'pipe 1 config bw 128Kbit/s',
    },

    // Combined conditions (realistic scenarios)
    mobile_3g: {
        name: '3G Mobile (150ms + 384kbps + 1% loss)',
        dnctl: 'pipe 1 config delay 150ms bw 384Kbit/s plr 0.01',
    },
    mobile_edge: {
        name: 'EDGE Mobile (300ms + 128kbps + 2% loss)',
        dnctl: 'pipe 1 config delay 300ms bw 128Kbit/s plr 0.02',
    },
    satellite: {
        name: 'Satellite (600ms + 1Mbps)',
        dnctl: 'pipe 1 config delay 600ms bw 1Mbit/s',
    },
    lossy_wifi: {
        name: 'Lossy WiFi (30ms + 5% loss)',
        dnctl: 'pipe 1 config delay 30ms plr 0.05',
    },
    congested: {
        name: 'Congested Network (100ms + 256kbps + 3% loss)',
        dnctl: 'pipe 1 config delay 100ms bw 256Kbit/s plr 0.03',
    },

    // Extreme/stress conditions
    extreme_latency: {
        name: 'EXTREME: 2s latency',
        dnctl: 'pipe 1 config delay 2000ms',
    },
    extreme_loss: {
        name: 'EXTREME: 30% packet loss',
        dnctl: 'pipe 1 config plr 0.30',
    },
    nightmare: {
        name: 'NIGHTMARE: 500ms + 64kbps + 10% loss',
        dnctl: 'pipe 1 config delay 500ms bw 64Kbit/s plr 0.10',
    },
};

class NetworkConditionerTest {
    constructor() {
        this.httpServer = null;
        this.ttProcess = null;
        this.browser = null;
        this.sessionCode = null;
        this.results = [];
        this.pfEnabled = false;
    }

    log(msg) {
        console.log(`[${new Date().toISOString()}] ${msg}`);
    }

    // Check if running as root (required for dnctl/pfctl)
    checkRoot() {
        try {
            execSync('id -u', { encoding: 'utf8' });
            const uid = parseInt(execSync('id -u', { encoding: 'utf8' }).trim());
            if (uid !== 0) {
                console.error('\n⚠️  This script must be run as root (sudo) to configure network conditions.\n');
                console.error('Usage: sudo node network-conditioner-test.js\n');
                process.exit(1);
            }
        } catch (e) {
            console.error('Failed to check root status:', e.message);
            process.exit(1);
        }
    }

    // Apply network condition using dummynet
    async applyNetworkCondition(profile) {
        this.log(`Applying network condition: ${profile.name}`);

        try {
            // Clear existing rules
            await this.clearNetworkConditions();

            if (profile.dnctl) {
                // Create dummynet pipe
                execSync(`dnctl ${profile.dnctl}`, { encoding: 'utf8' });

                // Create pf anchor rules to direct traffic through pipe
                // We'll affect traffic on port 8888 (HTTP) and WebRTC ports
                const pfRules = `
dummynet out proto tcp from any to any port 8888 pipe 1
dummynet in proto tcp from any port 8888 to any pipe 1
dummynet out proto udp from any to any pipe 1
dummynet in proto udp from any to any pipe 1
`;
                // Write rules to temp file
                require('fs').writeFileSync('/tmp/tt_test_pf.rules', pfRules);

                // Load rules
                execSync('pfctl -a tt_test -f /tmp/tt_test_pf.rules 2>/dev/null', { encoding: 'utf8' });

                // Enable pf if not already enabled
                if (!this.pfEnabled) {
                    try {
                        execSync('pfctl -E 2>/dev/null', { encoding: 'utf8' });
                    } catch (e) {
                        // May already be enabled
                    }
                    this.pfEnabled = true;
                }

                this.log(`  Applied: ${profile.dnctl}`);
            }
        } catch (e) {
            this.log(`  Warning: Failed to apply condition: ${e.message}`);
        }
    }

    // Clear all network conditions
    async clearNetworkConditions() {
        try {
            execSync('dnctl -q flush', { encoding: 'utf8' });
            execSync('pfctl -a tt_test -F all 2>/dev/null', { encoding: 'utf8' });
        } catch (e) {
            // Ignore errors when clearing
        }
    }

    async startHttpServer() {
        this.log('Starting HTTP server...');
        this.httpServer = spawn('python3', ['-m', 'http.server', '8888'], {
            cwd: CONFIG.staticDir,
            stdio: ['ignore', 'pipe', 'pipe'],
        });
        await new Promise(r => setTimeout(r, 1000));
        this.log('HTTP server started on port 8888');
    }

    async startTerminalTunnel() {
        this.log('Starting terminal-tunnel server...');

        return new Promise((resolve, reject) => {
            this.ttProcess = spawn(CONFIG.ttPath, ['start', '-p', CONFIG.password], {
                stdio: ['pipe', 'pipe', 'pipe'],
            });

            let output = '';
            const timeout = setTimeout(() => reject(new Error('Timeout')), 30000);

            this.ttProcess.stdout.on('data', (data) => {
                output += data.toString();
                const match = output.match(/Code:\s+([A-Z0-9]{8})/);
                if (match && !this.sessionCode) {
                    this.sessionCode = match[1];
                    clearTimeout(timeout);
                    this.log(`Session code: ${this.sessionCode}`);
                    resolve();
                }
            });

            this.ttProcess.on('error', reject);
        });
    }

    async launchBrowser() {
        this.log('Launching browser...');
        this.browser = await chromium.launch({ headless: true });
    }

    async testCondition(profileKey) {
        const profile = NETWORK_PROFILES[profileKey];
        this.log(`\n${'='.repeat(60)}`);
        this.log(`Testing: ${profile.name}`);
        this.log('='.repeat(60));

        const result = {
            profile: profileKey,
            name: profile.name,
            initialConnect: { success: false, time: 0 },
            reconnect: { success: false, time: 0 },
            dataTransfer: { success: false },
            errors: [],
        };

        const context = await this.browser.newContext({ bypassCSP: true });
        const page = await context.newPage();

        // Log browser console
        page.on('console', msg => {
            if (msg.type() === 'log' && msg.text().includes('[')) {
                this.log(`  Browser: ${msg.text()}`);
            }
        });

        try {
            // Apply network condition
            await this.applyNetworkCondition(profile);
            await new Promise(r => setTimeout(r, 500)); // Let conditions stabilize

            // Test 1: Initial Connection
            this.log('\n--- Test 1: Initial Connection ---');
            const connectStart = Date.now();

            await page.goto(`${CONFIG.webClientUrl}/?c=${this.sessionCode}`);
            await page.waitForTimeout(2000);

            await page.waitForSelector('.password-input', { timeout: 30000 });
            await page.fill('.password-input', CONFIG.password);
            await page.click('.connect-btn');

            // Wait for connection with extended timeout for harsh conditions
            const timeout = profile.dnctl?.includes('2000ms') ? 120000 : 60000;
            for (let i = 0; i < timeout / 500; i++) {
                await page.waitForTimeout(500);
                const connected = await page.evaluate(() => {
                    return window.session && window.session.status === 'connected' &&
                           window.session.dc && window.session.dc.readyState === 'open';
                });
                if (connected) {
                    result.initialConnect.success = true;
                    result.initialConnect.time = Date.now() - connectStart;
                    this.log(`  ✓ Connected in ${result.initialConnect.time}ms`);
                    break;
                }
            }

            if (!result.initialConnect.success) {
                throw new Error('Initial connection timeout');
            }

            // Test 2: Data Transfer
            this.log('\n--- Test 2: Data Transfer ---');
            const testId = `TEST_${Date.now()}`;
            await page.evaluate((id) => {
                if (window.session?.dc?.readyState === 'open') {
                    const encoder = new TextEncoder();
                    window.session.dc.send(encoder.encode(`echo "${id}"\n`));
                }
            }, testId);

            // Wait longer for slow connections
            const dataWait = profile.dnctl?.includes('delay') ?
                Math.max(5000, parseInt(profile.dnctl.match(/delay (\d+)ms/)?.[1] || 0) * 3) : 3000;
            await page.waitForTimeout(dataWait);

            const output = await page.evaluate(() => {
                const term = document.querySelector('.terminal-container');
                return term ? term.textContent : '';
            });

            result.dataTransfer.success = output.includes(testId);
            this.log(`  ${result.dataTransfer.success ? '✓' : '✗'} Data transfer: ${result.dataTransfer.success ? 'PASS' : 'FAIL'}`);

            // Test 3: Reconnection
            this.log('\n--- Test 3: Reconnection (page reload) ---');
            const reconnectStart = Date.now();

            await page.reload();
            await page.waitForTimeout(2000);

            await page.waitForSelector('.password-input', { timeout: 30000 });
            await page.fill('.password-input', CONFIG.password);
            await page.click('.connect-btn');

            for (let i = 0; i < timeout / 500; i++) {
                await page.waitForTimeout(500);
                const reconnected = await page.evaluate(() => {
                    return window.session && window.session.status === 'connected' &&
                           window.session.dc && window.session.dc.readyState === 'open';
                });
                if (reconnected) {
                    result.reconnect.success = true;
                    result.reconnect.time = Date.now() - reconnectStart;
                    this.log(`  ✓ Reconnected in ${result.reconnect.time}ms`);
                    break;
                }
            }

            if (!result.reconnect.success) {
                result.errors.push('Reconnection timeout');
                this.log('  ✗ Reconnection timeout');
            }

        } catch (err) {
            this.log(`  ✗ Error: ${err.message}`);
            result.errors.push(err.message);
        } finally {
            await context.close();
            await this.clearNetworkConditions();
        }

        this.results.push(result);
        return result;
    }

    async runAllTests() {
        this.log('╔════════════════════════════════════════════════════════════╗');
        this.log('║  OS-LEVEL NETWORK CONDITION STRESS TEST                    ║');
        this.log('║  Terminal Tunnel - WebRTC Reliability Testing              ║');
        this.log('╚════════════════════════════════════════════════════════════╝\n');

        this.checkRoot();

        try {
            await this.startHttpServer();
            await this.startTerminalTunnel();
            await this.launchBrowser();

            // Run all test profiles
            const profiles = Object.keys(NETWORK_PROFILES);
            for (const profileKey of profiles) {
                await this.testCondition(profileKey);
                // Brief pause between tests
                await new Promise(r => setTimeout(r, 2000));
            }

        } catch (err) {
            this.log(`Fatal error: ${err.message}`);
        } finally {
            await this.cleanup();
        }

        this.printResults();
    }

    async cleanup() {
        this.log('\nCleaning up...');
        await this.clearNetworkConditions();

        if (this.browser) await this.browser.close();
        if (this.ttProcess) this.ttProcess.kill('SIGTERM');
        if (this.httpServer) this.httpServer.kill('SIGTERM');

        // Clean up pf temp file
        try {
            require('fs').unlinkSync('/tmp/tt_test_pf.rules');
        } catch (e) {}
    }

    printResults() {
        console.log('\n');
        console.log('╔════════════════════════════════════════════════════════════════════════════╗');
        console.log('║                         NETWORK STRESS TEST RESULTS                        ║');
        console.log('╚════════════════════════════════════════════════════════════════════════════╝\n');

        // Summary table
        console.log('┌─────────────────────────────────────┬─────────┬──────────┬─────────┬────────┐');
        console.log('│ Condition                           │ Connect │ Reconn.  │ Data    │ Status │');
        console.log('├─────────────────────────────────────┼─────────┼──────────┼─────────┼────────┤');

        let passed = 0;
        let failed = 0;

        for (const r of this.results) {
            const connectOk = r.initialConnect.success ? '✓' : '✗';
            const reconnOk = r.reconnect.success ? '✓' : '✗';
            const dataOk = r.dataTransfer.success ? '✓' : '✗';
            const allPass = r.initialConnect.success && r.reconnect.success && r.dataTransfer.success;
            const status = allPass ? '✓ PASS' : '✗ FAIL';

            if (allPass) passed++; else failed++;

            const name = r.name.substring(0, 35).padEnd(35);
            const connectTime = r.initialConnect.success ? `${(r.initialConnect.time/1000).toFixed(1)}s`.padStart(5) : ' fail';
            const reconnTime = r.reconnect.success ? `${(r.reconnect.time/1000).toFixed(1)}s`.padStart(6) : '  fail';

            console.log(`│ ${name} │ ${connectOk} ${connectTime} │ ${reconnOk} ${reconnTime} │    ${dataOk}    │ ${status} │`);
        }

        console.log('└─────────────────────────────────────┴─────────┴──────────┴─────────┴────────┘');

        console.log('\n────────────────────────────────────────────────────────────────────────────────');
        console.log(`  Total: ${this.results.length} conditions tested`);
        console.log(`  Passed: ${passed} (${(passed/this.results.length*100).toFixed(1)}%)`);
        console.log(`  Failed: ${failed} (${(failed/this.results.length*100).toFixed(1)}%)`);
        console.log('────────────────────────────────────────────────────────────────────────────────');

        // Reliability assessment
        const reliabilityPct = (passed / this.results.length * 100);
        let assessment = '';
        if (reliabilityPct >= 95) assessment = '🏆 EXCELLENT - Production ready for all conditions';
        else if (reliabilityPct >= 80) assessment = '✓ GOOD - Handles most network conditions';
        else if (reliabilityPct >= 60) assessment = '⚠ FAIR - May struggle with harsh conditions';
        else assessment = '✗ POOR - Needs improvement for reliability';

        console.log(`\n  Reliability Assessment: ${assessment}\n`);

        process.exit(failed > 0 ? 1 : 0);
    }
}

// Handle graceful shutdown
process.on('SIGINT', async () => {
    console.log('\nInterrupted - cleaning up...');
    try {
        execSync('dnctl -q flush', { encoding: 'utf8' });
        execSync('pfctl -a tt_test -F all 2>/dev/null', { encoding: 'utf8' });
    } catch (e) {}
    process.exit(1);
});

new NetworkConditionerTest().runAllTests();
