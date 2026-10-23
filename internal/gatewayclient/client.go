package gatewayclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const RequestTimeout = 10 * time.Second

// Client is the minimal HTTP transport for the OpenShell health endpoint.
type Client struct {
	Base    string
	Token   string
	HTTP    *http.Client
	tokenMu sync.RWMutex
}

func New(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: RequestTimeout}}
}

func NewWithToken(base, token string) *Client {
	client := New(base)
	client.Token = strings.TrimSpace(token)
	return client
}

// SetTLSCertificate configures a client certificate and optional CA bundle.
func (c *Client) SetTLSCertificate(certFile, keyFile, caFile string) error {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	config := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	if caFile != "" {
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return fmt.Errorf("no certificates in %s", caFile)
		}
		config.RootCAs = pool
	}
	transport.TLSClientConfig = config
	c.HTTP.Transport = transport
	return nil
}

func (c *Client) SetInsecureSkipVerify(skip bool) {
	if c == nil || c.HTTP == nil {
		return
	}
	if !skip {
		c.HTTP.Transport = http.DefaultTransport
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	c.HTTP.Transport = transport
}

func (c *Client) auth(request *http.Request) {
	c.tokenMu.RLock()
	token := c.Token
	c.tokenMu.RUnlock()
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

// SetToken replaces the bearer used by subsequent HTTP requests.
func (c *Client) SetToken(token string) {
	if c == nil {
		return
	}
	c.tokenMu.Lock()
	c.Token = strings.TrimSpace(token)
	c.tokenMu.Unlock()
}

func (c *Client) Healthz(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.get(ctx, "/healthz", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	c.auth(request)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("gateway read response body: %w", err)
		}
		return fmt.Errorf("gateway %s: %s: %s", path, response.Status, bytes.TrimSpace(body))
	}
	return json.NewDecoder(response.Body).Decode(dest)
}
