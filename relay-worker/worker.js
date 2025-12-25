// Cloudflare Worker for terminal-tunnel relay
// Deploy: wrangler deploy

const landingPage = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Terminal Tunnel - P2P Terminal Sharing</title>
  <meta name="theme-color" content="#1a1a2e">
  <meta name="apple-mobile-web-app-capable" content="yes">
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
  <link rel="manifest" href="/manifest.json">
  <link rel="apple-touch-icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🔗</text></svg>">
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body {
      min-height: 100vh;
      background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      color: #fff;
      padding: 40px 20px;
    }
    .container { max-width: 600px; margin: 0 auto; }
    .logo { font-size: 64px; text-align: center; margin-bottom: 10px; }
    h1 { text-align: center; color: #e94560; margin-bottom: 10px; }
    .tagline { text-align: center; color: #888; margin-bottom: 40px; }

    .card {
      background: #16213e; border-radius: 12px; padding: 30px;
      margin-bottom: 20px;
    }
    .card h2 { color: #4ecdc4; margin-bottom: 15px; font-size: 18px; }
    .card p { color: #aaa; line-height: 1.6; margin-bottom: 15px; }

    .code-form { display: flex; gap: 10px; margin-bottom: 20px; }
    .code-form input {
      flex: 1; padding: 15px; border: none; border-radius: 8px;
      background: #0f3460; color: #fff; font-size: 20px;
      text-align: center; letter-spacing: 4px; text-transform: uppercase;
      font-family: monospace;
    }
    .code-form input:focus { outline: 2px solid #e94560; }
    .code-form button {
      padding: 15px 25px; border: none; border-radius: 8px;
      background: #e94560; color: #fff; font-size: 16px;
      cursor: pointer; font-weight: 600;
    }
    .code-form button:hover { background: #ff6b6b; }

    code {
      background: #0f3460; padding: 3px 8px; border-radius: 4px;
      font-family: monospace; color: #4ecdc4;
    }
    pre {
      background: #0f3460; padding: 15px; border-radius: 8px;
      overflow-x: auto; margin: 15px 0;
    }
    pre code { background: none; padding: 0; }

    .steps { counter-reset: step; }
    .step {
      display: flex; gap: 15px; margin-bottom: 20px;
      padding-bottom: 20px; border-bottom: 1px solid #0f3460;
    }
    .step:last-child { border-bottom: none; margin-bottom: 0; padding-bottom: 0; }
    .step-num {
      width: 30px; height: 30px; background: #e94560;
      border-radius: 50%; display: flex; align-items: center;
      justify-content: center; font-weight: bold; flex-shrink: 0;
    }
    .step-content { flex: 1; }
    .step-content h3 { margin-bottom: 8px; }
    .step-content p { color: #888; }

    .features { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; }
    .feature { background: #0f3460; padding: 15px; border-radius: 8px; text-align: center; }
    .feature-icon { font-size: 24px; margin-bottom: 8px; }
    .feature-text { color: #888; font-size: 14px; }

    a { color: #4ecdc4; }
    .github { text-align: center; margin-top: 30px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="logo">🔗</div>
    <h1>Terminal Tunnel</h1>
    <p class="tagline">P2P terminal sharing with end-to-end encryption</p>

    <div class="card">
      <h2>🎯 Join a Session</h2>
      <p>Enter the 6-character code from the host:</p>
      <form class="code-form" onsubmit="go(event)">
        <input type="text" id="code" maxlength="6" placeholder="ABC123" required>
        <button type="submit">Connect</button>
      </form>
    </div>

    <div class="card">
      <h2>🚀 Quick Start</h2>
      <div class="steps">
        <div class="step">
          <div class="step-num">1</div>
          <div class="step-content">
            <h3>Install</h3>
            <pre><code>go install github.com/artpar/terminal-tunnel/cmd/terminal-tunnel@latest</code></pre>
          </div>
        </div>
        <div class="step">
          <div class="step-num">2</div>
          <div class="step-content">
            <h3>Share your terminal</h3>
            <pre><code>terminal-tunnel serve -p mypassword</code></pre>
          </div>
        </div>
        <div class="step">
          <div class="step-num">3</div>
          <div class="step-content">
            <h3>Share the code</h3>
            <p>Give the 6-character code and password to your friend</p>
          </div>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>✨ Features</h2>
      <div class="features">
        <div class="feature">
          <div class="feature-icon">🔒</div>
          <div class="feature-text">E2E Encrypted</div>
        </div>
        <div class="feature">
          <div class="feature-icon">🌐</div>
          <div class="feature-text">Works on any network</div>
        </div>
        <div class="feature">
          <div class="feature-icon">📱</div>
          <div class="feature-text">Mobile friendly</div>
        </div>
        <div class="feature">
          <div class="feature-icon">⚡</div>
          <div class="feature-text">P2P (low latency)</div>
        </div>
      </div>
    </div>

    <p class="github">
      <a href="https://github.com/artpar/terminal-tunnel">View on GitHub</a>
    </p>
  </div>

  <script>
    function go(e) {
      e.preventDefault();
      const code = document.getElementById('code').value.toUpperCase();
      window.location.href = 'https://artpar.github.io/terminal-tunnel/?c=' + code;
    }
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js');
    }
  </script>
</body>
</html>`;

const manifest = JSON.stringify({
  name: 'Terminal Tunnel',
  short_name: 'TermTunnel',
  description: 'P2P terminal sharing with end-to-end encryption',
  start_url: '/',
  display: 'standalone',
  background_color: '#1a1a2e',
  theme_color: '#1a1a2e',
  icons: [
    {
      src: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect fill="%231a1a2e" width="100" height="100" rx="20"/><text x="50" y="50" text-anchor="middle" dominant-baseline="central" font-size="60">🔗</text></svg>',
      sizes: '512x512',
      type: 'image/svg+xml',
      purpose: 'any maskable'
    }
  ]
});

const serviceWorker = "self.addEventListener('install', e => self.skipWaiting());" +
  "self.addEventListener('activate', e => e.waitUntil(clients.claim()));" +
  "self.addEventListener('fetch', e => {" +
  "  if (e.request.mode === 'navigate') {" +
  "    e.respondWith(fetch(e.request).catch(() => caches.match('/')));" +
  "  }" +
  "});";

const ALPHABET = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
const CODE_LENGTH = 6;
const EXPIRY_SECONDS = 300; // 5 minutes

function generateCode() {
  let code = '';
  for (let i = 0; i < CODE_LENGTH; i++) {
    code += ALPHABET[Math.floor(Math.random() * ALPHABET.length)];
  }
  return code;
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;

    // CORS headers
    const corsHeaders = {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type',
    };

    if (request.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders });
    }

    // Landing page
    if (path === '/' || path === '') {
      return new Response(landingPage, {
        headers: { 'Content-Type': 'text/html' }
      });
    }

    // PWA manifest
    if (path === '/manifest.json') {
      return new Response(manifest, {
        headers: { 'Content-Type': 'application/manifest+json' }
      });
    }

    // Service worker
    if (path === '/sw.js') {
      return new Response(serviceWorker, {
        headers: { 'Content-Type': 'application/javascript' }
      });
    }

    // Health check
    if (path === '/health') {
      return new Response('OK', { headers: corsHeaders });
    }

    // POST /session - create new session
    if (path === '/session' && request.method === 'POST') {
      const { sdp, salt } = await request.json();
      if (!sdp) {
        return new Response(JSON.stringify({ error: 'SDP required' }), {
          status: 400,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      const code = generateCode();
      await env.SESSIONS.put(code, JSON.stringify({ sdp, salt, answer: null }), {
        expirationTtl: EXPIRY_SECONDS
      });

      return new Response(JSON.stringify({ code, expires_in: EXPIRY_SECONDS }), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }

    // GET /session/{code} - get session SDP
    const sessionMatch = path.match(/^\/session\/([A-Z0-9]+)$/i);
    if (sessionMatch && request.method === 'GET') {
      const code = sessionMatch[1].toUpperCase();
      const data = await env.SESSIONS.get(code);

      if (!data) {
        return new Response(JSON.stringify({ error: 'Session not found' }), {
          status: 404,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      const session = JSON.parse(data);
      return new Response(JSON.stringify({ sdp: session.sdp, salt: session.salt }), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }

    // POST /session/{code}/answer - submit answer
    const answerPostMatch = path.match(/^\/session\/([A-Z0-9]+)\/answer$/i);
    if (answerPostMatch && request.method === 'POST') {
      const code = answerPostMatch[1].toUpperCase();
      const { sdp } = await request.json();

      const data = await env.SESSIONS.get(code);
      if (!data) {
        return new Response(JSON.stringify({ error: 'Session not found' }), {
          status: 404,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      const session = JSON.parse(data);
      session.answer = sdp;
      await env.SESSIONS.put(code, JSON.stringify(session), {
        expirationTtl: EXPIRY_SECONDS
      });

      return new Response(JSON.stringify({ status: 'ok' }), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }

    // GET /session/{code}/answer - poll for answer
    const answerGetMatch = path.match(/^\/session\/([A-Z0-9]+)\/answer$/i);
    if (answerGetMatch && request.method === 'GET') {
      const code = answerGetMatch[1].toUpperCase();
      const data = await env.SESSIONS.get(code);

      if (!data) {
        return new Response(JSON.stringify({ error: 'Session not found' }), {
          status: 404,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      const session = JSON.parse(data);
      if (session.answer) {
        return new Response(JSON.stringify({ sdp: session.answer }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      return new Response(JSON.stringify({ status: 'waiting' }), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }

    return new Response('Not found', { status: 404, headers: corsHeaders });
  }
};
