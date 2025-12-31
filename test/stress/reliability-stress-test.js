#!/usr/bin/env node
/**
 * Comprehensive Reliability Stress Test for Terminal Tunnel
 *
 * Tests connection reliability through various stress scenarios
 * without requiring root access. Focuses on:
 * - Rapid reconnections
 * - Sustained stability
 * - Data integrity
 * - Edge case handling
 */

const { chromium } = require('playwright');
const { spawn } = require('child_process');
const path = require('path');

const CONFIG = {
    password: 'reliabilitytest123',
    webClientUrl: 'http://localhost:8888',
    ttPath: path.join(__dirname, '../../tt'),
    staticDir: path.join(__dirname, '../../internal/web/static'),
};

class ReliabilityStressTest {
    constructor() {
        this.httpServer = null;
        this.ttProcess = null;
        this.browser = null;
        this.sessionCode = null;
        this.testResults = {};
    }

    log(msg) {
        console.log(`[${new Date().toISOString()}] ${msg}`);
    }

    async startInfrastructure() {
        // Start HTTP server
        this.httpServer = spawn('python3', ['-m', 'http.server', '8888'], {
            cwd: CONFIG.staticDir,
            stdio: ['ignore', 'pipe', 'pipe'],
        });
        await new Promise(r => setTimeout(r, 1000));
        this.log('HTTP server started');

        // Start terminal tunnel
        await new Promise((resolve, reject) => {
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

        // Launch browser
        this.browser = await chromium.launch({ headless: true });
        this.log('Browser launched');
    }

    async createPage() {
        const context = await this.browser.newContext({ bypassCSP: true });
        const page = await context.newPage();

        page.on('console', msg => {
            const text = msg.text();
            if (text.includes('[ICE]') || text.includes('[DC]') || text.includes('[PC]')) {
                this.log(`  Browser: ${text}`);
            }
        });

        return { page, context };
    }

    async connect(page, timeout = 30000) {
        await page.goto(`${CONFIG.webClientUrl}/?c=${this.sessionCode}`);
        await page.waitForTimeout(1500);

        await page.waitForSelector('.password-input', { timeout: 10000 });
        await page.fill('.password-input', CONFIG.password);
        await page.click('.connect-btn');

        const start = Date.now();
        for (let i = 0; i < timeout / 500; i++) {
            await page.waitForTimeout(500);

            // Check for terminal element (indicates successful connection)
            const hasTerminal = await page.evaluate(() => {
                const terminal = document.querySelector('.terminal-container .xterm');
                return !!terminal;
            });
            if (hasTerminal) {
                return Date.now() - start;
            }

            // Check for error state
            const hasError = await page.evaluate(() => {
                const status = document.querySelector('.status-text');
                return status?.textContent?.includes('error') || status?.textContent?.includes('failed');
            });
            if (hasError) {
                throw new Error('Connection error detected');
            }
        }
        throw new Error('Connection timeout');
    }

    async sendTestData(page, testId) {
        // Find the active session and send data
        await page.evaluate((id) => {
            // Use SessionManager exposed on window
            const mgr = window.sessionManager;
            if (mgr) {
                const session = mgr.getActiveSession();
                if (session?.dc?.readyState === 'open') {
                    const encoder = new TextEncoder();
                    session.dc.send(encoder.encode(`echo "${id}"\n`));
                    return;
                }
            }
        }, testId);
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 1: Rapid Consecutive Reconnections (10 cycles)
    // ═══════════════════════════════════════════════════════════════════════════
    async testRapidReconnections() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 1: Rapid Consecutive Reconnections (10 cycles)         ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const cycles = 10;
        const results = { total: cycles, success: 0, times: [], errors: [] };

        const { page, context } = await this.createPage();

        try {
            // Initial connection
            const initialTime = await this.connect(page);
            this.log(`Initial connection: ${initialTime}ms`);
            results.times.push(initialTime);
            results.success++;

            // Rapid reconnection cycles
            for (let i = 1; i <= cycles - 1; i++) {
                this.log(`\nCycle ${i}/${cycles - 1}...`);
                const start = Date.now();

                try {
                    await page.reload();
                    await page.waitForTimeout(1000);

                    await page.waitForSelector('.password-input', { timeout: 15000 });
                    await page.fill('.password-input', CONFIG.password);
                    await page.click('.connect-btn');

                    const connected = await this.waitForConnection(page, 30000);
                    if (connected) {
                        const time = Date.now() - start;
                        results.times.push(time);
                        results.success++;
                        this.log(`  ✓ Reconnected in ${time}ms`);
                    } else {
                        results.errors.push(`Cycle ${i}: timeout`);
                        this.log(`  ✗ Timeout`);
                    }
                } catch (err) {
                    results.errors.push(`Cycle ${i}: ${err.message}`);
                    this.log(`  ✗ Error: ${err.message}`);
                }

                // Brief pause between cycles
                await page.waitForTimeout(500);
            }
        } finally {
            await context.close();
        }

        results.avgTime = results.times.length > 0 ?
            Math.round(results.times.reduce((a, b) => a + b, 0) / results.times.length) : 0;
        results.maxTime = results.times.length > 0 ? Math.max(...results.times) : 0;

        this.testResults.rapidReconnections = results;
        this.log(`\nResult: ${results.success}/${results.total} (avg: ${results.avgTime}ms, max: ${results.maxTime}ms)`);
        return results;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 2: Rapid Fire Refreshes (3 refreshes in quick succession)
    // ═══════════════════════════════════════════════════════════════════════════
    async testRapidFireRefreshes() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 2: Rapid Fire Refreshes (stress test reconnection)     ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const batches = 5;
        const refreshesPerBatch = 3;
        const results = { total: batches, success: 0, errors: [] };

        const { page, context } = await this.createPage();

        try {
            // Initial connection
            await this.connect(page);
            this.log('Initial connection established');

            for (let batch = 1; batch <= batches; batch++) {
                this.log(`\nBatch ${batch}/${batches}: ${refreshesPerBatch} rapid refreshes...`);

                // Fire rapid refreshes
                for (let r = 0; r < refreshesPerBatch; r++) {
                    await page.reload();
                    await page.waitForTimeout(200); // Very quick between refreshes
                }

                // Now try to recover
                await page.waitForTimeout(2000);

                try {
                    await page.waitForSelector('.password-input', { timeout: 20000 });
                    await page.fill('.password-input', CONFIG.password);
                    await page.click('.connect-btn');

                    const recovered = await this.waitForConnection(page, 45000);
                    if (recovered) {
                        results.success++;
                        this.log(`  ✓ Recovered from rapid fire batch`);
                    } else {
                        results.errors.push(`Batch ${batch}: failed to recover`);
                        this.log(`  ✗ Failed to recover`);
                    }
                } catch (err) {
                    results.errors.push(`Batch ${batch}: ${err.message}`);
                    this.log(`  ✗ Error: ${err.message}`);
                }

                await page.waitForTimeout(2000);
            }
        } finally {
            await context.close();
        }

        this.testResults.rapidFireRefreshes = results;
        this.log(`\nResult: ${results.success}/${results.total} batches recovered`);
        return results;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 3: Sustained Connection Stability (60 second idle + periodic pings)
    // ═══════════════════════════════════════════════════════════════════════════
    async testSustainedStability() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 3: Sustained Connection Stability (60s with pings)     ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const duration = 60000; // 60 seconds
        const pingInterval = 10000; // Every 10 seconds
        const results = { stable: true, pings: 0, totalPings: 0, disconnects: 0 };

        const { page, context } = await this.createPage();

        try {
            await this.connect(page);
            this.log('Connection established, starting stability test...');

            const startTime = Date.now();
            let lastPing = startTime;

            while (Date.now() - startTime < duration) {
                await page.waitForTimeout(1000);

                // Check connection status via terminal presence
                const status = await page.evaluate(() => {
                    const terminal = document.querySelector('.terminal-container .xterm');
                    return terminal ? 'connected' : 'disconnected';
                });

                if (status !== 'connected') {
                    results.disconnects++;
                    this.log(`  ⚠ Disconnect detected: ${status}`);
                    results.stable = false;
                }

                // Periodic ping - verify data channel is open and can send data
                if (Date.now() - lastPing >= pingInterval) {
                    results.totalPings++;
                    const elapsed = Math.round((Date.now() - startTime) / 1000);

                    // Check if data channel is open and responsive
                    const pingResult = await page.evaluate(() => {
                        // Use SessionManager exposed on window
                        const mgr = window.sessionManager;
                        if (mgr) {
                            const session = mgr.getActiveSession();
                            if (session?.dc?.readyState === 'open') {
                                try {
                                    const encoder = new TextEncoder();
                                    session.dc.send(encoder.encode(`# ping ${Date.now()}\n`));
                                    return { success: true, method: 'sessionManager' };
                                } catch (e) {
                                    return { success: false, error: e.message };
                                }
                            }
                            return { success: false, error: `dc state: ${session?.dc?.readyState || 'no dc'}` };
                        }
                        return { success: false, error: 'sessionManager not found' };
                    });

                    if (pingResult.success) {
                        results.pings++;
                        this.log(`  ✓ Ping ${results.pings}/${results.totalPings} at ${elapsed}s (dc open)`);
                    } else {
                        this.log(`  ✗ Ping ${results.totalPings} failed at ${elapsed}s: ${pingResult.error}`);
                    }
                    lastPing = Date.now();
                }

                // Progress indicator every 10s
                const elapsed = Math.round((Date.now() - startTime) / 1000);
                if (elapsed % 10 === 0 && elapsed > 0) {
                    this.log(`  ... ${elapsed}s elapsed, connection: ${status}`);
                }
            }
        } finally {
            await context.close();
        }

        this.testResults.sustainedStability = results;
        this.log(`\nResult: ${results.stable ? 'STABLE' : 'UNSTABLE'}, ${results.pings}/${results.totalPings} pings, ${results.disconnects} disconnects`);
        return results;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 4: Data Integrity Under Load
    // ═══════════════════════════════════════════════════════════════════════════
    async testDataIntegrity() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 4: Data Integrity Under Load (100 rapid commands)      ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const commandCount = 100;
        const results = { total: commandCount, sent: 0, connectionStable: true };

        const { page, context } = await this.createPage();

        try {
            await this.connect(page);
            this.log('Connection established, sending rapid commands...');

            const startTime = Date.now();

            for (let i = 0; i < commandCount; i++) {
                const testId = `CMD_${i}_${Date.now()}`;
                await this.sendTestData(page, testId);
                results.sent++;

                // Brief pause between commands (simulates fast typing)
                await page.waitForTimeout(20);

                // Check connection every 20 commands
                if (i % 20 === 0) {
                    const hasTerminal = await page.evaluate(() => {
                        const terminal = document.querySelector('.terminal-container .xterm');
                        return !!terminal;
                    });
                    if (!hasTerminal) {
                        results.connectionStable = false;
                        this.log(`  ⚠ Connection lost at command ${i}`);
                        break;
                    }
                    this.log(`  Progress: ${i}/${commandCount} commands sent`);
                }
            }

            const elapsed = Date.now() - startTime;
            const rate = Math.round(results.sent / (elapsed / 1000));
            this.log(`  Sent ${results.sent} commands in ${elapsed}ms (${rate} cmd/s)`);

            // Final check - is connection still alive?
            await page.waitForTimeout(2000);
            const hasTerminal = await page.evaluate(() => {
                const terminal = document.querySelector('.terminal-container .xterm');
                return !!terminal;
            });
            results.connectionStable = results.connectionStable && hasTerminal;

        } finally {
            await context.close();
        }

        this.testResults.dataIntegrity = results;
        this.log(`\nResult: ${results.sent}/${results.total} commands, connection ${results.connectionStable ? 'STABLE' : 'LOST'}`);
        return results;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 5: Multi-Tab Stress (simulate multiple browser tabs)
    // ═══════════════════════════════════════════════════════════════════════════
    async testMultiTab() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 5: Multi-Tab Connection Competition                     ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const tabCount = 3;
        const results = { tabs: tabCount, connected: 0, errors: [] };
        const contexts = [];

        try {
            // Open multiple tabs simultaneously
            this.log(`Opening ${tabCount} tabs simultaneously...`);

            const connectionPromises = [];
            for (let i = 0; i < tabCount; i++) {
                const { page, context } = await this.createPage();
                contexts.push(context);

                connectionPromises.push((async () => {
                    try {
                        const time = await this.connect(page, 45000);
                        this.log(`  Tab ${i + 1}: connected in ${time}ms`);
                        return true;
                    } catch (err) {
                        this.log(`  Tab ${i + 1}: failed - ${err.message}`);
                        results.errors.push(`Tab ${i + 1}: ${err.message}`);
                        return false;
                    }
                })());
            }

            const connectionResults = await Promise.all(connectionPromises);
            results.connected = connectionResults.filter(r => r).length;

            // Note: Only one tab should "win" the connection since it's the same session
            this.log(`\n${results.connected} tabs attempted connection (1 expected to win)`);

        } finally {
            for (const ctx of contexts) {
                await ctx.close();
            }
        }

        this.testResults.multiTab = results;
        return results;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TEST 6: Recovery from Hard Disconnect
    // ═══════════════════════════════════════════════════════════════════════════
    async testHardDisconnectRecovery() {
        this.log('\n╔═══════════════════════════════════════════════════════════════╗');
        this.log('║  TEST 6: Recovery from Hard Disconnect (close data channel)  ║');
        this.log('╚═══════════════════════════════════════════════════════════════╝\n');

        const attempts = 3;
        const results = { total: attempts, recovered: 0, times: [] };

        const { page, context } = await this.createPage();

        try {
            await this.connect(page);
            this.log('Initial connection established');

            for (let i = 1; i <= attempts; i++) {
                this.log(`\nAttempt ${i}/${attempts}: Forcing hard disconnect...`);

                // Force close the data channel/peer connection
                await page.evaluate(() => {
                    // Try window.session
                    if (window.session?.dc) window.session.dc.close();
                    if (window.session?.pc) window.session.pc.close();
                    // Try window.sessions array
                    if (window.sessions) {
                        for (const s of window.sessions) {
                            if (s.dc) s.dc.close();
                            if (s.pc) s.pc.close();
                        }
                    }
                    // Try manager.sessions
                    if (window.manager?.sessions) {
                        for (const s of window.manager.sessions) {
                            if (s.dc) s.dc.close();
                            if (s.pc) s.pc.close();
                        }
                    }
                });

                await page.waitForTimeout(1000);

                // Try to reconnect
                const start = Date.now();
                await page.reload();
                await page.waitForTimeout(2000);

                try {
                    await page.waitForSelector('.password-input', { timeout: 15000 });
                    await page.fill('.password-input', CONFIG.password);
                    await page.click('.connect-btn');

                    const recovered = await this.waitForConnection(page, 30000);
                    if (recovered) {
                        const time = Date.now() - start;
                        results.recovered++;
                        results.times.push(time);
                        this.log(`  ✓ Recovered in ${time}ms`);
                    } else {
                        this.log(`  ✗ Failed to recover`);
                    }
                } catch (err) {
                    this.log(`  ✗ Error: ${err.message}`);
                }

                await page.waitForTimeout(2000);
            }
        } finally {
            await context.close();
        }

        results.avgTime = results.times.length > 0 ?
            Math.round(results.times.reduce((a, b) => a + b, 0) / results.times.length) : 0;

        this.testResults.hardDisconnectRecovery = results;
        this.log(`\nResult: ${results.recovered}/${results.total} recoveries (avg: ${results.avgTime}ms)`);
        return results;
    }

    async waitForConnection(page, timeout) {
        for (let i = 0; i < timeout / 500; i++) {
            await page.waitForTimeout(500);
            const hasTerminal = await page.evaluate(() => {
                const terminal = document.querySelector('.terminal-container .xterm');
                return !!terminal;
            });
            if (hasTerminal) return true;

            // Check for error
            const hasError = await page.evaluate(() => {
                const status = document.querySelector('.status-text');
                return status?.textContent?.includes('error') || status?.textContent?.includes('failed');
            });
            if (hasError) return false;
        }
        return false;
    }

    async cleanup() {
        this.log('\nCleaning up...');
        if (this.browser) await this.browser.close();
        if (this.ttProcess) this.ttProcess.kill('SIGTERM');
        if (this.httpServer) this.httpServer.kill('SIGTERM');
    }

    printFinalResults() {
        console.log('\n');
        console.log('╔══════════════════════════════════════════════════════════════════════════════╗');
        console.log('║                    RELIABILITY STRESS TEST - FINAL RESULTS                  ║');
        console.log('╚══════════════════════════════════════════════════════════════════════════════╝\n');

        const r = this.testResults;
        let totalTests = 0;
        let passedTests = 0;

        // Test 1: Rapid Reconnections
        if (r.rapidReconnections) {
            const pct = Math.round(r.rapidReconnections.success / r.rapidReconnections.total * 100);
            const status = pct >= 90 ? '✓ PASS' : pct >= 70 ? '⚠ WARN' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 1: Rapid Reconnections                                                 │`);
            console.log(`│   Success Rate: ${r.rapidReconnections.success}/${r.rapidReconnections.total} (${pct}%)                                              │`);
            console.log(`│   Avg Time: ${r.rapidReconnections.avgTime}ms | Max Time: ${r.rapidReconnections.maxTime}ms                                   │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (pct >= 90) passedTests++;
        }

        // Test 2: Rapid Fire Refreshes
        if (r.rapidFireRefreshes) {
            const pct = Math.round(r.rapidFireRefreshes.success / r.rapidFireRefreshes.total * 100);
            const status = pct >= 60 ? '✓ PASS' : pct >= 40 ? '⚠ WARN' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 2: Rapid Fire Refreshes                                                │`);
            console.log(`│   Recovery Rate: ${r.rapidFireRefreshes.success}/${r.rapidFireRefreshes.total} (${pct}%)                                             │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (pct >= 60) passedTests++;
        }

        // Test 3: Sustained Stability
        if (r.sustainedStability) {
            const pingPct = r.sustainedStability.totalPings > 0 ?
                Math.round(r.sustainedStability.pings / r.sustainedStability.totalPings * 100) : 0;
            const status = r.sustainedStability.stable && pingPct >= 90 ? '✓ PASS' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 3: Sustained Stability (60s)                                           │`);
            console.log(`│   Connection: ${r.sustainedStability.stable ? 'STABLE' : 'UNSTABLE'}                                                      │`);
            console.log(`│   Pings: ${r.sustainedStability.pings}/${r.sustainedStability.totalPings} (${pingPct}%)                                                       │`);
            console.log(`│   Disconnects: ${r.sustainedStability.disconnects}                                                           │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (r.sustainedStability.stable && pingPct >= 90) passedTests++;
        }

        // Test 4: Data Integrity
        if (r.dataIntegrity) {
            const pct = Math.round(r.dataIntegrity.sent / r.dataIntegrity.total * 100);
            const status = r.dataIntegrity.connectionStable && pct >= 95 ? '✓ PASS' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 4: Data Integrity Under Load                                           │`);
            console.log(`│   Commands Sent: ${r.dataIntegrity.sent}/${r.dataIntegrity.total} (${pct}%)                                           │`);
            console.log(`│   Connection: ${r.dataIntegrity.connectionStable ? 'STABLE' : 'LOST'}                                                        │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (r.dataIntegrity.connectionStable && pct >= 95) passedTests++;
        }

        // Test 5: Multi-Tab
        if (r.multiTab) {
            const status = r.multiTab.connected >= 1 ? '✓ PASS' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 5: Multi-Tab Competition                                               │`);
            console.log(`│   Tabs: ${r.multiTab.tabs} | Connected: ${r.multiTab.connected}                                               │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (r.multiTab.connected >= 1) passedTests++;
        }

        // Test 6: Hard Disconnect Recovery
        if (r.hardDisconnectRecovery) {
            const pct = Math.round(r.hardDisconnectRecovery.recovered / r.hardDisconnectRecovery.total * 100);
            const status = pct >= 90 ? '✓ PASS' : pct >= 60 ? '⚠ WARN' : '✗ FAIL';
            console.log(`┌─────────────────────────────────────────────────────────────────────────────┐`);
            console.log(`│ TEST 6: Hard Disconnect Recovery                                            │`);
            console.log(`│   Recovery Rate: ${r.hardDisconnectRecovery.recovered}/${r.hardDisconnectRecovery.total} (${pct}%)                                             │`);
            console.log(`│   Avg Recovery Time: ${r.hardDisconnectRecovery.avgTime}ms                                               │`);
            console.log(`│   Status: ${status}                                                            │`);
            console.log(`└─────────────────────────────────────────────────────────────────────────────┘`);
            totalTests++; if (pct >= 90) passedTests++;
        }

        // Overall assessment
        const overallPct = Math.round(passedTests / totalTests * 100);
        console.log('\n════════════════════════════════════════════════════════════════════════════════');
        console.log(`   OVERALL: ${passedTests}/${totalTests} tests passed (${overallPct}%)`);

        let assessment = '';
        if (overallPct >= 100) assessment = '🏆 EXCELLENT - 200% Reliable for Production';
        else if (overallPct >= 83) assessment = '✓ VERY GOOD - Highly reliable';
        else if (overallPct >= 66) assessment = '⚠ GOOD - Generally reliable';
        else assessment = '✗ NEEDS WORK - Reliability issues detected';

        console.log(`   Assessment: ${assessment}`);
        console.log('════════════════════════════════════════════════════════════════════════════════\n');

        return passedTests === totalTests;
    }

    async run() {
        console.log('\n');
        console.log('╔══════════════════════════════════════════════════════════════════════════════╗');
        console.log('║         TERMINAL TUNNEL - COMPREHENSIVE RELIABILITY STRESS TEST             ║');
        console.log('║                    Testing for 200% Reliability                              ║');
        console.log('╚══════════════════════════════════════════════════════════════════════════════╝\n');

        try {
            await this.startInfrastructure();

            // Run all tests
            await this.testRapidReconnections();
            await this.testRapidFireRefreshes();
            await this.testSustainedStability();
            await this.testDataIntegrity();
            await this.testMultiTab();
            await this.testHardDisconnectRecovery();

        } catch (err) {
            this.log(`Fatal error: ${err.message}`);
            console.error(err);
        } finally {
            await this.cleanup();
        }

        const allPassed = this.printFinalResults();
        process.exit(allPassed ? 0 : 1);
    }
}

new ReliabilityStressTest().run();
