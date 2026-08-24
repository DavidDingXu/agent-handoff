package link

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/idgen"
)

const (
	RelayVersion        = 1
	RelayFragmentParam  = "h"
	ResolverEnv         = "AGENT_HANDOFF_RESOLVER"
	RelayDefaultTTL     = 24 * 60 * 60
	RelayMinTTL         = 60
	RelayMaxTTL         = 7 * 24 * 60 * 60
	RelayMaxBlobBytes   = 64 << 20
	RelayTargetReplicas = 2
	RelayMaxReplicas    = 3
	RelayReplicaGrace   = 3 * time.Second
)

// DefaultResolverURL is a static page only. The encrypted bundle and its key
// remain in third-party object URLs and the URL fragment, respectively.
var DefaultResolverURL = "https://agent-handoff-link.798148655.workers.dev/r"

// RelayReplica identifies one encrypted copy stored by an anonymous provider.
type RelayReplica struct {
	Provider string `json:"p"`
	URL      string `json:"u"`
}

// RelayManifest is encoded as base64url JSON in the #h= URL fragment.
// Browsers do not send it to the resolver server.
type RelayManifest struct {
	Version   int            `json:"v"`
	Replicas  []RelayReplica `json:"r"`
	Key       string         `json:"k"`
	Nonce     string         `json:"n"`
	SHA256    string         `json:"s"`
	Bytes     int64          `json:"b"`
	ExpiresAt string         `json:"e"`
}

// RelayUploadOptions configures zero-configuration anonymous uploads.
type RelayUploadOptions struct {
	TTLSeconds  int
	ResolverURL string
	Client      *http.Client
	providers   []relayProvider
}

// RelayUploadResult reports the generated link and provider-level outcome.
type RelayUploadResult struct {
	ShareURL       string
	ExpiresAt      string
	Providers      []string
	ProviderErrors []string
}

type relayProvider struct {
	name       string
	endpoint   string
	field      string
	parseReply func([]byte) (string, error)
	upload     func(context.Context, *http.Client, string, []byte) (string, error)
}

type relayUploadOutcome struct {
	index   int
	replica RelayReplica
	err     error
}

// ResolveResolverURL resolves the static resolver: env > built-in default.
func ResolveResolverURL() string {
	if v := strings.TrimSpace(os.Getenv(ResolverEnv)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(DefaultResolverURL, "/")
}

// UploadRelay encrypts a bundle once and stores the same ciphertext with up to
// two anonymous providers. One successful copy is enough to return a link.
func UploadRelay(payload []byte, opts RelayUploadOptions) (*RelayUploadResult, error) {
	enc, err := Encrypt(payload)
	if err != nil {
		return nil, err
	}
	if len(enc.Ciphertext) > RelayMaxBlobBytes {
		return nil, fmt.Errorf("anonymous link payload is too large: %d bytes (max %d)", len(enc.Ciphertext), RelayMaxBlobBytes)
	}

	ttl := clampRelayTTL(opts.TTLSeconds)
	resolver := strings.TrimSpace(opts.ResolverURL)
	if resolver == "" {
		resolver = ResolveResolverURL()
	}
	if err := validateResolverURL(resolver); err != nil {
		return nil, err
	}

	providers := opts.providers
	if len(providers) == 0 {
		providers = defaultRelayProviders()
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	filename := idgen.NewUUID() + ".enc"
	outcomes := make(chan relayUploadOutcome, len(providers))
	for i, provider := range providers {
		go func(index int, p relayProvider) {
			downloadURL, uploadErr := uploadRelayReplica(ctx, client, p, filename, enc.Ciphertext)
			replica := RelayReplica{Provider: p.name, URL: downloadURL}
			if uploadErr == nil {
				uploadErr = validateRelayReplica(replica)
			}
			outcomes <- relayUploadOutcome{index: index, replica: replica, err: uploadErr}
		}(i, provider)
	}

	replicas := make([]relayUploadOutcome, 0, RelayTargetReplicas)
	providerErrors := make([]relayUploadOutcome, 0, len(providers))
	var grace *time.Timer
	var graceDone <-chan time.Time
	for range providers {
		select {
		case outcome := <-outcomes:
			if outcome.err != nil {
				providerErrors = append(providerErrors, outcome)
				continue
			}
			replicas = append(replicas, outcome)
			if len(replicas) == 1 {
				grace = time.NewTimer(RelayReplicaGrace)
				graceDone = grace.C
			}
			if len(replicas) == RelayTargetReplicas {
				cancel()
				goto uploadsDone
			}
		case <-graceDone:
			cancel()
			goto uploadsDone
		case <-ctx.Done():
			goto uploadsDone
		}
	}
uploadsDone:
	if grace != nil {
		grace.Stop()
	}
	if len(replicas) == 0 {
		messages := relayProviderErrorMessages(providerErrors)
		if len(messages) == 0 {
			messages = []string{ctx.Err().Error()}
		}
		return nil, fmt.Errorf("anonymous link upload failed: %s", strings.Join(messages, "; "))
	}
	sort.Slice(replicas, func(i, j int) bool { return replicas[i].index < replicas[j].index })
	resolvedReplicas := make([]RelayReplica, 0, len(replicas))
	for _, outcome := range replicas {
		resolvedReplicas = append(resolvedReplicas, outcome.replica)
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttl) * time.Second).Format(time.RFC3339)
	m := RelayManifest{
		Version:   RelayVersion,
		Replicas:  resolvedReplicas,
		Key:       EncodeKey(enc.Key),
		Nonce:     enc.NonceB64,
		SHA256:    enc.SHA256,
		Bytes:     int64(len(enc.Ciphertext)),
		ExpiresAt: expiresAt,
	}
	shareURL, err := encodeRelayLink(resolver, &m)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resolvedReplicas))
	for _, replica := range resolvedReplicas {
		names = append(names, replica.Provider)
	}
	return &RelayUploadResult{
		ShareURL:       shareURL,
		ExpiresAt:      expiresAt,
		Providers:      names,
		ProviderErrors: relayProviderErrorMessages(providerErrors),
	}, nil
}

func relayProviderErrorMessages(outcomes []relayUploadOutcome) []string {
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].index < outcomes[j].index })
	messages := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		messages = append(messages, outcome.replica.Provider+": "+outcome.err.Error())
	}
	return messages
}

func defaultRelayProviders() []relayProvider {
	return []relayProvider{
		{
			name:   "filebin.net",
			upload: uploadFilebinReplica,
		},
		{
			name:       "tmpfiles.org",
			endpoint:   "https://tmpfiles.org/api/v1/upload",
			field:      "file",
			parseReply: parseTmpFilesReply,
		},
		{
			name:       "uguu.se",
			endpoint:   "https://uguu.se/upload",
			field:      "files[]",
			parseReply: parseUguuReply,
		},
		{
			name:       "temp.sh",
			endpoint:   "https://temp.sh/upload",
			field:      "file",
			parseReply: parseTempSHReply,
		},
	}
}

func uploadRelayReplica(ctx context.Context, client *http.Client, provider relayProvider, filename string, ciphertext []byte) (string, error) {
	if provider.upload != nil {
		return provider.upload(ctx, client, filename, ciphertext)
	}
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile(provider.field, filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(ciphertext); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-handoff/relay")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	return provider.parseReply(data)
}

func uploadFilebinReplica(ctx context.Context, client *http.Client, filename string, ciphertext []byte) (string, error) {
	endpoint := "https://filebin.net/" + idgen.NewUUID() + "/" + url.PathEscape(filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ciphertext))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-handoff/relay")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	return endpoint, nil
}

func parseTmpFilesReply(data []byte) (string, error) {
	var out struct {
		Status string `json:"status"`
		Data   struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("invalid response: %w", err)
	}
	if out.Status != "success" || strings.TrimSpace(out.Data.URL) == "" {
		return "", errors.New("response missing download url")
	}
	u, err := url.Parse(out.Data.URL)
	if err != nil {
		return "", errors.New("invalid download url")
	}
	if !strings.HasPrefix(u.Path, "/dl/") {
		u.Path = "/dl" + "/" + strings.TrimLeft(u.Path, "/")
	}
	return u.String(), nil
}

func parseUguuReply(data []byte) (string, error) {
	var out struct {
		Success bool `json:"success"`
		Files   []struct {
			URL string `json:"url"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("invalid response: %w", err)
	}
	if !out.Success || len(out.Files) == 0 || strings.TrimSpace(out.Files[0].URL) == "" {
		return "", errors.New("response missing download url")
	}
	return out.Files[0].URL, nil
}

func parseTempSHReply(data []byte) (string, error) {
	return strings.TrimSpace(string(data)), nil
}

func clampRelayTTL(requested int) int {
	if requested <= 0 {
		return RelayDefaultTTL
	}
	if requested < RelayMinTTL {
		return RelayMinTTL
	}
	if requested > RelayMaxTTL {
		return RelayMaxTTL
	}
	return requested
}

func validateResolverURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("resolver must be an https URL")
	}
	if u.Fragment != "" {
		return errors.New("resolver URL must not contain a fragment")
	}
	return nil
}

func encodeRelayLink(resolver string, m *RelayManifest) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return resolver + "#" + RelayFragmentParam + "=" + base64.RawURLEncoding.EncodeToString(data), nil
}

// ParseRelayLink validates a #h= link and returns its fragment manifest plus
// the resolver URL with the fragment removed.
func ParseRelayLink(rawURL string) (*RelayManifest, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid share url: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, "", errors.New("relay share url must use https")
	}
	frag := u.Fragment
	u.Fragment = ""
	q, err := url.ParseQuery(frag)
	if err != nil {
		return nil, "", errors.New("invalid relay share url fragment")
	}
	encoded := strings.TrimSpace(q.Get(RelayFragmentParam))
	if encoded == "" {
		return nil, "", errors.New("share url fragment has no h parameter")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(data) > 16<<10 {
		return nil, "", errors.New("relay share url fragment is malformed")
	}
	var m RelayManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, "", errors.New("relay share url fragment is malformed")
	}
	if err := m.Validate(time.Now()); err != nil {
		return nil, "", err
	}
	return &m, u.String(), nil
}

// Validate checks the full relay capability before any provider is contacted.
func (m *RelayManifest) Validate(now time.Time) error {
	if m.Version != RelayVersion {
		return fmt.Errorf("unsupported relay link version %d", m.Version)
	}
	if len(m.Replicas) == 0 || len(m.Replicas) > RelayMaxReplicas {
		return fmt.Errorf("invalid relay replica count %d", len(m.Replicas))
	}
	seen := make(map[string]bool, len(m.Replicas))
	for _, replica := range m.Replicas {
		if seen[replica.Provider] {
			return fmt.Errorf("duplicate relay provider %q", replica.Provider)
		}
		seen[replica.Provider] = true
		if err := validateRelayReplica(replica); err != nil {
			return err
		}
	}
	key, err := DecodeKey(m.Key)
	if err != nil || len(key) != KeySize {
		return errors.New("relay link key is malformed")
	}
	nonce, err := DecodeNonce(m.Nonce)
	if err != nil || len(nonce) != NonceSize {
		return errors.New("relay link nonce is malformed")
	}
	if len(m.SHA256) != 64 {
		return errors.New("relay link checksum is malformed")
	}
	for _, c := range m.SHA256 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("relay link checksum is malformed")
		}
	}
	if m.Bytes <= 0 || m.Bytes > RelayMaxBlobBytes {
		return fmt.Errorf("invalid relay blob size %d", m.Bytes)
	}
	expiresAt, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return errors.New("relay link expiry is malformed")
	}
	if !now.IsZero() && !expiresAt.After(now) {
		return errors.New("relay link has expired")
	}
	return nil
}

func validateRelayReplica(replica RelayReplica) error {
	u, err := url.Parse(replica.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid %s replica url", replica.Provider)
	}
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" {
		return fmt.Errorf("invalid %s replica port", replica.Provider)
	}
	switch replica.Provider {
	case "filebin.net":
		if host != "filebin.net" || strings.Count(strings.Trim(u.Path, "/"), "/") != 1 {
			return errors.New("filebin.net replica has an unexpected url")
		}
	case "tmpfiles.org":
		if host != "tmpfiles.org" || !strings.HasPrefix(u.Path, "/dl/") {
			return errors.New("tmpfiles.org replica has an unexpected url")
		}
	case "uguu.se":
		if !strings.HasSuffix(host, ".uguu.se") {
			return fmt.Errorf("uguu.se replica has unexpected host %q", host)
		}
	case "temp.sh":
		if host != "temp.sh" {
			return fmt.Errorf("temp.sh replica has unexpected host %q", host)
		}
	default:
		return fmt.Errorf("unsupported relay provider %q", replica.Provider)
	}
	return nil
}

// DownloadRelay tries each encrypted replica until one passes size, SHA-256,
// and AES-GCM authentication checks.
func DownloadRelay(rawURL string, client *http.Client) ([]byte, *RelayManifest, string, error) {
	m, cleanURL, err := ParseRelayLink(rawURL)
	if err != nil {
		return nil, nil, "", err
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	key, _ := DecodeKey(m.Key)
	nonce, _ := DecodeNonce(m.Nonce)
	errs := make([]string, 0, len(m.Replicas))
	for _, replica := range m.Replicas {
		ciphertext, err := fetchRelayBlob(client, replica, m.Bytes)
		if err != nil {
			errs = append(errs, replica.Provider+": "+err.Error())
			continue
		}
		if sha256hex(ciphertext) != m.SHA256 {
			errs = append(errs, replica.Provider+": checksum mismatch")
			continue
		}
		plaintext, err := Decrypt(ciphertext, key, nonce)
		if err != nil {
			errs = append(errs, replica.Provider+": "+err.Error())
			continue
		}
		return plaintext, m, cleanURL, nil
	}
	return nil, m, cleanURL, fmt.Errorf("all relay replicas failed: %s", strings.Join(errs, "; "))
}

func fetchRelayBlob(client *http.Client, replica RelayReplica, expectedBytes int64) ([]byte, error) {
	method := http.MethodGet
	if replica.Provider == "temp.sh" {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, replica.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "agent-handoff/relay")
	client = withRedirectPolicy(client, func(next *url.URL) error {
		_, err := validatePublicHTTPSURL(next.String(), "provider redirect")
		return err
	})
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	if resp.ContentLength > expectedBytes {
		return nil, fmt.Errorf("blob is larger than declared size %d", expectedBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, expectedBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expectedBytes {
		return nil, fmt.Errorf("blob size mismatch: expected %d, got %d", expectedBytes, len(data))
	}
	return data, nil
}

// DownloadURL loads either a self-hosted #k= link or a zero-config #h= link.
func DownloadURL(rawURL string, client *http.Client) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid share url: %w", err)
	}
	q, err := url.ParseQuery(u.Fragment)
	if err == nil && strings.TrimSpace(q.Get(RelayFragmentParam)) != "" {
		payload, _, cleanURL, err := DownloadRelay(rawURL, client)
		return payload, cleanURL, err
	}
	key, cleanURL, err := ParseLinkKey(rawURL)
	if err != nil {
		return nil, "", err
	}
	payload, _, err := Download(cleanURL, key, client)
	return payload, cleanURL, err
}
