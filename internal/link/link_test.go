package link

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte(`{"format_version":2,"artifact_type":"agent-handoff"}`)

	enc, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(enc.Key) != KeySize {
		t.Errorf("key size = %d, want %d", len(enc.Key), KeySize)
	}
	if len(enc.Nonce) != NonceSize {
		t.Errorf("nonce size = %d, want %d", len(enc.Nonce), NonceSize)
	}
	if bytes.Equal(enc.Ciphertext, plaintext) {
		t.Error("ciphertext must differ from plaintext")
	}

	got, err := Decrypt(enc.Ciphertext, enc.Key, enc.Nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round trip mismatch: %q", got)
	}
}

func TestEncryptUniqueKeys(t *testing.T) {
	// Two encryptions of the same payload must use different keys/nonces.
	a, _ := Encrypt([]byte("payload"))
	b, _ := Encrypt([]byte("payload"))
	if bytes.Equal(a.Key, b.Key) {
		t.Error("keys must be unique per encryption")
	}
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Error("nonces must be unique per encryption")
	}
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Error("ciphertexts must differ")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	enc, _ := Encrypt([]byte("secret payload"))

	ct := make([]byte, len(enc.Ciphertext))
	copy(ct, enc.Ciphertext)
	ct[0] ^= 0xff
	if _, err := Decrypt(ct, enc.Key, enc.Nonce); err != ErrDecrypt {
		t.Errorf("tampered ciphertext err = %v, want ErrDecrypt", err)
	}

	wrongKey := make([]byte, KeySize) // all zeros: right size, wrong bytes
	if _, err := Decrypt(enc.Ciphertext, wrongKey, enc.Nonce); err != ErrDecrypt {
		t.Errorf("wrong key err = %v, want ErrDecrypt", err)
	}
}

func TestDecryptRejectsBadSizes(t *testing.T) {
	enc, _ := Encrypt([]byte("x"))
	if _, err := Decrypt(enc.Ciphertext, enc.Key[:16], enc.Nonce); err == nil {
		t.Error("short key should fail")
	}
	if _, err := Decrypt(enc.Ciphertext, enc.Key, enc.Nonce[:8]); err == nil {
		t.Error("short nonce should fail")
	}
}

func TestKeyEncodeDecode(t *testing.T) {
	enc, _ := Encrypt([]byte("x"))
	s := EncodeKey(enc.Key)
	if strings.ContainsAny(s, "+/=") {
		t.Errorf("key encoding %q must be base64url", s)
	}
	back, err := DecodeKey(s)
	if err != nil {
		t.Fatalf("DecodeKey: %v", err)
	}
	if !bytes.Equal(back, enc.Key) {
		t.Error("key encode/decode mismatch")
	}
}

func TestParseLinkKey(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	url := "https://share.example.com/s/abc123"

	full := AppendKeyFragment(url, key)
	parsed, clean, err := ParseLinkKey(full)
	if err != nil {
		t.Fatalf("ParseLinkKey: %v", err)
	}
	if !bytes.Equal(parsed, key) {
		t.Error("parsed key mismatch")
	}
	if clean != url {
		t.Errorf("clean url = %q, want %q", clean, url)
	}
}

func TestParseLinkKeyRejectsMissingFragment(t *testing.T) {
	if _, _, err := ParseLinkKey("https://share.example.com/s/abc123"); err == nil {
		t.Error("missing fragment should fail")
	}
	if _, _, err := ParseLinkKey("https://share.example.com/s/abc123#k="); err == nil {
		t.Error("empty k parameter should fail")
	}
	if _, _, err := ParseLinkKey("https://share.example.com/s/abc123#k=tooshort"); err == nil {
		t.Error("malformed key should fail")
	}
	if _, _, err := ParseLinkKey("https://share.example.com/s/abc123#x=1"); err == nil {
		t.Error("fragment without k should fail")
	}
}

func TestParseLinkKeyRejectsInsecureBaseURL(t *testing.T) {
	fragment := "#k=" + EncodeKey(make([]byte, KeySize))
	for _, raw := range []string{
		"http://share.example.com/s/abc" + fragment,
		"https://user@share.example.com/s/abc" + fragment,
	} {
		if _, _, err := ParseLinkKey(raw); err == nil {
			t.Errorf("ParseLinkKey(%q) should fail", raw)
		}
	}
}

func TestDownloadRejectsPrivateURLWithDefaultClient(t *testing.T) {
	if _, _, err := Download("https://127.0.0.1/s/abc", make([]byte, KeySize), nil); err == nil {
		t.Fatal("Download should reject private URLs before making a request")
	}
}

func validManifest() *Manifest {
	m := &Manifest{Schema: ManifestSchema}
	m.Bundle.URL = "https://share.example.com/v1/shares/abc/blob"
	m.Bundle.SHA256 = strings.Repeat("a", 64)
	m.Bundle.Bytes = 100
	m.Crypto.Alg = KeyAlg
	m.Crypto.Nonce = "AAAAAAAAAAAAAAAA"
	m.Crypto.KeyRef = KeyRef
	return m
}

func TestManifestValidate(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Errorf("valid manifest rejected: %v", err)
	}

	m := validManifest()
	m.Schema = "other.v1"
	if err := m.Validate(); err == nil {
		t.Error("bad schema should fail")
	}

	m = validManifest()
	m.Bundle.URL = ""
	if err := m.Validate(); err == nil {
		t.Error("missing bundle url should fail")
	}

	m = validManifest()
	m.Crypto.Alg = "AES-128-CBC"
	if err := m.Validate(); err == nil {
		t.Error("bad algorithm should fail")
	}

	m = validManifest()
	m.Crypto.Nonce = ""
	if err := m.Validate(); err == nil {
		t.Error("missing nonce should fail")
	}

	m = validManifest()
	m.Crypto.KeyRef = "server-side-key"
	if err := m.Validate(); err == nil {
		t.Error("bad key_ref should fail")
	}
}

// fakeWorker simulates the upload + download endpoints of the Cloudflare
// Worker enough to exercise Upload and Download end to end.
func fakeWorker(t *testing.T) *httptest.Server {
	t.Helper()
	var blob []byte
	var manifest *Manifest

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/shares", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer testtoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "manifest":
				manifest = &Manifest{}
				if err := json.Unmarshal(data, manifest); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				manifest.Bundle.URL = "/v1/shares/abc/blob"
			case "blob":
				blob = data
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(UploadResult{ShareURL: "/s/abc"})
	})
	mux.HandleFunc("GET /v1/shares/abc", func(w http.ResponseWriter, r *http.Request) {
		if manifest == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	})
	mux.HandleFunc("GET /v1/shares/abc/blob", func(w http.ResponseWriter, r *http.Request) {
		if blob == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(blob)
	})
	return httptest.NewTLSServer(mux)
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	srv := fakeWorker(t)
	defer srv.Close()

	payload := []byte("this is the share zip payload, much larger than a block")
	shareURL, m, err := Upload(payload, "thread-1", "My Task", UploadOptions{
		Endpoint: srv.URL,
		Token:    "testtoken",
		Client:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if m.Crypto.Alg != KeyAlg {
		t.Errorf("manifest alg = %q", m.Crypto.Alg)
	}
	if !strings.HasPrefix(shareURL, srv.URL+"/s/") || !strings.Contains(shareURL, "#k=") {
		t.Errorf("share url = %q", shareURL)
	}

	key, cleanURL, err := ParseLinkKey(shareURL)
	if err != nil {
		t.Fatalf("ParseLinkKey: %v", err)
	}
	got, dm, err := Download(cleanURL, key, srv.Client())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("downloaded payload = %q", got)
	}
	if dm.Thread.ID != "thread-1" || dm.Thread.Title != "My Task" {
		t.Errorf("manifest thread = %+v", dm.Thread)
	}
}

func TestDownloadRejectsWrongKey(t *testing.T) {
	srv := fakeWorker(t)
	defer srv.Close()

	shareURL, _, err := Upload([]byte("payload"), "t", "T", UploadOptions{
		Endpoint: srv.URL, Token: "testtoken", Client: srv.Client(),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	_, cleanURL, _ := ParseLinkKey(shareURL)

	wrongKey := make([]byte, KeySize)
	for i := range wrongKey {
		wrongKey[i] = 0xEE
	}
	if _, _, err := Download(cleanURL, wrongKey, srv.Client()); err != ErrDecrypt {
		t.Errorf("wrong key err = %v, want ErrDecrypt", err)
	}
}

func TestUploadRejectsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	if _, _, err := Upload([]byte("p"), "t", "T", UploadOptions{
		Endpoint: srv.URL, Token: "x", Client: srv.Client(),
	}); err == nil {
		t.Error("server 500 should fail the upload")
	}
}

func TestResolveEndpointAndToken(t *testing.T) {
	t.Setenv(DefaultEndpointEnv, "")
	t.Setenv(TokenEnv, "")
	if ep := ResolveEndpoint(""); ep != DefaultEndpoint {
		t.Errorf("default endpoint = %q, want %q", ep, DefaultEndpoint)
	}
	if HasExplicitEndpoint("") {
		t.Error("default endpoint must not be treated as explicit")
	}
	if ep := ResolveEndpoint("https://x.example.com/"); ep != "https://x.example.com" {
		t.Errorf("endpoint trailing slash = %q", ep)
	}
	if !HasExplicitEndpoint("https://x.example.com/") {
		t.Error("flag endpoint should be explicit")
	}
	if tk := ResolveToken(""); tk != "" {
		t.Errorf("empty token = %q", tk)
	}
	t.Setenv(DefaultEndpointEnv, "https://env.example.com/")
	if ep := ResolveEndpoint(""); ep != "https://env.example.com" {
		t.Errorf("env endpoint = %q", ep)
	}
	if !HasExplicitEndpoint("") {
		t.Error("environment endpoint should be explicit")
	}
	t.Setenv(TokenEnv, "envtoken")
	if tk := ResolveToken(""); tk != "envtoken" {
		t.Errorf("env token = %q", tk)
	}
}

func TestDecodeNonce(t *testing.T) {
	enc, _ := Encrypt([]byte("x"))
	n, err := DecodeNonce(enc.NonceB64)
	if err != nil {
		t.Fatalf("DecodeNonce: %v", err)
	}
	if !bytes.Equal(n, enc.Nonce) {
		t.Error("nonce decode mismatch")
	}
}
