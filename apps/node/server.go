package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// The control API, reached from the yVPN dashboard in the browser at
// https://<node>.<tailnet>.ts.net:8443. It is cross-origin by nature (the
// dashboard is served from somewhere else) and authenticated by a bearer token
// rather than cookies, which is why allowing any origin is safe: a page without
// the token gets nowhere.

// maxChunk caps one upload request. The dashboard sends 16 MiB.
const maxChunk = 64 << 20

type statusResponse struct {
	Addon   string     `json:"addon"`
	Version string     `json:"version"`
	Phase   string     `json:"phase"` // installing, ready, failed
	Step    string     `json:"step"`
	Error   string     `json:"error,omitempty"`
	URL     string     `json:"url,omitempty"` // Jellyfin, on the tailnet (and the internet while shared)
	Disk    diskInfo   `json:"disk"`
	Media   []Item     `json:"media"`
	Share   shareState `json:"share"`
	Log     []string   `json:"log"`
}

type diskInfo struct {
	Free  int64 `json:"free"`
	Total int64 `json:"total"`
}

func (a *Agent) status() statusResponse {
	free, total := diskSpace(a.cfg.DataDir)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := statusResponse{
		Addon: a.cfg.Addon, Version: a.version,
		Phase: a.phase, Step: a.step, Error: a.instErr,
		Disk:  diskInfo{Free: free, Total: total},
		Media: make([]Item, 0, len(a.items)),
		Share: a.share,
		Log:   append([]string(nil), a.logTail...),
	}
	if a.fqdn != "" {
		st.URL = "https://" + a.fqdn + "/"
	}
	for _, it := range a.items {
		st.Media = append(st.Media, *it)
	}
	return st
}

func (a *Agent) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.status()) })

	mux.HandleFunc("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if in.Size <= 0 {
			writeErr(w, 400, "size must be more than zero")
			return
		}
		it, err := a.startUpload(in.Name, in.Size)
		if err != nil {
			writeErr(w, http.StatusInsufficientStorage, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"id": it.ID, "offset": it.Received, "name": it.Name})
	})

	mux.HandleFunc("PATCH /v1/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
		if err != nil || offset < 0 {
			writeErr(w, 400, "Upload-Offset header is required")
			return
		}
		got, err := a.writeChunk(r.PathValue("id"), offset, http.MaxBytesReader(w, r.Body, maxChunk))
		w.Header().Set("Upload-Offset", strconv.FormatInt(got, 10))
		var oe offsetError
		switch {
		case err == nil:
			writeJSON(w, 200, map[string]any{"offset": got})
		case errors.As(err, &oe):
			writeJSON(w, http.StatusConflict, map[string]any{"offset": oe.offset, "message": err.Error()})
		case errors.Is(err, errBusy):
			writeJSON(w, http.StatusConflict, map[string]any{"offset": got, "message": err.Error()})
		case errors.Is(err, os.ErrNotExist):
			writeErr(w, 404, "no such upload; start it again")
		default:
			writeJSON(w, 400, map[string]any{"offset": got, "message": err.Error()})
		}
	})

	mux.HandleFunc("POST /v1/fetch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		it, err := a.fetchLink(in.URL)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, it)
	})

	mux.HandleFunc("DELETE /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := a.remove(r.PathValue("id")); err != nil {
			writeErr(w, 404, "no such video")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/share", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if err := a.setShare(r.Context(), in.Enabled); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"message": err.Error(), "status": a.status()})
			return
		}
		writeJSON(w, 200, a.status())
	})

	mux.HandleFunc("POST /v1/retry", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		failed := a.phase == "failed"
		a.mu.Unlock()
		if !failed {
			writeErr(w, 409, "setup has not failed")
			return
		}
		go a.runInstall(context.Background())
		writeJSON(w, 202, map[string]string{"message": "retrying"})
	})

	return a.guard(mux)
}

// guard answers CORS (including Chrome's private-network preflight, since the
// node's tailnet address is private and the dashboard's usually isn't) and
// checks the bearer token on everything but the health check.
func (a *Agent) guard(next http.Handler) http.Handler {
	want := []byte("Bearer " + a.cfg.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Upload-Offset")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		h.Set("Access-Control-Expose-Headers", "Upload-Offset")
		h.Set("Access-Control-Max-Age", "600")
		h.Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				h.Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/healthz" &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeErr(w, http.StatusUnauthorized, "wrong or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "bad request body: "+strings.TrimPrefix(err.Error(), "json: "))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}
