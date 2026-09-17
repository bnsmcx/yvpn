/**
 * yVPN web — Cloudflare Worker.
 *
 * Serves the app (index.html) and relays its Tailscale API calls from the same
 * origin. Tailscale's API returns no CORS headers, so a browser cannot call it
 * from a web page; putting the page and the relay on one origin means the
 * browser never makes a cross-origin request and users never configure a proxy.
 *
 * Only the four endpoints yVPN needs are forwarded. The relay holds no secrets:
 * the browser supplies its own Tailscale key in the Authorization header, which
 * is passed straight through. The DigitalOcean token never reaches it.
 *
 * Deploy (from apps/web, using wrangler.toml there):
 *   npx wrangler deploy
 */

import INDEX_HTML from "../index.html";

const UPSTREAM = "https://api.tailscale.com";

const ALLOWED = [
  { re: /^\/api\/v2\/tailnet\/[^/]+\/keys$/,          methods: ["GET", "POST"] },
  { re: /^\/api\/v2\/tailnet\/[^/]+\/keys\/[^/]+$/,   methods: ["GET", "DELETE"] },
  { re: /^\/api\/v2\/tailnet\/[^/]+\/devices$/,       methods: ["GET"] },
  { re: /^\/api\/v2\/device\/[^/]+\/routes$/,         methods: ["GET", "POST"] },
];

const permitted = (pathname, method) =>
  ALLOWED.some((r) => r.re.test(pathname) && r.methods.includes(method));

const json = (obj, status) =>
  new Response(JSON.stringify(obj), { status, headers: { "Content-Type": "application/json" } });

export default {
  async fetch(request) {
    const url = new URL(request.url);

    if (url.pathname === "/" || url.pathname === "/index.html") {
      if (!["GET", "HEAD"].includes(request.method)) return new Response(null, { status: 405 });
      return new Response(request.method === "HEAD" ? null : INDEX_HTML, {
        headers: {
          "Content-Type": "text/html; charset=utf-8",
          "Cache-Control": "no-cache",
          "X-Content-Type-Options": "nosniff",
          "Referrer-Policy": "no-referrer",
        },
      });
    }

    if (!url.pathname.startsWith("/api/")) return new Response("Not found\n", { status: 404 });

    if (!permitted(url.pathname, request.method)) {
      return json({ message: `yvpn-proxy: ${request.method} ${url.pathname} is not on the allowlist` }, 403);
    }

    const auth = request.headers.get("Authorization");
    if (!auth) return json({ message: "yvpn-proxy: missing Authorization header" }, 401);

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
      return json({ message: "yvpn-proxy: upstream unreachable: " + e.message }, 502);
    }

    return new Response(res.body, {
      status: res.status,
      headers: { "Content-Type": res.headers.get("Content-Type") || "application/json" },
    });
  },
};
