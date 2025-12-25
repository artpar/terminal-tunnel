// Cloudflare Worker for terminal-tunnel relay
// Deploy: wrangler deploy

const landingPage = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Terminal Tunnel</title>
  <meta name="theme-color" content="#000">
  <meta name="apple-mobile-web-app-capable" content="yes">
  <meta name="apple-mobile-web-app-status-bar-style" content="black">
  <link rel="manifest" href="/manifest.json">
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    html, body { height: 100%; overflow: hidden; }
    body {
      background: #000; color: #fff;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
      display: flex; align-items: center; justify-content: center;
    }
    .container { width: 100%; max-width: 400px; padding: 20px; text-align: center; }
    h1 { font-size: 28px; margin-bottom: 8px; font-weight: 500; }
    .tagline { color: #666; font-size: 14px; margin-bottom: 30px; }
    .code-form { display: flex; gap: 8px; margin-bottom: 30px; }
    .code-form input {
      flex: 1; padding: 14px; border: 1px solid #333; border-radius: 6px;
      background: #111; color: #fff; font-size: 18px;
      text-align: center; letter-spacing: 3px; text-transform: uppercase;
      font-family: monospace;
    }
    .code-form input:focus { outline: none; border-color: #fff; }
    .code-form input::placeholder { color: #444; }
    .code-form button {
      padding: 14px 20px; border: none; border-radius: 6px;
      background: #fff; color: #000; font-size: 14px;
      cursor: pointer; font-weight: 600;
    }
    .code-form button:hover { background: #ddd; }
    .divider { color: #333; font-size: 12px; margin-bottom: 20px; }
    .steps { text-align: left; background: #111; border-radius: 8px; padding: 16px; }
    .step { margin-bottom: 12px; font-size: 13px; }
    .step:last-child { margin-bottom: 0; }
    .step-label { color: #666; margin-bottom: 4px; }
    .step code { color: #888; font-family: monospace; font-size: 12px; word-break: break-all; }
    .footer { margin-top: 30px; }
    .footer a { color: #444; font-size: 12px; text-decoration: none; }
    .footer a:hover { color: #888; }
  </style>
</head>
<body>
  <div class="container">
    <h1>Terminal Tunnel</h1>
    <p class="tagline">P2P terminal sharing with E2E encryption</p>
    <form class="code-form" onsubmit="go(event)">
      <input type="text" id="code" maxlength="6" placeholder="CODE" required>
      <button type="submit">Connect</button>
    </form>
    <div class="divider">— or host —</div>
    <div class="steps">
      <div class="step">
        <div class="step-label">Install</div>
        <code>go install github.com/artpar/terminal-tunnel/cmd/terminal-tunnel@latest</code>
      </div>
      <div class="step">
        <div class="step-label">Run</div>
        <code>terminal-tunnel serve -p yourpassword</code>
      </div>
    </div>
    <div class="footer"><a href="https://github.com/artpar/terminal-tunnel">GitHub</a></div>
  </div>
  <script>
    function go(e) {
      e.preventDefault();
      const code = document.getElementById('code').value.toUpperCase();
      window.location.href = 'https://artpar.github.io/terminal-tunnel/?c=' + code;
    }
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js');
  </script>
</body>
</html>`;

const manifest = JSON.stringify({
  name: 'Terminal Tunnel',
  short_name: 'TermTunnel',
  description: 'P2P terminal sharing with E2E encryption',
  start_url: '/',
  display: 'standalone',
  background_color: '#000000',
  theme_color: '#000000',
  icons: [
    {
      src: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect fill="%23000" width="100" height="100" rx="20"/><text x="50" y="50" text-anchor="middle" dominant-baseline="central" font-size="50">%3E_%3C</text></svg>',
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
