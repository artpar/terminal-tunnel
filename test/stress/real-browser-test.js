const { chromium } = require('playwright');
const { spawn } = require('child_process');

async function realTest() {
    console.log('=== Real Browser Test (Non-Headless) ===\n');

    // Start server
    console.log('1. Starting server...');
    const server = spawn('../../tt', ['start', '-p', 'testpass12345'], {
        cwd: __dirname,
        stdio: ['pipe', 'pipe', 'pipe']
    });

    let serverOutput = '';
    let code = null, url = null;

    server.stdout.on('data', (data) => {
        const text = data.toString();
        serverOutput += text;
        // Show ALL server output
        text.split('\n').forEach(line => {
            const clean = line.replace(/\x1b\[[0-9;]*m/g, '').trim();
            if (clean && clean.length > 2) {
                console.log(`[S] ${clean}`);
            }
        });
        const codeMatch = serverOutput.match(/Code:\s+(\w+)/);
        const urlMatch = serverOutput.match(/(https:\/\/[^\s]+\?c=\w+)/);
        if (codeMatch) code = codeMatch[1];
        if (urlMatch) url = urlMatch[1];
    });

    // Wait for ready
    for (let i = 0; i < 20 && !url; i++) await new Promise(r => setTimeout(r, 500));
    if (!url) { console.log('Server failed'); server.kill(); return; }

    console.log(`\n2. URL: ${url}\n`);

    // Launch VISIBLE browser
    const browser = await chromium.launch({
        headless: false,  // VISIBLE
        slowMo: 100       // Slow down to see what happens
    });
    const page = await browser.newPage();

    // Log EVERYTHING from browser
    page.on('console', msg => console.log(`[B:${msg.type()}] ${msg.text()}`));
    page.on('pageerror', err => console.log(`[B:ERROR] ${err.message}`));

    console.log('3. Opening page...');
    await page.goto(url);
    await page.waitForTimeout(3000);

    console.log('4. Filling password...');
    await page.fill('input[type="password"]', 'testpass12345');

    console.log('5. Clicking Connect...');
    await page.click('button:has-text("Connect")');

    console.log('6. Watching for 60 seconds (Ctrl+C to stop)...\n');

    let connectCount = 0, disconnectCount = 0;

    for (let i = 0; i < 60; i++) {
        await page.waitForTimeout(1000);

        const state = await page.evaluate(() => {
            const body = document.body.innerText;
            return {
                connecting: body.includes('Connecting'),
                disconnected: body.includes('Disconnected'),
                hasTerminal: document.querySelector('.xterm-screen')?.textContent?.length || 0
            };
        });

        if (state.connecting) {
            console.log(`[${i}s] CONNECTING...`);
        } else if (state.disconnected) {
            disconnectCount++;
            console.log(`[${i}s] DISCONNECTED (count: ${disconnectCount})`);
        } else if (state.hasTerminal > 100) {
            if (connectCount === 0) connectCount++;
            console.log(`[${i}s] CONNECTED (terminal: ${state.hasTerminal} chars)`);
        }
    }

    console.log(`\n=== RESULT: ${connectCount} connects, ${disconnectCount} disconnects ===`);

    await browser.close();
    server.kill('SIGINT');
}

realTest().catch(e => console.error(e));
