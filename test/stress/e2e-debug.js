const { chromium } = require('playwright');
const { spawn } = require('child_process');

async function e2eDebug() {
    console.log('=== E2E Debug Test ===\n');

    // Start server
    console.log('1. Starting tt server...');
    const server = spawn('../../tt', ['start', '-p', 'testpass12345'], {
        cwd: __dirname,
        stdio: ['pipe', 'pipe', 'pipe']
    });

    let serverOutput = '';
    let code = null;
    let url = null;

    server.stdout.on('data', (data) => {
        const text = data.toString();
        serverOutput += text;
        // Log server events in real-time
        if (text.includes('[') || text.includes('✓') || text.includes('⚠') || text.includes('✗')) {
            text.split('\n').forEach(line => {
                if (line.trim() && (line.includes('[') || line.includes('✓') || line.includes('⚠'))) {
                    console.log(`[SERVER] ${line.trim()}`);
                }
            });
        }
        const codeMatch = serverOutput.match(/Code:\s+(\w+)/);
        const urlMatch = serverOutput.match(/(https:\/\/[^\s]+\?c=\w+)/);
        if (codeMatch) code = codeMatch[1];
        if (urlMatch) url = urlMatch[1];
    });

    server.stderr.on('data', (data) => {
        console.log(`[SERVER STDERR] ${data.toString().trim()}`);
    });

    // Wait for server to be ready
    for (let i = 0; i < 20; i++) {
        await new Promise(r => setTimeout(r, 500));
        if (code && url) break;
    }

    if (!code || !url) {
        console.log('ERROR: Server did not start properly');
        console.log('Output:', serverOutput);
        server.kill();
        process.exit(1);
    }

    console.log(`\n2. Server ready: ${code}`);
    console.log(`   URL: ${url}\n`);

    // Start browser
    console.log('3. Starting browser...');
    const browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();

    // Capture browser logs
    page.on('console', msg => {
        const text = msg.text();
        if (text.includes('[') || text.includes('WebRTC') || text.includes('ICE') ||
            text.includes('connection') || text.includes('DC') || text.includes('error') ||
            text.includes('disconnect') || text.includes('reconnect')) {
            console.log(`[BROWSER] ${text}`);
        }
    });

    page.on('pageerror', err => console.log(`[BROWSER ERROR] ${err.message}`));

    console.log('4. Navigating to URL...');
    await page.goto(url);
    await page.waitForTimeout(2000);

    console.log('5. Entering password and connecting...');
    await page.fill('input[type="password"]', 'testpass12345');
    await page.click('button:has-text("Connect")');

    console.log('6. Monitoring for 30 seconds...\n');

    let connectionCount = 0;
    let disconnectionCount = 0;
    let lastState = '';

    for (let i = 0; i < 30; i++) {
        await page.waitForTimeout(1000);

        // Check page state
        const state = await page.evaluate(() => {
            const statusEl = document.querySelector('[class*="status"]');
            const termEl = document.querySelector('.xterm-screen');
            const dialogEl = document.querySelector('[class*="dialog"], [class*="modal"]');

            return {
                hasTerminal: termEl ? termEl.textContent.length : 0,
                status: statusEl ? statusEl.textContent : 'unknown',
                hasDialog: dialogEl ? dialogEl.textContent.substring(0, 50) : null,
                bodyText: document.body.innerText.substring(0, 100)
            };
        });

        // Check for state changes
        const currentState = state.hasTerminal > 100 ? 'CONNECTED' :
                            state.bodyText.includes('Connecting') ? 'CONNECTING' :
                            state.bodyText.includes('Disconnected') ? 'DISCONNECTED' :
                            state.bodyText.includes('Enter password') ? 'WAITING' : 'UNKNOWN';

        if (currentState !== lastState) {
            console.log(`[${i}s] State changed: ${lastState} -> ${currentState}`);
            if (currentState === 'CONNECTED') connectionCount++;
            if (currentState === 'DISCONNECTED') disconnectionCount++;
            lastState = currentState;
        }

        // If we see a disconnect, log more details
        if (currentState === 'DISCONNECTED' || currentState === 'CONNECTING') {
            console.log(`[${i}s] Page state: ${JSON.stringify(state).substring(0, 150)}`);
        }

        // If stable connection for 5 seconds, success
        if (currentState === 'CONNECTED' && state.hasTerminal > 100) {
            console.log(`[${i}s] Terminal content length: ${state.hasTerminal}`);
            if (i > 5) {
                console.log('\n✓ Connection appears stable');
                break;
            }
        }
    }

    console.log(`\n=== Summary ===`);
    console.log(`Connections: ${connectionCount}`);
    console.log(`Disconnections: ${disconnectionCount}`);
    console.log(`Final state: ${lastState}`);

    if (disconnectionCount > 1) {
        console.log('\n❌ ISSUE CONFIRMED: Multiple disconnections detected');
        console.log('This indicates a reconnect loop problem');
    }

    // Cleanup
    await browser.close();
    server.kill('SIGINT');
    await new Promise(r => setTimeout(r, 1000));

    console.log('\n=== Test Complete ===');
}

e2eDebug().catch(e => {
    console.error('Fatal:', e);
    process.exit(1);
});
