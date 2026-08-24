package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxProviderConfigBytes = 1 << 20

// ProviderConfig is the user-owned declarative HTTP provider configuration.
type ProviderConfig struct {
	Providers []HTTPProviderConfig `json:"providers"`
}

// HTTPProviderConfig describes one encrypted-file upload API.
type HTTPProviderConfig struct {
	Name           string            `json:"name"`
	UploadURL      string            `json:"upload_url"`
	UploadType     string            `json:"upload_type"`
	FileField      string            `json:"file_field,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	FormFields     map[string]string `json:"form_fields,omitempty"`
	ResponseType   string            `json:"response_type"`
	URLJSONPointer string            `json:"url_json_pointer,omitempty"`
}

// DefaultProviderConfigPath returns the platform-native user configuration path.
func DefaultProviderConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "agent-handoff", "config.json"), nil
}

// ResolveProviderConfig loads an explicit config or the default config when it
// exists. A missing default config means zero-config mode.
func ResolveProviderConfig(flagPath string) (*ProviderConfig, string, error) {
	explicit := strings.TrimSpace(flagPath) != ""
	path := strings.TrimSpace(flagPath)
	if !explicit {
		var err error
		path, err = DefaultProviderConfigPath()
		if err != nil {
			return nil, "", err
		}
	}
	config, err := readProviderConfig(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil, path, nil
		}
		return nil, path, err
	}
	if len(config.Providers) == 0 {
		return nil, path, fmt.Errorf("provider config %q must contain at least one provider", path)
	}
	seen := make(map[string]struct{}, len(config.Providers))
	for i := range config.Providers {
		if err := config.Providers[i].validate(); err != nil {
			return nil, path, fmt.Errorf("provider config %q: providers[%d]: %w", path, i, err)
		}
		name := config.Providers[i].Name
		if _, duplicate := seen[name]; duplicate {
			return nil, path, fmt.Errorf("provider config %q: duplicate provider name %q", path, name)
		}
		seen[name] = struct{}{}
	}
	return config, path, nil
}

func readProviderConfig(path string) (*ProviderConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read provider config %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read provider config %q: %w", path, err)
	}
	if info.Size() > maxProviderConfigBytes {
		return nil, fmt.Errorf("read provider config %q: file exceeds %d bytes", path, maxProviderConfigBytes)
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxProviderConfigBytes+1))
	decoder.DisallowUnknownFields()
	var config ProviderConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("read provider config %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("read provider config %q: %w", path, err)
	}
	return &config, nil
}

func (p HTTPProviderConfig) validate() error {
	if err := validateConfiguredProviderID("config:" + p.Name); err != nil {
		return err
	}
	if _, err := validatePublicHTTPSURL(p.UploadURL, "upload_url"); err != nil {
		return err
	}
	switch p.UploadType {
	case "multipart":
		if strings.TrimSpace(p.FileField) == "" {
			return errors.New("multipart provider requires file_field")
		}
	case "raw":
		if p.FileField != "" || len(p.FormFields) > 0 {
			return errors.New("raw provider cannot set file_field or form_fields")
		}
	default:
		return errors.New("upload_type must be multipart or raw")
	}
	switch p.ResponseType {
	case "json":
		if !strings.HasPrefix(p.URLJSONPointer, "/") {
			return errors.New("json response requires url_json_pointer")
		}
	case "text":
		if p.URLJSONPointer != "" {
			return errors.New("text response cannot set url_json_pointer")
		}
	default:
		return errors.New("response_type must be json or text")
	}
	for header := range p.Headers {
		switch strings.ToLower(strings.TrimSpace(header)) {
		case "host", "content-length", "connection", "transfer-encoding":
			return fmt.Errorf("header %q is not allowed", header)
		}
	}
	return nil
}

func validateConfiguredProviderID(provider string) error {
	if !strings.HasPrefix(provider, "config:") {
		return errors.New("configured provider id must have config prefix")
	}
	name := strings.TrimPrefix(provider, "config:")
	if name == "" || len(name) > 64 {
		return errors.New("configured provider has an invalid name")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return errors.New("configured provider has an invalid name")
		}
	}
	return nil
}

func configuredRelayProviders(config *ProviderConfig, ttlSeconds int) []relayProvider {
	providers := make([]relayProvider, 0, len(config.Providers))
	for i := range config.Providers {
		configured := config.Providers[i]
		providers = append(providers, relayProvider{
			name: "config:" + configured.Name,
			upload: func(ctx context.Context, client *http.Client, filename string, ciphertext []byte) (string, error) {
				return configured.upload(ctx, client, filename, ciphertext, ttlSeconds)
			},
		})
	}
	return providers
}

func (p HTTPProviderConfig) upload(ctx context.Context, client *http.Client, filename string, ciphertext []byte, ttlSeconds int) (string, error) {
	values := map[string]string{
		"filename":    filename,
		"bytes":       strconv.Itoa(len(ciphertext)),
		"sha256":      sha256hex(ciphertext),
		"ttl_seconds": strconv.Itoa(ttlSeconds),
	}
	uploadURL, err := expandProviderValue(p.UploadURL, values)
	if err != nil {
		return "", err
	}
	if _, err := validatePublicHTTPSURL(uploadURL, "upload_url"); err != nil {
		return "", err
	}

	var body io.Reader
	contentType := "application/octet-stream"
	if p.UploadType == "multipart" {
		buffer := &bytes.Buffer{}
		writer := multipart.NewWriter(buffer)
		for name, value := range p.FormFields {
			expanded, err := expandProviderValue(value, values)
			if err != nil {
				return "", fmt.Errorf("form field %q: %w", name, err)
			}
			if err := writer.WriteField(name, expanded); err != nil {
				return "", err
			}
		}
		part, err := writer.CreateFormFile(p.FileField, filename)
		if err != nil {
			return "", err
		}
		if _, err := part.Write(ciphertext); err != nil {
			return "", err
		}
		if err := writer.Close(); err != nil {
			return "", err
		}
		body = buffer
		contentType = writer.FormDataContentType()
	} else {
		body = bytes.NewReader(ciphertext)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json, text/plain")
	req.Header.Set("User-Agent", "agent-handoff/configured-provider")
	for name, value := range p.Headers {
		expanded, err := expandProviderValue(value, values)
		if err != nil {
			return "", fmt.Errorf("header %q: %w", name, err)
		}
		req.Header.Set(name, expanded)
	}
	client = withRedirectPolicy(client, func(next *url.URL) error {
		_, err := validatePublicHTTPSURL(next.String(), "provider redirect")
		return err
	})
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, truncateBody(data))
	}
	downloadURL, err := p.responseURL(data)
	if err != nil {
		return "", err
	}
	base, _ := url.Parse(uploadURL)
	ref, err := url.Parse(downloadURL)
	if err != nil {
		return "", errors.New("provider returned an invalid download url")
	}
	resolved := base.ResolveReference(ref).String()
	if _, err := validatePublicHTTPSURL(resolved, "provider download url"); err != nil {
		return "", err
	}
	return resolved, nil
}

func (p HTTPProviderConfig) responseURL(data []byte) (string, error) {
	if p.ResponseType == "text" {
		value := strings.TrimSpace(string(data))
		if value == "" {
			return "", errors.New("provider response missing download url")
		}
		return value, nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("provider returned invalid JSON: %w", err)
	}
	resolved, err := resolveJSONPointer(value, p.URLJSONPointer)
	if err != nil {
		return "", err
	}
	text, ok := resolved.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", errors.New("provider response url is not a string")
	}
	return strings.TrimSpace(text), nil
}

func resolveJSONPointer(value any, pointer string) (any, error) {
	current := value
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = node[part]
			if !ok {
				return nil, fmt.Errorf("provider response has no value at %s", pointer)
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, fmt.Errorf("provider response has no value at %s", pointer)
			}
			current = node[index]
		default:
			return nil, fmt.Errorf("provider response has no value at %s", pointer)
		}
	}
	return current, nil
}

func expandProviderValue(value string, builtins map[string]string) (string, error) {
	for key, replacement := range builtins {
		value = strings.ReplaceAll(value, "{"+key+"}", replacement)
	}
	for {
		start := strings.Index(value, "${")
		if start < 0 {
			return value, nil
		}
		end := strings.Index(value[start+2:], "}")
		if end < 0 {
			return "", errors.New("unterminated environment variable reference")
		}
		end += start + 2
		name := value[start+2 : end]
		if !validEnvironmentName(name) {
			return "", errors.New("invalid environment variable reference")
		}
		replacement, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		value = value[:start] + replacement + value[end+1:]
	}
}

func validEnvironmentName(name string) bool {
	if name == "" || ((name[0] < 'A' || name[0] > 'Z') && (name[0] < 'a' || name[0] > 'z') && name[0] != '_') {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
