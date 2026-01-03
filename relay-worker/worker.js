// Cloudflare Worker for terminal-tunnel relay (D1 backend)
// Deploy: wrangler deploy

const DEFAULT_CLIENT_URL = 'https://artpar.github.io/terminal-tunnel';

function getLandingPage(clientUrl) {
  return `<!DOCTYPE html>
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
      <input type="text" id="code" maxlength="8" placeholder="CODE" required>
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
        <code>tt daemon start && tt start -p yourpassword</code>
      </div>
    </div>
    <div class="footer"><a href="https://github.com/artpar/terminal-tunnel">GitHub</a></div>
  </div>
  <script>
    function go(e) {
      e.preventDefault();
      const code = document.getElementById('code').value.toUpperCase();
      window.location.href = '${clientUrl}/?c=' + code;
    }
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js');
  </script>
</body>
</html>`;
}

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
const CODE_LENGTH = 8;
const EXPIRY_SECONDS = 300; // 5 minutes

// Default STUN servers (free, public)
const DEFAULT_STUN_SERVERS = [
  'stun:stun.l.google.com:19302',
  'stun:stun1.l.google.com:19302',
  'stun:stun2.l.google.com:19302',
  'stun:stun3.l.google.com:19302',
  'stun:stun4.l.google.com:19302'
];

// Build ICE servers configuration
function getICEServers(env) {
  const servers = [
    { urls: DEFAULT_STUN_SERVERS }
  ];

  // Add TURN server if configured via environment variables
  // Set these in wrangler.toml or Cloudflare dashboard:
  //   TURN_URL = "turn:your-server.com:3478"
  //   TURN_USERNAME = "your-username"
  //   TURN_PASSWORD = "your-password"
  if (env.TURN_URL) {
    const turnUrls = env.TURN_URL.split(',').map(u => u.trim());
    servers.push({
      urls: turnUrls,
      username: env.TURN_USERNAME || '',
      credential: env.TURN_PASSWORD || ''
    });
  }

  return servers;
}

function generateCode() {
  let code = '';
  for (let i = 0; i < CODE_LENGTH; i++) {
    code += ALPHABET[Math.floor(Math.random() * ALPHABET.length)];
  }
  return code;
}

function getCorsHeaders(request) {
  const origin = request.headers.get('Origin');
  return {
    'Access-Control-Allow-Origin': origin || '*',
    'Access-Control-Allow-Methods': 'GET, POST, PUT, PATCH, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
  };
}

// Check if session is expired
function isExpired(createdAt) {
  const now = Math.floor(Date.now() / 1000);
  return (now - createdAt) > EXPIRY_SECONDS;
}

export default {
  // Scheduled cleanup of expired sessions (runs hourly via cron)
  async scheduled(event, env, ctx) {
    const cutoff = Math.floor(Date.now() / 1000) - EXPIRY_SECONDS;
    const result = await env.DB.prepare(
      'DELETE FROM sessions WHERE created_at < ?'
    ).bind(cutoff).run();
    console.log(`Cleanup: deleted ${result.meta.changes} expired sessions`);
  },

  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;
    const clientUrl = env.CLIENT_URL || DEFAULT_CLIENT_URL;
    const corsHeaders = getCorsHeaders(request);

    if (request.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders });
    }

    try {
      // Landing page
      if (path === '/' || path === '') {
        return new Response(getLandingPage(clientUrl), {
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

      // ICE servers configuration endpoint
      // Returns STUN servers + TURN servers (if configured)
      if (path === '/ice-servers') {
        const iceServers = getICEServers(env);
        const hasTurn = iceServers.some(s =>
          Array.isArray(s.urls)
            ? s.urls.some(u => u.startsWith('turn:'))
            : s.urls?.startsWith('turn:')
        );
        return new Response(JSON.stringify({
          iceServers,
          hasTurn,
          message: hasTurn
            ? 'TURN relay configured for symmetric NAT support'
            : 'STUN-only mode (configure TURN_URL for symmetric NAT)'
        }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // POST /session - create new session
      if (path === '/session' && request.method === 'POST') {
        const { sdp, salt, viewer_sdp, viewer_key } = await request.json();
        if (!sdp) {
          return new Response(JSON.stringify({ error: 'SDP required' }), {
            status: 400,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        const code = generateCode();
        const now = Math.floor(Date.now() / 1000);

        await env.DB.prepare(
          'INSERT INTO sessions (code, sdp, salt, created_at) VALUES (?, ?, ?, ?)'
        ).bind(code, sdp, salt, now).run();

        const response = { code, expires_in: EXPIRY_SECONDS };

        // If viewer session requested, create it with V suffix
        if (viewer_sdp && viewer_key) {
          const viewerCode = code + 'V';
          await env.DB.prepare(
            'INSERT INTO sessions (code, sdp, key, read_only, created_at) VALUES (?, ?, ?, 1, ?)'
          ).bind(viewerCode, viewer_sdp, viewer_key, now).run();
          response.viewer_code = viewerCode;
        }

        return new Response(JSON.stringify(response), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // GET /session/{code} - get session SDP
      const sessionMatch = path.match(/^\/session\/([A-Z0-9]+)$/i);
      if (sessionMatch && request.method === 'GET') {
        const code = sessionMatch[1].toUpperCase();
        const session = await env.DB.prepare(
          'SELECT * FROM sessions WHERE code = ?'
        ).bind(code).first();

        if (!session || isExpired(session.created_at)) {
          return new Response(JSON.stringify({ error: 'Session not found' }), {
            status: 404,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        // Viewer session
        if (session.read_only) {
          return new Response(JSON.stringify({
            sdp: session.sdp,
            key: session.key,
            read_only: true,
            used: session.answer !== null
          }), {
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        // Normal control session
        return new Response(JSON.stringify({
          sdp: session.sdp,
          salt: session.salt,
          used: session.answer !== null
        }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // PUT /session/{code} - update session (for reconnection)
      const updateMatch = path.match(/^\/session\/([A-Z0-9]+)$/i);
      if (updateMatch && request.method === 'PUT') {
        const code = updateMatch[1].toUpperCase();
        const { sdp, salt } = await request.json();

        const existing = await env.DB.prepare(
          'SELECT code FROM sessions WHERE code = ?'
        ).bind(code).first();

        if (!existing) {
          return new Response(JSON.stringify({ error: 'Session not found' }), {
            status: 404,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        const now = Math.floor(Date.now() / 1000);
        await env.DB.prepare(
          'UPDATE sessions SET sdp = ?, salt = ?, answer = NULL, created_at = ? WHERE code = ?'
        ).bind(sdp, salt, now, code).run();

        return new Response(JSON.stringify({ status: 'ok' }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // PATCH /session/{code} - heartbeat (keep session alive)
      const heartbeatMatch = path.match(/^\/session\/([A-Z0-9]+)$/i);
      if (heartbeatMatch && request.method === 'PATCH') {
        const code = heartbeatMatch[1].toUpperCase();

        const existing = await env.DB.prepare(
          'SELECT code FROM sessions WHERE code = ?'
        ).bind(code).first();

        if (!existing) {
          return new Response(JSON.stringify({ error: 'Session not found' }), {
            status: 404,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        const now = Math.floor(Date.now() / 1000);
        await env.DB.prepare(
          'UPDATE sessions SET created_at = ? WHERE code = ?'
        ).bind(now, code).run();

        return new Response(JSON.stringify({ status: 'ok' }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // POST /session/{code}/answer - submit answer
      const answerPostMatch = path.match(/^\/session\/([A-Z0-9]+)\/answer$/i);
      if (answerPostMatch && request.method === 'POST') {
        const code = answerPostMatch[1].toUpperCase();
        const { sdp } = await request.json();

        const session = await env.DB.prepare(
          'SELECT code, created_at FROM sessions WHERE code = ?'
        ).bind(code).first();

        if (!session || isExpired(session.created_at)) {
          return new Response(JSON.stringify({ error: 'Session not found' }), {
            status: 404,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

        await env.DB.prepare(
          'UPDATE sessions SET answer = ? WHERE code = ?'
        ).bind(sdp, code).run();

        return new Response(JSON.stringify({ status: 'ok' }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' }
        });
      }

      // GET /session/{code}/answer - poll for answer
      const answerGetMatch = path.match(/^\/session\/([A-Z0-9]+)\/answer$/i);
      if (answerGetMatch && request.method === 'GET') {
        const code = answerGetMatch[1].toUpperCase();
        const session = await env.DB.prepare(
          'SELECT answer, created_at FROM sessions WHERE code = ?'
        ).bind(code).first();

        if (!session || isExpired(session.created_at)) {
          return new Response(JSON.stringify({ error: 'Session not found' }), {
            status: 404,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' }
          });
        }

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

    } catch (error) {
      console.error('Worker error:', error.message, error.stack);
      return new Response(JSON.stringify({
        error: 'Internal server error',
        message: error.message
      }), {
        status: 500,
        headers: { ...corsHeaders, 'Content-Type': 'application/json' }
      });
    }
  }
};
