// Command yvpn-proxy serves the yVPN web app and forwards its Tailscale API
// calls, so you can run the whole thing on your own machine with one binary and
// no cloud account.
//
// Tailscale's API sends no CORS headers, which means a browser refuses to call
// it from a web page. Serving the page and the API from the same origin sidesteps
// CORS entirely; the browser never makes a cross-origin request, and the app
// needs no proxy URL configured.
//
//	cd apps/web/proxy && go run .      # then open http://localhost:8777
//
// The proxy holds no secrets. The browser sends its own Tailscale key in the
// Authorization header and this passes it straight through.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const upstream = "https://api.tailscale.com"

// Only the endpoints the app actually uses. Keeps this from becoming an open relay.
var allowed = []struct {
	re      *regexp.Regexp
	methods []string
}{
	{regexp.MustCompile(`^/api/v2/tailnet/[^/]+/keys$`), []string{"GET", "POST"}},
	{regexp.MustCompile(`^/api/v2/tailnet/[^/]+/keys/[^/]+$`), []string{"GET", "DELETE"}},
	{regexp.MustCompile(`^/api/v2/tailnet/[^/]+/devices$`), []string{"GET"}},
	{regexp.MustCompile(`^/api/v2/device/[^/]+/routes$`), []string{"GET", "POST"}},
}

func permitted(path, method string) bool {
	for _, a := range allowed {
		if !a.re.MatchString(path) {
			continue
		}
		for _, m := range a.methods {
			if m == method {
				return true
			}
		}
	}
	return false
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, "{%q:%q}\n", "message", "yvpn-proxy: "+msg)
}

func proxyHandler(client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !permitted(r.URL.Path, r.Method) {
			writeJSONError(w, http.StatusForbidden,
				r.Method+" "+r.URL.Path+" is not on the allowlist")
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		target, err := url.Parse(upstream + r.URL.Path)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		target.RawQuery = r.URL.RawQuery

		// Forward the token and the body. Nothing else crosses over.
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("Accept", "application/json")
		if ct := r.Header.Get("Content-Type"); ct != "" {
			req.Header.Set("Content-Type", ct)
		} else {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := client.Do(req)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "upstream unreachable: "+err.Error())
			return
		}
		defer res.Body.Close()

		if ct := res.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(res.StatusCode)
		io.Copy(w, res.Body)

		log.Printf("%s %s -> %d", r.Method, r.URL.Path, res.StatusCode)
	}
}

// PORT is how App Platform (and most container hosts) name the port to serve on;
// in a container the app must also bind every interface, not just loopback.
func defaultAddr() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return "127.0.0.1:8777"
}

func main() {
	addr := flag.String("addr", defaultAddr(), "listen address")
	dir := flag.String("dir", "", "directory to serve the app from (default: the directory above this one)")
	flag.Parse()

	root := *dir
	if root == "" {
		exe, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		// Default to apps/web whether run from the repo root or from this package.
		for _, c := range []string{"apps/web", "..", "."} {
			if _, err := os.Stat(filepath.Join(exe, c, "index.html")); err == nil {
				root = filepath.Join(exe, c)
				break
			}
		}
	}
	if root == "" {
		panic("could not find index.html; pass -dir pointing at apps/web")
	}
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		panic(err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	mux := http.NewServeMux()
	mux.Handle("/api/", proxyHandler(client))
	// Serve the app and nothing else from apps/web.
	index := filepath.Join(root, "index.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})

	url := "http://" + *addr
	if strings.HasPrefix(*addr, ":") {
		url = "http://localhost" + *addr
	}
	log.Printf("yvpn-proxy serving %s", root)
	log.Printf("open %s", url)

	if err := http.ListenAndServe(*addr, mux); err != nil {
		panic(err)
	}
}
