package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ManifestSchema identifies the link manifest format.
const ManifestSchema = "agent-handoff.link.v1"

// Manifest describes an uploaded encrypted share. The server stores it
// verbatim (after normalizing server-owned fields) and serves it at the
// manifest URL.
type Manifest struct {
	Schema     string `json:"schema"`
	CreatedAt  string `json:"created_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"` // requested lifetime; the selected service may clamp it

	Thread struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"thread"`

	Bundle struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"bundle"`

	Crypto struct {
		Alg    string `json:"alg"`
		Nonce  string `json:"nonce"`
		KeyRef string `json:"key_ref"`
	} `json:"crypto"`

	Import struct {
		Tool           string `json:"tool,omitempty"`
		Command        string `json:"command,omitempty"`
		InstallCommand string `json:"install_command,omitempty"`
		DocsURL        string `json:"docs_url,omitempty"`
	} `json:"import,omitempty"`
}

// Validate checks the manifest structure on the download side.
func (m *Manifest) Validate() error {
	if m.Schema != ManifestSchema {
		return fmt.Errorf("unsupported link schema %q", m.Schema)
	}
	if m.Bundle.URL == "" || m.Bundle.SHA256 == "" {
		return errors.New("invalid link manifest: missing bundle url/sha256")
	}
	if m.Crypto.Alg != KeyAlg {
		return fmt.Errorf("unsupported crypto algorithm %q", m.Crypto.Alg)
	}
	if m.Crypto.Nonce == "" {
		return errors.New("invalid link manifest: missing nonce")
	}
	if m.Crypto.KeyRef != KeyRef {
		return fmt.Errorf("unsupported key_ref %q", m.Crypto.KeyRef)
	}
	return nil
}

// ParseLinkKey extracts the raw key from a share URL fragment and returns
// the key with the cleaned URL (fragment removed).
func ParseLinkKey(rawURL string) (key []byte, cleanURL string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid share url: %w", err)
	}
	if _, err := validateHTTPSURL(rawURL, "share URL"); err != nil {
		return nil, "", err
	}
	frag := u.Fragment
	u.Fragment = ""
	if frag == "" {
		return nil, "", errors.New("share url is missing its #k= key fragment")
	}
	// The fragment is query-formatted: k=<base64url>.
	q, err := url.ParseQuery(frag)
	if err != nil {
		return nil, "", errors.New("invalid share url fragment")
	}
	k := strings.TrimSpace(q.Get("k"))
	if k == "" {
		return nil, "", errors.New("share url fragment has no k parameter")
	}
	key, err = DecodeKey(k)
	if err != nil || len(key) != KeySize {
		return nil, "", errors.New("share url key fragment is malformed")
	}
	return key, u.String(), nil
}

// AppendKeyFragment attaches the key to a share URL.
func AppendKeyFragment(shareURL string, key []byte) string {
	sep := "#"
	if strings.Contains(shareURL, "#") {
		sep = "&"
	}
	return shareURL + sep + "k=" + EncodeKey(key)
}

// marshalManifest is used by the uploader.
func marshalManifest(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
