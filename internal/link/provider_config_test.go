package link

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredProviderRoundTrip(t *testing.T) {
	t.Setenv("TEST_FILE_TOKEN", "secret-token")
	var stored []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "upload.example":
			if req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer secret-token" {
				t.Errorf("upload request = %s auth=%q", req.Method, req.Header.Get("Authorization"))
			}
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			if req.FormValue("expire") != "3600" {
				t.Errorf("expire = %q", req.FormValue("expire"))
			}
			file, _, err := req.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			stored, err = io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			return response(req, http.StatusCreated, `{"data":{"url":"https://files.example/download/bundle.enc"}}`), nil
		case "files.example":
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(stored)), Body: io.NopCloser(bytes.NewReader(stored)), Header: make(http.Header), Request: req}, nil
		default:
			t.Fatalf("unexpected host %q", req.URL.Host)
			return nil, nil
		}
	})}
	config := &ProviderConfig{Providers: []HTTPProviderConfig{{
		Name:           "cn-store",
		UploadURL:      "https://upload.example/v1/files",
		UploadType:     "multipart",
		FileField:      "file",
		Headers:        map[string]string{"Authorization": "Bearer ${TEST_FILE_TOKEN}"},
		FormFields:     map[string]string{"expire": "{ttl_seconds}"},
		ResponseType:   "json",
		URLJSONPointer: "/data/url",
	}}}

	payload := bytes.Repeat([]byte("configured provider payload"), 4096)
	result, err := UploadRelay(payload, RelayUploadOptions{
		TTLSeconds:          3600,
		ResolverURL:         "https://resolver.example/r",
		Client:              client,
		ConfiguredProviders: config,
	})
	if err != nil {
		t.Fatalf("UploadRelay: %v", err)
	}
	if got := strings.Join(result.Providers, ","); got != "config:cn-store" {
		t.Fatalf("providers = %q", got)
	}
	got, _, err := DownloadURL(result.ShareURL, client)
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("configured provider payload did not round trip")
	}
}

func TestResolveProviderConfigReadsStrictJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"providers":[{"name":"store","upload_url":"https://upload.example/files","upload_type":"raw","headers":{},"response_type":"text"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, resolved, err := ResolveProviderConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path || len(config.Providers) != 1 || config.Providers[0].Name != "store" {
		t.Fatalf("config=%#v path=%q", config, resolved)
	}
}

func TestResolveProviderConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":[],"command":"unsafe"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveProviderConfig(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ResolveProviderConfig error = %v", err)
	}
}

func TestResolveProviderConfigRejectsDuplicateProviderNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"providers":[{"name":"store","upload_url":"https://one.example/files","upload_type":"raw","response_type":"text"},{"name":"store","upload_url":"https://two.example/files","upload_type":"raw","response_type":"text"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveProviderConfig(path); err == nil || !strings.Contains(err.Error(), "duplicate provider name") {
		t.Fatalf("ResolveProviderConfig error = %v", err)
	}
}

func TestConfiguredRawProviderUsesArrayJSONPointer(t *testing.T) {
	var stored []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "upload.example":
			if got := req.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Errorf("content type = %q", got)
			}
			var err error
			stored, err = io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			return response(req, http.StatusOK, `[{"download":"https://files.example/raw.enc"}]`), nil
		case "files.example":
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(stored)), Body: io.NopCloser(bytes.NewReader(stored)), Header: make(http.Header), Request: req}, nil
		default:
			t.Fatalf("unexpected host %q", req.URL.Host)
			return nil, nil
		}
	})}
	config := &ProviderConfig{Providers: []HTTPProviderConfig{{
		Name: "raw-store", UploadURL: "https://upload.example/{filename}", UploadType: "raw", ResponseType: "json", URLJSONPointer: "/0/download",
	}}}
	payload := []byte("raw configured provider")
	result, err := UploadRelay(payload, RelayUploadOptions{ResolverURL: "https://resolver.example/r", Client: client, ConfiguredProviders: config})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := DownloadURL(result.ShareURL, client)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload = %q", got)
	}
}

func TestResolveProviderConfigRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxProviderConfigBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveProviderConfig(path); err == nil || !strings.Contains(err.Error(), "file exceeds") {
		t.Fatalf("ResolveProviderConfig error = %v", err)
	}
}

func TestExpandProviderValueRejectsInvalidOrMissingEnvironmentName(t *testing.T) {
	for _, value := range []string{"${1TOKEN}", "${TOKEN-NAME}", "${MISSING_PROVIDER_TOKEN}"} {
		if _, err := expandProviderValue(value, nil); err == nil {
			t.Fatalf("expandProviderValue(%q) succeeded", value)
		}
	}
}

func TestConfiguredProviderRevalidatesExpandedUploadURL(t *testing.T) {
	t.Setenv("TEST_UPLOAD_HOST", "127.0.0.1")
	provider := HTTPProviderConfig{
		Name: "local", UploadURL: "https://${TEST_UPLOAD_HOST}/upload", UploadType: "raw", ResponseType: "text",
	}
	_, err := provider.upload(t.Context(), http.DefaultClient, "bundle.enc", []byte("payload"), 60)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("upload error = %v", err)
	}
}

func TestConfiguredProviderRejectsPrivateRedirect(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "upload.example" {
			t.Fatalf("redirect reached transport: %s", req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://127.0.0.1/private"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}, nil
	})}
	provider := HTTPProviderConfig{
		Name: "redirect", UploadURL: "https://upload.example/files", UploadType: "raw", ResponseType: "text",
	}
	_, err := provider.upload(t.Context(), client, "bundle.enc", []byte("payload"), 60)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("upload error = %v", err)
	}
}

func TestConfiguredProviderFailureDoesNotUseBuiltIns(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusBadGateway, "unavailable"), nil
	})}
	config := &ProviderConfig{Providers: []HTTPProviderConfig{{
		Name: "failing", UploadURL: "https://upload.example/files", UploadType: "raw", ResponseType: "text",
	}}}
	_, err := UploadRelay([]byte("payload"), RelayUploadOptions{ResolverURL: "https://resolver.example/r", Client: client, ConfiguredProviders: config})
	if err == nil || !strings.Contains(err.Error(), "config:failing: server returned 502") {
		t.Fatalf("UploadRelay error = %v", err)
	}
}

func TestRelayRejectsMalformedConfiguredProviderID(t *testing.T) {
	replica := RelayReplica{Provider: "config:", URL: "https://files.example/bundle.enc"}
	if err := validateRelayReplica(replica); err == nil {
		t.Fatal("expected malformed configured provider id to fail")
	}
}

func response(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}
}
