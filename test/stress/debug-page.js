#!/usr/bin/env node
const { chromium } = require('playwright');

async function debug() {
    const browser = await chromium.launch({ headless: true });
    const context = await browser.newContext({ bypassCSP: true });
    const page = await context.newPage();

    // Log all console messages
    page.on('console', msg => console.log(`[Console] ${msg.type()}: ${msg.text()}`));
    page.on('pageerror', err => console.log(`[Error] ${err.message}`));

    const url = 'http://localhost:8888/?c=TEST123';
    console.log(`Navigating to ${url}`);

    await page.goto(url);
    await page.waitForTimeout(3000);

    // Check what's on the page
    const html = await page.content();
    console.log('\n=== Page title ===');
    console.log(await page.title());

    console.log('\n=== Looking for form elements ===');
    const inputs = await page.$$('input');
    console.log(`Found ${inputs.length} input elements`);

    const buttons = await page.$$('button');
    console.log(`Found ${buttons.length} button elements`);

    const passwordInputs = await page.$$('.password-input');
    console.log(`Found ${passwordInputs.length} .password-input elements`);

    const formContainers = await page.$$('.form-container');
    console.log(`Found ${formContainers.length} .form-container elements`);

    // Check if tab bar exists
    const tabBars = await page.$$('.tab-bar');
    console.log(`Found ${tabBars.length} .tab-bar elements`);

    // Take screenshot
    await page.screenshot({ path: '/tmp/debug-page.png' });
    console.log('\nScreenshot saved to /tmp/debug-page.png');

    // Check session state
    const sessionState = await page.evaluate(() => {
        if (window.sessions && window.sessions.length > 0) {
            return window.sessions.map(s => ({
                code: s.code,
                status: s.status
            }));
        }
        return 'No sessions';
    });
    console.log('\nSession state:', JSON.stringify(sessionState, null, 2));

    await browser.close();
}

debug().catch(console.error);
