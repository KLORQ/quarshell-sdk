package klorq

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type gatewayCredentials struct {
	token     func() string
	edgeToken string
}

func (c gatewayCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	if c.token != nil {
		if token := c.token(); token != "" {
			return map[string]string{"authorization": "Bearer " + token}, nil
		}
	}
	if c.edgeToken != "" {
		return map[string]string{
			"cf-access-jwt-assertion": c.edgeToken,
			"cookie":                  "CF_Authorization=" + c.edgeToken,
		}, nil
	}
	return nil, nil
}

// gatewayConnection creates the generated OpenShell transport. The host alias
// is injected by the CLI into sandbox proxy environments as a trusted route.
func (c *Client) gatewayConnection() (*grpc.ClientConn, error) {
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	if c.gatewayConn != nil {
		return c.gatewayConn, nil
	}
	baseEndpoint, err := url.Parse(c.base)
	if err != nil || baseEndpoint.Host == "" || baseEndpoint.Path != "" || baseEndpoint.RawQuery != "" || baseEndpoint.Fragment != "" {
		return nil, fmt.Errorf("gateway RPC: base URL must contain only scheme and authority")
	}
	endpoint := *baseEndpoint
	if configured := strings.TrimSpace(c.rpcEndpoint); configured != "" {
		if strings.Contains(configured, "://") {
			configuredEndpoint, parseErr := url.Parse(configured)
			if parseErr != nil || configuredEndpoint.Host == "" || configuredEndpoint.Path != "" || configuredEndpoint.RawQuery != "" || configuredEndpoint.Fragment != "" {
				return nil, fmt.Errorf("gateway RPC: endpoint must contain only scheme and authority")
			}
			endpoint = *configuredEndpoint
		} else {
			endpoint.Host = configured
		}
	}
	var transport credentials.TransportCredentials
	switch strings.ToLower(endpoint.Scheme) {
	case "https":
		config := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.insecureTLS}
		if c.tlsCertFile != "" && c.tlsKeyFile != "" {
			certificate, err := tls.LoadX509KeyPair(c.tlsCertFile, c.tlsKeyFile)
			if err != nil {
				return nil, fmt.Errorf("gateway RPC: client certificate: %w", err)
			}
			config.Certificates = []tls.Certificate{certificate}
		}
		if c.tlsCAFile != "" {
			pemBytes, err := os.ReadFile(c.tlsCAFile)
			if err != nil {
				return nil, fmt.Errorf("gateway RPC: CA bundle: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pemBytes) {
				return nil, fmt.Errorf("gateway RPC: empty CA bundle")
			}
			config.RootCAs = pool
		}
		transport = credentials.NewTLS(config)
	case "http":
		host := strings.ToLower(endpoint.Hostname())
		if !isLoopbackHost(host) && host != "host.quarshell.internal" {
			return nil, fmt.Errorf("gateway RPC: unencrypted gRPC is allowed only for local gateway routes")
		}
		transport = insecure.NewCredentials()
	default:
		return nil, fmt.Errorf("gateway RPC: unsupported URL scheme %q", endpoint.Scheme)
	}
	conn, err := grpc.NewClient(endpoint.Host,
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(gatewayCredentials{token: c.authToken, edgeToken: c.edgeToken}),
	)
	if err != nil {
		return nil, fmt.Errorf("gateway RPC: connect: %w", err)
	}
	c.gatewayConn = conn
	return conn, nil
}

func (c *Client) gatewayClient() (gatewayv1.OpenShellClient, error) {
	conn, err := c.gatewayConnection()
	if err != nil {
		return nil, err
	}
	return gatewayv1.NewOpenShellClient(conn), nil
}

func (gatewayCredentials) RequireTransportSecurity() bool { return false }

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
