package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUploadRelayFallsBackAndCreatesTwoReplicas(t *testing.T) {
	var uploads atomic.Int32
	failureSeen := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tempsh", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
		close(failureSeen)
	})
	mux.HandleFunc("POST /tmpfiles", func(w http.ResponseWriter, r *http.Request) {
		<-failureSeen
		assertRelayUpload(t, r, "file")
		uploads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"url":"https://tmpfiles.org/a/file.enc"}}`))
	})
	mux.HandleFunc("POST /uguu", func(w http.ResponseWriter, r *http.Request) {
		<-failureSeen
		assertRelayUpload(t, r, "files[]")
		uploads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"files":[{"url":"https://d.uguu.se/file.enc"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	providers := []relayProvider{
		{name: "temp.sh", endpoint: srv.URL + "/tempsh", field: "file", parseReply: parseTempSHReply},
		{name: "tmpfiles.org", endpoint: srv.URL + "/tmpfiles", field: "file", parseReply: parseTmpFilesReply},
		{name: "uguu.se", endpoint: srv.URL + "/uguu", field: "files[]", parseReply: parseUguuReply},
	}
	res, err := UploadRelay([]byte("zip payload"), RelayUploadOptions{
		TTLSeconds:  1,
		ResolverURL: "https://resolver.example/r",
		Client:      srv.Client(),
		providers:   providers,
	})
	if err != nil {
		t.Fatalf("UploadRelay: %v", err)
	}
	if uploads.Load() != 2 {
		t.Fatalf("successful uploads = %d, want 2", uploads.Load())
	}
	if got := strings.Join(res.Providers, ","); got != "tmpfiles.org,uguu.se" {
		t.Errorf("providers = %q", got)
	}
	m, cleanURL, err := ParseRelayLink(res.ShareURL)
	if err != nil {
		t.Fatalf("ParseRelayLink: %v", err)
	}
	if cleanURL != "https://resolver.example/r" {
		t.Errorf("clean URL = %q", cleanURL)
	}
	if len(m.Replicas) != 2 || !strings.HasPrefix(m.Replicas[0].URL, "https://tmpfiles.org/dl/") {
		t.Errorf("replicas = %#v", m.Replicas)
	}
	expiresAt, _ := time.Parse(time.RFC3339, res.ExpiresAt)
	if remaining := time.Until(expiresAt); remaining < 55*time.Second || remaining > 65*time.Second {
		t.Errorf("minimum TTL was not applied: %v", remaining)
	}
}

func assertRelayUpload(t *testing.T, r *http.Request, field string) {
	t.Helper()
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}
	f, header, err := r.FormFile(field)
	if err != nil {
		t.Fatalf("FormFile(%q): %v", field, err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if len(data) == 0 || !strings.HasSuffix(header.Filename, ".enc") {
		t.Errorf("invalid encrypted upload: filename=%q bytes=%d", header.Filename, len(data))
	}
}

func TestUploadRelayReturnsSingleReplicaWhenOthersFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.Write([]byte(`{"success":true,"files":[{"url":"https://d.uguu.se/only.enc"}]}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	providers := []relayProvider{
		{name: "tmpfiles.org", endpoint: srv.URL + "/fail", field: "file", parseReply: parseTmpFilesReply},
		{name: "uguu.se", endpoint: srv.URL + "/ok", field: "files[]", parseReply: parseUguuReply},
		{name: "temp.sh", endpoint: srv.URL + "/fail", field: "file", parseReply: parseTempSHReply},
	}
	res, err := UploadRelay([]byte("payload"), RelayUploadOptions{
		ResolverURL: "https://resolver.example/r",
		Client:      srv.Client(),
		providers:   providers,
	})
	if err != nil {
		t.Fatalf("UploadRelay: %v", err)
	}
	if len(res.Providers) != 1 || res.Providers[0] != "uguu.se" {
		t.Errorf("providers = %#v", res.Providers)
	}
	if len(res.ProviderErrors) != 2 {
		t.Errorf("provider errors = %#v, want both failed providers", res.ProviderErrors)
	}
}

func TestDownloadRelayFallsBackAfterCorruptReplica(t *testing.T) {
	payload := []byte("complete agent handoff zip")
	enc, err := Encrypt(payload)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Repeat([]byte{0xee}, len(enc.Ciphertext))
	m := &RelayManifest{
		Version: RelayVersion,
		Replicas: []RelayReplica{
			{Provider: "tmpfiles.org", URL: "https://tmpfiles.org/dl/a/file.enc"},
			{Provider: "uguu.se", URL: "https://d.uguu.se/file.enc"},
		},
		Key:       EncodeKey(enc.Key),
		Nonce:     enc.NonceB64,
		SHA256:    enc.SHA256,
		Bytes:     int64(len(enc.Ciphertext)),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	shareURL, err := encodeRelayLink("https://resolver.example/r", m)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		body := enc.Ciphertext
		if req.URL.Host == "tmpfiles.org" {
			body = corrupt
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(body)),
			Body:          io.NopCloser(bytes.NewReader(body)),
			Header:        make(http.Header),
			Request:       req,
		}, nil
	})}

	got, parsed, cleanURL, err := DownloadRelay(shareURL, client)
	if err != nil {
		t.Fatalf("DownloadRelay: %v", err)
	}
	if !bytes.Equal(got, payload) || parsed.SHA256 != enc.SHA256 {
		t.Error("relay payload did not round trip")
	}
	if cleanURL != "https://resolver.example/r" || requests != 2 {
		t.Errorf("cleanURL=%q requests=%d", cleanURL, requests)
	}
}

func TestDownloadRelayUsesPostForTempSH(t *testing.T) {
	payload := []byte("temp.sh payload")
	enc, _ := Encrypt(payload)
	m := &RelayManifest{
		Version:   RelayVersion,
		Replicas:  []RelayReplica{{Provider: "temp.sh", URL: "https://temp.sh/a/file.enc"}},
		Key:       EncodeKey(enc.Key),
		Nonce:     enc.NonceB64,
		SHA256:    enc.SHA256,
		Bytes:     int64(len(enc.Ciphertext)),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	shareURL, _ := encodeRelayLink("https://resolver.example/r", m)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Errorf("temp.sh download method = %s, want POST", req.Method)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(enc.Ciphertext)),
			Body:          io.NopCloser(bytes.NewReader(enc.Ciphertext)),
			Header:        make(http.Header),
			Request:       req,
		}, nil
	})}
	got, _, _, err := DownloadRelay(shareURL, client)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("DownloadRelay = %q, %v", got, err)
	}
}

func TestParseRelayLinkRejectsUnsafeAndExpiredManifests(t *testing.T) {
	enc, _ := Encrypt([]byte("payload"))
	base := RelayManifest{
		Version:   RelayVersion,
		Replicas:  []RelayReplica{{Provider: "uguu.se", URL: "https://d.uguu.se/file.enc"}},
		Key:       EncodeKey(enc.Key),
		Nonce:     enc.NonceB64,
		SHA256:    enc.SHA256,
		Bytes:     int64(len(enc.Ciphertext)),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}

	unsafe := base
	unsafe.Replicas = []RelayReplica{{Provider: "uguu.se", URL: "https://127.0.0.1/private"}}
	unsafeURL, _ := encodeRelayLink("https://resolver.example/r", &unsafe)
	if _, _, err := ParseRelayLink(unsafeURL); err == nil || !strings.Contains(err.Error(), "unexpected host") {
		t.Errorf("unsafe host error = %v", err)
	}

	expired := base
	expired.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	expiredURL, _ := encodeRelayLink("https://resolver.example/r", &expired)
	if _, _, err := ParseRelayLink(expiredURL); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired error = %v", err)
	}
}

func TestRelayProviderReplyParsers(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) (string, error)
		body any
		want string
	}{
		{"tmpfiles.org", parseTmpFilesReply, map[string]any{"status": "success", "data": map[string]any{"url": "https://tmpfiles.org/a/f.enc"}}, "https://tmpfiles.org/dl/a/f.enc"},
		{"uguu.se", parseUguuReply, map[string]any{"success": true, "files": []map[string]any{{"url": "https://d.uguu.se/f.enc"}}}, "https://d.uguu.se/f.enc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(tt.body)
			got, err := tt.fn(data)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	got, err := parseTempSHReply([]byte("https://temp.sh/abc/f.enc\n"))
	if err != nil || got != "https://temp.sh/abc/f.enc" {
		t.Fatalf("temp.sh parser got %q, %v", got, err)
	}
}

func TestRelayTTLDefaultsAndClamps(t *testing.T) {
	if got := clampRelayTTL(0); got != 24*60*60 {
		t.Errorf("default TTL = %d, want 86400", got)
	}
	if got := clampRelayTTL(30 * 24 * 60 * 60); got != 7*24*60*60 {
		t.Errorf("maximum TTL = %d, want 604800", got)
	}
}

func TestUploadFilebinReplicaUsesRawBody(t *testing.T) {
	payload := []byte("encrypted bytes")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Host != "filebin.net" {
			t.Errorf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("content type = %q", req.Header.Get("Content-Type"))
		}
		got, _ := io.ReadAll(req.Body)
		if !bytes.Equal(got, payload) {
			t.Errorf("body = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader(`{"file":{"bytes":15}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	got, err := uploadFilebinReplica(context.Background(), client, "bundle.enc", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://filebin.net/") || !strings.HasSuffix(got, "/bundle.enc") {
		t.Errorf("download URL = %q", got)
	}
	if err := validateRelayReplica(RelayReplica{Provider: "filebin.net", URL: got}); err != nil {
		t.Fatalf("validate filebin replica: %v", err)
	}
}

func TestResolveResolverURL(t *testing.T) {
	t.Setenv(ResolverEnv, "https://resolver.example/custom/")
	if got := ResolveResolverURL(); got != "https://resolver.example/custom" {
		t.Errorf("resolver = %q", got)
	}
}

func TestLiveAnonymousProviders(t *testing.T) {
	if os.Getenv("AGENT_HANDOFF_LIVE_PROVIDER_TEST") != "1" {
		t.Skip("set AGENT_HANDOFF_LIVE_PROVIDER_TEST=1 to exercise public providers")
	}
	payload := []byte("agent-handoff anonymous provider integration probe")
	res, err := UploadRelay(payload, RelayUploadOptions{
		TTLSeconds:  RelayMinTTL,
		ResolverURL: "https://resolver.example/r",
	})
	if err != nil {
		t.Fatalf("UploadRelay: %v", err)
	}
	t.Logf("providers=%v warnings=%v", res.Providers, res.ProviderErrors)
	got, _, err := DownloadURL(res.ShareURL, nil)
	if err != nil {
		t.Fatalf("DownloadURL via %v: %v", res.Providers, err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("live relay payload mismatch")
	}
}
