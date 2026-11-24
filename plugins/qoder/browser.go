package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The localhost view provides the same actions when a desktop client does not
// expose the MCP App renderer. Its per-launch URL is private to this process.
func (s *server) serveBrowser(output io.Writer) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()

	origin := "http://" + listener.Addr().String()
	path := "/" + uuid.NewString() + "/"
	server := &http.Server{
		Handler:           s.browserHandler(path, origin),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      2 * time.Minute,
	}
	if _, err := fmt.Fprintln(output, origin+path); err != nil {
		return err
	}
	return server.Serve(listener)
}

func (s *server) browserHandler(path, origin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+path+"{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		_, _ = io.WriteString(w, s.panel("browser"))
	})
	var operations sync.Mutex
	mux.HandleFunc("POST "+path+"rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			http.Error(w, "same-origin JSON required", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JSONRPC != "2.0" || req.ID == nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		reply := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "ui/initialize":
			reply.Result = map[string]any{"hostContext": map[string]any{}}
		case "tools/call":
			operations.Lock()
			result, err := s.handle(r.Context(), req.Method, req.Params)
			operations.Unlock()
			if err != nil {
				reply.Error = &responseError{Code: -32602, Message: err.Error()}
			} else {
				reply.Result = result
			}
		default:
			reply.Error = &responseError{Code: -32601, Message: "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if "http://"+r.Host != origin {
			http.Error(w, "unexpected host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}
