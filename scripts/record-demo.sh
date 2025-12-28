#!/bin/bash
# Record demo GIF for terminal-tunnel README
# Runs on secondary monitor to avoid disturbing your workflow
# Uses Puppeteer for automation + screencapture for screenshots with browser chrome

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
DEMO_DIR="$PROJECT_DIR/docs/demo-frames"
OUTPUT_GIF="$PROJECT_DIR/docs/demo.gif"
PASSWORD="demopassword123"

# Secondary monitor position (adjust if needed)
WINDOW_X=3500
WINDOW_Y=100
WINDOW_WIDTH=900
WINDOW_HEIGHT=700

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --primary)
            WINDOW_X=100
            WINDOW_Y=100
            shift
            ;;
        --x)
            WINDOW_X="$2"
            shift 2
            ;;
        --y)
            WINDOW_Y="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --primary     Use primary monitor (default: secondary)"
            echo "  --x VALUE     Set window X position"
            echo "  --y VALUE     Set window Y position"
            echo "  -h, --help    Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo "=== Terminal Tunnel Demo Recorder ==="
echo "Window position: ${WINDOW_X}, ${WINDOW_Y}"
echo ""

# Cleanup function
cleanup() {
    echo "Cleaning up..."
    [[ -n "$TUNNEL_PID" ]] && kill "$TUNNEL_PID" 2>/dev/null || true
    rm -f "$PROJECT_DIR/tunnel" /tmp/tunnel-demo.log /tmp/cli-frame.html /tmp/puppeteer-demo.mjs 2>/dev/null || true
}
trap cleanup EXIT

# Check for node
if ! command -v node &> /dev/null; then
    echo "ERROR: Node.js not found. Install with: brew install node"
    exit 1
fi

# Create output directory
rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR"

# Build fresh binary
echo "[1/8] Building tunnel binary..."
cd "$PROJECT_DIR"
go build -o tunnel ./cmd/terminal-tunnel

# Start tunnel server
echo "[2/8] Starting tunnel server..."
./tunnel start --password "$PASSWORD" > /tmp/tunnel-demo.log 2>&1 &
TUNNEL_PID=$!

# Wait for server to be ready
echo "[3/8] Waiting for server to start..."
for i in {1..30}; do
    if grep -q "Code:" /tmp/tunnel-demo.log 2>/dev/null; then
        break
    fi
    sleep 0.5
done

# Extract session code
CODE=$(grep -o 'Code:[[:space:]]*[A-Z0-9]*' /tmp/tunnel-demo.log | head -1 | awk '{print $2}')
if [[ -z "$CODE" ]]; then
    echo "ERROR: Failed to get session code"
    cat /tmp/tunnel-demo.log
    exit 1
fi

URL="https://artpar.github.io/terminal-tunnel/?c=$CODE"
echo "Session code: $CODE"
echo "URL: $URL"

# Create CLI frame HTML
echo "[4/8] Creating CLI frame..."
cat > /tmp/cli-frame.html << HTMLEOF
<!DOCTYPE html>
<html>
<head>
<style>
body {
    background: #1a1b26;
    color: #a9b1d6;
    font-family: 'SF Mono', 'Monaco', 'Menlo', monospace;
    font-size: 14px;
    padding: 20px;
    margin: 0;
    line-height: 1.4;
}
.prompt { color: #7aa2f7; }
.success { color: #9ece6a; }
.box { color: #bb9af7; }
.url { color: #7dcfff; }
pre { margin: 0; white-space: pre-wrap; }
</style>
</head>
<body>
<pre><span class="prompt">\$ tt start --password ************ --public</span>

<span class="success">Using signaling method: Short Code
✓ TURN relay enabled for symmetric NAT traversal
✓ Public IP discovered via STUN</span>

<span class="box">╔══════════════════════════════════════════════════╗
║           Terminal Tunnel - Ready                ║
╠══════════════════════════════════════════════════╣
║  Code:     $CODE                              ║
║  Password: ************                          ║
╚══════════════════════════════════════════════════╝</span>

█████████████████████████████████████
█████████████████████████████████████
████ ▄▄▄▄▄ █▀█▄▀▀   █ ▄▄██ ▄▄▄▄▄ ████
████ █   █ █▄█▄▀▀█▀▀▀▄▀▄▄█ █   █ ████
████ █▄▄▄█ █ █▄███▄ ▄  ▀ █ █▄▄▄█ ████
████▄▄▄▄▄▄▄█ ▀▄█ █▄▀▄▀ ▀▄█▄▄▄▄▄▄▄████
████ ▄▀ ██▄▄██▄▄█ ▄█▀█▄█▀▀ ▄▄▀▄▄▀████
████▀█▄▀█▀▄▀ ▄▄▀▀ █▀ ▄▀ ████▄▄  █████
████ ▀▀█▄ ▄▀▄▀▀██ █▄  ▀▀▀▄▄▄█▄█▄▄████
████  ▀▀ ▄▄▀▀█ ▄ ▀▄▀  █▀█▀ ▄▄ ▄ ▄████
████▄▄ ▄██▄▀   █ █ ▄▀▄██▄▀▀ █▀█▄▀████
████▄█▀ ▄ ▄ █▄ █▄▄▀▀▀▀▀█  █▀███  ████
████▄██▄▄█▄▄   ▀ ▄▀█ ▀▀█ ▄▄▄ █ ▀▀████
████ ▄▄▄▄▄ █▄█▀▀ ▀██▀▄▄█ █▄█ ▀▀ █████
████ █   █ █▀ ▀▀▀▄█▄▄▄█▀▄▄ ▄ ▀▀▄▄████
████ █▄▄▄█ █▀█▄ ▀ ▀▀█ ▀▀ ███▄▄▄▀▄████
████▄▄▄▄▄▄▄█▄▄█▄██▄▄█▄██▄▄▄█▄▄█▄█████
█████████████████████████████████████
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀

<span class="url">  $URL</span>

  Waiting for client to connect...
  Press Ctrl+C to cancel
</pre>
</body>
</html>
HTMLEOF

# Create Puppeteer automation script
echo "[5/8] Creating automation script..."
cat > /tmp/puppeteer-demo.mjs << 'JSEOF'
import puppeteer from 'puppeteer';
import { exec } from 'child_process';
import { promisify } from 'util';

const execAsync = promisify(exec);

const url = process.argv[2];
const password = process.argv[3];
const demoDir = process.argv[4];
const windowX = parseInt(process.argv[5] || '3500');
const windowY = parseInt(process.argv[6] || '100');
const windowW = 900;
const windowH = 700;

async function screenshot(name) {
    // Use macOS screencapture to get browser chrome
    const cmd = `screencapture -R ${windowX},${windowY},${windowW},${windowH} "${demoDir}/${name}"`;
    await execAsync(cmd);
    console.log(`  Captured: ${name}`);
}

async function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

(async () => {
    console.log('Launching browser...');
    const browser = await puppeteer.launch({
        headless: false,
        args: [
            `--window-position=${windowX},${windowY}`,
            `--window-size=${windowW},${windowH}`,
            '--disable-infobars',
            '--no-first-run',
            '--disable-extensions'
        ]
    });

    const page = await browser.newPage();
    await page.setViewport({ width: windowW - 20, height: windowH - 120 });

    // Screenshot 1: CLI frame
    console.log('Loading CLI frame...');
    await page.goto('file:///tmp/cli-frame.html');
    await sleep(1500);
    await screenshot('00-cli-start.png');

    // Screenshot 2: Connect page
    console.log('Navigating to terminal-tunnel...');
    await page.goto(url);
    await sleep(2500);
    await screenshot('01-connect.png');

    // Enter password and connect
    console.log('Entering password...');
    try {
        await page.waitForSelector('input', { timeout: 5000 });
        const inputs = await page.$$('input');
        for (const input of inputs) {
            const placeholder = await input.evaluate(el => el.placeholder || '');
            if (placeholder.toLowerCase().includes('password')) {
                await input.type(password);
                break;
            }
        }
    } catch (e) {
        console.log('  Could not find password field');
    }

    console.log('Clicking Connect...');
    try {
        const buttons = await page.$$('button');
        for (const btn of buttons) {
            const text = await btn.evaluate(el => el.textContent || '');
            if (text.includes('Connect')) {
                await btn.click();
                break;
            }
        }
    } catch (e) {
        console.log('  Could not find Connect button');
    }

    console.log('Waiting for connection...');
    await sleep(5000);
    await screenshot('02-connected.png');

    // Type whoami
    console.log('Running whoami...');
    try {
        await page.keyboard.type('whoami');
        await page.keyboard.press('Enter');
        await sleep(2000);
        await screenshot('03-whoami.png');

        // Type ls
        console.log('Running ls...');
        await page.keyboard.type('ls');
        await page.keyboard.press('Enter');
        await sleep(2000);
        await screenshot('04-ls.png');
    } catch (e) {
        console.log('  Error typing commands:', e.message);
    }

    console.log('Done! Closing browser...');
    await browser.close();
})();
JSEOF

# Run Puppeteer
echo "[6/8] Running browser automation..."
cd /tmp
npm init -y > /dev/null 2>&1 || true
npm install puppeteer --save > /dev/null 2>&1
node /tmp/puppeteer-demo.mjs "$URL" "$PASSWORD" "$DEMO_DIR" "$WINDOW_X" "$WINDOW_Y"

# Check if screenshots were captured
if [[ ! -f "$DEMO_DIR/00-cli-start.png" ]]; then
    echo "ERROR: Screenshots not captured"
    exit 1
fi

echo "[7/8] Screenshots captured"

# Create animated GIF
echo "[8/8] Creating animated GIF..."
cd "$PROJECT_DIR"
if command -v magick &> /dev/null; then
    magick -delay 200 -loop 0 \
        "$DEMO_DIR/00-cli-start.png" \
        "$DEMO_DIR/01-connect.png" \
        "$DEMO_DIR/02-connected.png" \
        "$DEMO_DIR/03-whoami.png" \
        "$DEMO_DIR/04-ls.png" \
        "$OUTPUT_GIF" 2>/dev/null
elif command -v convert &> /dev/null; then
    convert -delay 200 -loop 0 \
        "$DEMO_DIR/00-cli-start.png" \
        "$DEMO_DIR/01-connect.png" \
        "$DEMO_DIR/02-connected.png" \
        "$DEMO_DIR/03-whoami.png" \
        "$DEMO_DIR/04-ls.png" \
        "$OUTPUT_GIF" 2>/dev/null
else
    echo "ERROR: ImageMagick not found. Install with: brew install imagemagick"
    exit 1
fi

echo ""
echo "=== Demo recording complete! ==="
echo "Output: $OUTPUT_GIF"
echo "Size: $(du -h "$OUTPUT_GIF" | cut -f1)"
echo ""
echo "Frames captured:"
ls -lh "$DEMO_DIR"/*.png
