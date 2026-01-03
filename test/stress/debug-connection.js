const { chromium } = require('playwright');

async function debugConnection() {
    const CODE = process.argv[2] || 'L2MJGDP6';
    const PASSWORD = process.argv[3] || 'testpass12345';
    const URL = `https://artpar.github.io/terminal-tunnel/?c=${CODE}`;

    console.log(`\n=== Debug Connection Test ===`);
    console.log(`Code: ${CODE}`);
    console.log(`URL: ${URL}\n`);

    const browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();

    // Capture ALL console logs
    page.on('console', msg => {
        const type = msg.type().toUpperCase().padEnd(7);
        console.log(`[BROWSER ${type}] ${msg.text()}`);
    });

    // Capture page errors
    page.on('pageerror', err => {
        console.log(`[PAGE ERROR] ${err.message}`);
    });

    // Capture requests
    page.on('requestfailed', req => {
        console.log(`[REQ FAILED] ${req.url()} - ${req.failure()?.errorText}`);
    });

    console.log('1. Navigating to page...');
    await page.goto(URL);
    await page.waitForTimeout(2000);

    console.log('\n2. Taking snapshot of initial state...');
    const initialState = await page.evaluate(() => document.body.innerText.substring(0, 300));
    console.log(`   Page content: ${initialState.substring(0, 150)}...`);

    console.log('\n3. Entering password...');
    try {
        await page.waitForSelector('input[type="password"]', { timeout: 5000 });
        await page.fill('input[type="password"]', PASSWORD);
        console.log('   Password entered');
    } catch (e) {
        console.log(`   Error: ${e.message}`);
    }

    console.log('\n4. Clicking Connect...');
    try {
        await page.click('button:has-text("Connect")');
        console.log('   Clicked');
    } catch (e) {
        console.log(`   Error: ${e.message}`);
    }

    console.log('\n5. Monitoring connection for 15 seconds...');
    for (let i = 0; i < 15; i++) {
        await page.waitForTimeout(1000);

        // Check connection state
        const state = await page.evaluate(() => {
            if (typeof window.sessionManager !== 'undefined') {
                const session = window.sessionManager?.getActiveSession?.();
                if (session) {
                    return {
                        hasSession: true,
                        pc: session.pc?.connectionState || 'no pc',
                        ice: session.pc?.iceConnectionState || 'no ice',
                        dc: session.dc?.readyState || 'no dc',
                        dcLabel: session.dc?.label || 'no label'
                    };
                }
            }
            // Try to find any WebRTC state info
            return { hasSession: false, note: 'sessionManager not exposed' };
        });

        console.log(`   [${i+1}s] State: ${JSON.stringify(state)}`);

        // Check if terminal is visible
        const hasTerminal = await page.evaluate(() => {
            const term = document.querySelector('.xterm-screen');
            return term ? term.textContent.length : 0;
        });
        if (hasTerminal > 10) {
            console.log(`   ✓ Terminal has content (${hasTerminal} chars)`);
            break;
        }
    }

    console.log('\n6. Final page state...');
    const finalContent = await page.evaluate(() => {
        const term = document.querySelector('.xterm-screen');
        if (term) return `TERMINAL: ${term.textContent.substring(0, 100)}`;
        return `PAGE: ${document.body.innerText.substring(0, 200)}`;
    });
    console.log(`   ${finalContent}`);

    await browser.close();
    console.log('\n=== Test Complete ===\n');
}

debugConnection().catch(e => {
    console.error('Fatal error:', e);
    process.exit(1);
});
