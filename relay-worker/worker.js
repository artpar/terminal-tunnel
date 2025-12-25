// Cloudflare Worker for terminal-tunnel relay
// Deploy: wrangler deploy

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
