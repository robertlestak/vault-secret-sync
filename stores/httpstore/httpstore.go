package httpstore

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httputil"
	"slices"

	"github.com/robertlestak/vault-secret-sync/pkg/driver"
	"github.com/robertlestak/vault-secret-sync/pkg/kubesecret"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HTTPClient struct {
	URL          string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	HeaderSecret string            `yaml:"headerSecret,omitempty" json:"headerSecret,omitempty"`
	Template     string            `yaml:"template,omitempty" json:"template,omitempty"`
	Method       string            `yaml:"method,omitempty" json:"method,omitempty"`

	SuccessCodes []int `yaml:"successCodes,omitempty" json:"successCodes,omitempty"`

	client *http.Client `yaml:"-" json:"-"`
}

// DeepCopyInto copies all properties from this object into another object of the same type
func (in *HTTPClient) DeepCopyInto(out *HTTPClient) {
	*out = *in

	// Deep copy for the Headers map
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		for key, val := range in.Headers {
			out.Headers[key] = val
		}
	}

	if in.SuccessCodes != nil {
		out.SuccessCodes = make([]int, len(in.SuccessCodes))
		copy(out.SuccessCodes, in.SuccessCodes)
	}

	// Note: The http.Client is not deep copied because it is typically not a value type and its fields are often unexported.
	// It is assumed that the client will be re-initialized as needed.
	out.client = in.client
}

func (in *HTTPClient) DeepCopy() *HTTPClient {
	if in == nil {
		return nil
	}
	out := new(HTTPClient)
	in.DeepCopyInto(out)
	return out
}

func NewClient(cfg *HTTPClient) (*HTTPClient, error) {
	l := log.WithFields(log.Fields{
		"action": "NewClient",
	})
	l.Trace("start")
	vc := &HTTPClient{}
	jd, err := json.Marshal(cfg)
	if err != nil {
		l.Debugf("error: %v", err)
		return nil, err
	}
	err = json.Unmarshal(jd, &vc)
	if err != nil {
		l.Debugf("error: %v", err)
		return nil, err
	}
	l.Debugf("client=%+v", vc)
	l.Trace("end")
	return vc, nil
}

// Validate the HTTP client configuration
func (h *HTTPClient) Validate() error {
	if h.URL == "" {
		return errors.New("URL is required")
	}
	return nil
}

// Meta returns metadata for the HTTP client
func (h *HTTPClient) Meta() map[string]any {
	jd, err := json.Marshal(h)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(jd, &m); err != nil {
		return nil
	}
	return m
}

// Init initializes the HTTP client
func (h *HTTPClient) Init(ctx context.Context) error {
	h.client = &http.Client{}
	return h.Validate()
}

// Driver returns the driver name
func (h *HTTPClient) Driver() driver.DriverName {
	return driver.DriverNameHttp
}

// GetPath returns the path
func (h *HTTPClient) GetPath() string {
	return h.URL
}

// ApplyTemplate applies the configured template to the secret data
func (h *HTTPClient) ApplyTemplate(secrets map[string]any) (string, error) {
	if h.Template == "" {
		// No template provided, return the secrets as JSON
		payload, err := json.Marshal(secrets)
		if err != nil {
			return "", err
		}
		return string(payload), nil
	}

	// Parse and execute the template
	tmpl, err := template.New("secretTemplate").Parse(h.Template)
	if err != nil {
		return "", err
	}

	var tplOutput bytes.Buffer
	err = tmpl.Execute(&tplOutput, secrets)
	if err != nil {
		return "", err
	}

	return tplOutput.String(), nil
}

// GetSecret retrieves a secret from the HTTP URL
func (h *HTTPClient) GetSecret(ctx context.Context, path string) (map[string]any, error) {
	url := path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	for key, value := range h.Headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get secret: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var secret map[string]any
	if err := json.Unmarshal(body, &secret); err != nil {
		return nil, err
	}

	return secret, nil
}

// WriteSecret writes a secret to the HTTP URL
func (h *HTTPClient) WriteSecret(ctx context.Context, meta metav1.ObjectMeta, path string, secrets map[string]any) (map[string]any, error) {
	l := log.WithFields(log.Fields{
		"action": "WriteSecret",
		"path":   path,
		"store":  "http",
	})
	l.Trace("start")
	defer l.Trace("end")
	url := path
	payload, err := h.ApplyTemplate(secrets)
	if err != nil {
		l.WithError(err).Error("failed to apply template")
		return nil, err
	}
	method := cmp.Or(h.Method, http.MethodPost)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer([]byte(payload)))
	if err != nil {
		l.WithError(err).Error("failed to create request")
		return nil, err
	}
	if h.HeaderSecret != "" {
		sc, err := kubesecret.GetSecret(ctx, meta.Namespace, h.HeaderSecret)
		if err != nil {
			l.WithError(err).Error("failed to get header secret")
			return nil, err
		}
		for key, value := range sc {
			h.Headers[key] = string(value)
		}
	}
	for key, value := range h.Headers {
		req.Header.Set(key, value)
	}
	// debug log the whole request
	httpReqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		l.WithError(err).Error("failed to dump request")
		return nil, err
	}
	l.Debugf("request=%s", string(httpReqDump))
	// send the request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to write secret: %s", resp.Status)
	}
	if len(h.SuccessCodes) == 0 {
		h.SuccessCodes = []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent}
	}
	if !slices.Contains(h.SuccessCodes, resp.StatusCode) {
		return nil, fmt.Errorf("failed to write secret: %s", resp.Status)
	}
	return secrets, nil
}

// DeleteSecret deletes a secret from the HTTP URL
func (h *HTTPClient) DeleteSecret(ctx context.Context, path string) error {
	url := path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	for key, value := range h.Headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete secret: %s", resp.Status)
	}

	return nil
}

// ListSecrets lists secrets from the HTTP URL
func (h *HTTPClient) ListSecrets(ctx context.Context, path string) ([]string, error) {
	url := path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	for key, value := range h.Headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list secrets: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var secrets []string
	if err := json.Unmarshal(body, &secrets); err != nil {
		return nil, err
	}

	return secrets, nil
}

// SetDefaults sets default values for the HTTP client
func (h *HTTPClient) SetDefaults(cfg any) error {
	jd, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return json.Unmarshal(jd, h)
}

// Close closes the HTTP client
func (h *HTTPClient) Close() error {
	h.client = nil
	return nil
}
