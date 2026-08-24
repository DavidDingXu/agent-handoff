package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/idgen"
)

// DefaultEndpoint is the project-operated zero-configuration link service.
// Explicit --endpoint or AGENT_HANDOFF_ENDPOINT values always take precedence.
var DefaultEndpoint = "https://agent-handoff-link.798148655.workers.dev"

// DefaultEndpointEnv is the env var overriding the default worker endpoint.
const DefaultEndpointEnv = "AGENT_HANDOFF_ENDPOINT"

// TokenEnv is the env var carrying the upload bearer token.
const TokenEnv = "AGENT_HANDOFF_TOKEN"

// UploadOptions configures an upload.
type UploadOptions struct {
	Endpoint   string // worker origin, e.g. https://share.example.com
	Token      string // bearer token; empty for anonymous
	TTLSeconds int    // requested link lifetime; 0 → server default (10 min)
	Client     *http.Client
}

// UploadResult is the server response.
type UploadResult struct {
	ShareURL    string `json:"share_url"`
	ManifestURL string `json:"manifest_url"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// ResolveEndpoint resolves the worker endpoint: flag > env > DefaultEndpoint.
func ResolveEndpoint(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv(DefaultEndpointEnv)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(DefaultEndpoint, "/")
}

// HasExplicitEndpoint reports whether the user deliberately selected a link
// service. Explicit services never fall back to anonymous third parties.
func HasExplicitEndpoint(flagValue string) bool {
	return strings.TrimSpace(flagValue) != "" || strings.TrimSpace(os.Getenv(DefaultEndpointEnv)) != ""
}

// ResolveToken resolves the upload token: flag > env > empty.
func ResolveToken(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(TokenEnv))
}

// Upload encrypts the payload and uploads it to the worker. It returns the
// final share link (with the #k= fragment) and the link manifest.
func Upload(payload []byte, threadID, title string, opts UploadOptions) (shareURL string, m *Manifest, err error) {
	enc, err := Encrypt(payload)
	if err != nil {
		return "", nil, err
	}

	m = &Manifest{Schema: ManifestSchema, CreatedAt: idgen.NowRFC3339()}
	m.Thread.ID = threadID
	m.Thread.Title = title
	m.TTLSeconds = opts.TTLSeconds
	m.Bundle.SHA256 = enc.SHA256
	m.Bundle.Bytes = int64(len(enc.Ciphertext))
	m.Crypto.Alg = KeyAlg
	m.Crypto.Nonce = enc.NonceB64
	m.Crypto.KeyRef = KeyRef

	res, err := postShare(opts, m, enc.Ciphertext)
	if err != nil {
		return "", nil, err
	}
	if res.ExpiresAt != "" {
		m.ExpiresAt = res.ExpiresAt // server-decided expiry (it may clamp the requested ttl)
	}

	link := AppendKeyFragment(res.ShareURL, enc.Key)
	return link, m, nil
}

func postShare(opts UploadOptions, m *Manifest, ciphertext []byte) (*UploadResult, error) {
	manifestBytes, err := marshalManifest(m)
	if err != nil {
		return nil, err
	}
	shareID := idgen.NewUUID()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if err := mw.WriteField("share_id", shareID); err != nil {
		return nil, err
	}
	if err := mw.WriteField("manifest", string(manifestBytes)); err != nil {
		return nil, err
	}
	fw, err := mw.CreateFormFile("blob", "blob.enc")
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(ciphertext); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(opts.Endpoint, "/")
	req, err := http.NewRequest(http.MethodPost, endpoint+"/v1/shares", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+opts.Token)
	}

	client := opts.Client
	if client == nil {
		timeout := 120 * time.Second
		if strings.TrimRight(opts.Endpoint, "/") == strings.TrimRight(DefaultEndpoint, "/") {
			timeout = 20 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload share: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("upload share: server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	var out UploadResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("upload share: invalid response: %w", err)
	}
	if out.ShareURL == "" {
		return nil, fmt.Errorf("upload share: response missing share_url")
	}
	if !strings.HasPrefix(out.ShareURL, "http") {
		// Relative URL: resolve against the endpoint.
		base, _ := url.Parse(endpoint)
		ref, err := url.Parse(out.ShareURL)
		if err != nil {
			return nil, fmt.Errorf("upload share: invalid share_url")
		}
		out.ShareURL = base.ResolveReference(ref).String()
	}
	return &out, nil
}

func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// Download fetches the manifest and ciphertext for a cleaned share URL and
// decrypts it. key comes from ParseLinkKey.
func Download(cleanURL string, key []byte, client *http.Client) ([]byte, *Manifest, error) {
	validateURL := validatePublicHTTPSURL
	if client != nil {
		// Explicit clients are used by embedders and tests for private endpoints.
		// The CLI always passes nil and therefore keeps the public-network guard.
		validateURL = validateHTTPSURL
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	origin, err := validateURL(cleanURL, "share URL")
	if err != nil {
		return nil, nil, err
	}
	client = withRedirectPolicy(client, func(next *url.URL) error {
		if _, err := validateURL(next.String(), "share redirect"); err != nil {
			return err
		}
		if !sameOrigin(origin, next) {
			return fmt.Errorf("share redirect changed origin")
		}
		return nil
	})

	manifestURL := cleanURL
	// A /s/<id> page URL becomes the manifest API URL.
	if strings.Contains(cleanURL, "/s/") {
		manifestURL = strings.Replace(cleanURL, "/s/", "/v1/shares/", 1)
	}
	// If the URL already is a manifest URL, use it directly.
	manifestOrigin, err := validateURL(manifestURL, "manifest URL")
	if err != nil {
		return nil, nil, err
	}
	m, err := fetchJSON(client, manifestURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch share manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}

	bundleURL := m.Bundle.URL
	if !strings.HasPrefix(bundleURL, "http") {
		base, err := url.Parse(manifestURL)
		if err != nil {
			return nil, nil, err
		}
		ref, err := url.Parse(bundleURL)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bundle url in manifest")
		}
		bundleURL = base.ResolveReference(ref).String()
	}
	bundleOrigin, err := validateURL(bundleURL, "bundle URL")
	if err != nil {
		return nil, nil, err
	}
	if !sameOrigin(manifestOrigin, bundleOrigin) {
		return nil, nil, fmt.Errorf("bundle URL must use the share origin")
	}

	ciphertext, err := fetchBlob(client, bundleURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch share blob: %w", err)
	}
	if m.Bundle.Bytes != 0 && int64(len(ciphertext)) != m.Bundle.Bytes {
		return nil, nil, fmt.Errorf("share blob size mismatch: manifest says %d, got %d", m.Bundle.Bytes, len(ciphertext))
	}
	sum := sha256hex(ciphertext)
	if sum != m.Bundle.SHA256 {
		return nil, nil, fmt.Errorf("share blob checksum mismatch")
	}

	nonce, err := DecodeNonce(m.Crypto.Nonce)
	if err != nil {
		return nil, nil, err
	}
	plaintext, err := Decrypt(ciphertext, key, nonce)
	if err != nil {
		return nil, nil, err
	}
	return plaintext, m, nil
}

func validatePublicHTTPSURL(raw, label string) (*url.URL, error) {
	u, err := validateHTTPSURL(raw, label)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, fmt.Errorf("%s must be a public https URL", label)
	}
	if ip := net.ParseIP(host); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate()) {
		return nil, fmt.Errorf("%s must be a public https URL", label)
	}
	return u, nil
}

func validateHTTPSURL(raw, label string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("%s must be an https URL", label)
	}
	return u, nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func withRedirectPolicy(client *http.Client, validate func(*url.URL) error) *http.Client {
	clone := *client
	previous := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validate(req.URL); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func fetchJSON(client *http.Client, u string) (*Manifest, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return &m, nil
}

func fetchBlob(client *http.Client, u string) ([]byte, error) {
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	return data, nil
}
