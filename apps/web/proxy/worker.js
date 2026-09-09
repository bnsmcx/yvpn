/**
 * yVPN Tailscale proxy — Cloudflare Worker.
 *
 * Tailscale's API returns no CORS headers, so a browser cannot call it from a web
 * page. This forwards only the four endpoints yVPN needs, adds the CORS headers
 * the browser demands, and does nothing else.
 *
 * It holds no secrets: the caller supplies its own Tailscale key in the
 * Authorization header, which is passed straight through. Someone who finds this
 * URL without a key can do nothing with it.
 *
 * Deploy:
 *   npx wrangler deploy apps/web/proxy/worker.js --name yvpn-proxy --compatibility-date 2026-01-01
 * Or paste it into the Cloudflare dashboard: Workers & Pages -> Create -> Worker.
 *
 * Optional: set ALLOWED_ORIGINS to a comma-separated list to restrict callers,
 * e.g. "https://bnsmcx.github.io,null"   ("null" is what file:// pages send).
 * Unset means any origin, which is safe here only because there are no cookies
 * and no ambient credentials — every request must carry its own bearer token.
 */

const UPSTREAM = "https://api.tailscale.com";

const ALLOWED = [
  { re: /^\/api\/v2\/tailnet\/[^/]+\/keys$/,          methods: ["GET", "POST"] },
  { re: /^\/api\/v2\/tailnet\/[^/]+\/keys\/[^/]+$/,   methods: ["GET", "DELETE"] },
  { re: /^\/api\/v2\/tailnet\/[^/]+\/devices$/,       methods: ["GET"] },
  { re: /^\/api\/v2\/device\/[^/]+\/routes$/,         methods: ["GET", "POST"] },
];

function corsHeaders(request, env) {
  const origin = request.headers.get("Origin") || "*";
  const allowList = (env && env.ALLOWED_ORIGINS || "").split(",").map((s) => s.trim()).filter(Boolean);
  const allow = allowList.length === 0 ? origin : (allowList.includes(origin) ? origin : null);
  if (allow === null) return null;

  return {
    "Access-Control-Allow-Origin": allow,
    "Access-Control-Allow-Methods": "GET, POST, DELETE, OPTIONS",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
    "Access-Control-Max-Age": "86400",
    "Vary": "Origin",
  };
}

function permitted(pathname, method) {
  const m = method === "OPTIONS" ? null : method;
  return ALLOWED.some((r) => r.re.test(pathname) && (m === null || r.methods.includes(m)));
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const cors = corsHeaders(request, env);

    if (cors === null) {
      return new Response("Origin not allowed\n", { status: 403 });
    }

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: cors });
    }

    if (!permitted(url.pathname, request.method)) {
      return new Response(
        JSON.stringify({ message: `yvpn-proxy: ${request.method} ${url.pathname} is not on the allowlist` }),
        { status: 403, headers: { ...cors, "Content-Type": "application/json" } });
    }

    const auth = request.headers.get("Authorization");
    if (!auth) {
      return new Response(JSON.stringify({ message: "yvpn-proxy: missing Authorization header" }),
        { status: 401, headers: { ...cors, "Content-Type": "application/json" } });
    }

    // Rebuild the request deliberately: forward the bearer token and body, and
    // nothing else. No cookies, no client headers, no origin leakage upstream.
    const upstream = new Request(UPSTREAM + url.pathname + url.search, {
      method: request.method,
      headers: {
        "Authorization": auth,
        "Content-Type": request.headers.get("Content-Type") || "application/json",
        "Accept": "application/json",
      },
      body: ["GET", "HEAD"].includes(request.method) ? undefined : await request.text(),
    });

    let res;
    try {
      res = await fetch(upstream);
    } catch (e) {
      return new Response(JSON.stringify({ message: "yvpn-proxy: upstream unreachable: " + e.message }),
        { status: 502, headers: { ...cors, "Content-Type": "application/json" } });
    }

    const out = new Headers(cors);
    out.set("Content-Type", res.headers.get("Content-Type") || "application/json");
    return new Response(res.body, { status: res.status, headers: out });
  },
};
