package gatewayclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestHealthzUsesOnlyTheHTTPHealthRoute(t *testing.T) {
	client := NewWithToken("http://gateway.example", "health-token")
	client.HTTP.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/healthz" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer health-token" {
			t.Fatalf("authorization=%q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`))}, nil
	})
	result, err := client.Healthz(context.Background())
	if err != nil || result["status"] != "ok" {
		t.Fatalf("healthz=%v err=%v", result, err)
	}
}

func TestSetTokenRotatesHTTPBearer(t *testing.T) {
	client := NewWithToken("http://gateway.example", "initial")
	client.SetToken("rotated")
	client.HTTP.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer rotated" {
			t.Fatalf("authorization=%q, want rotated bearer", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`))}, nil
	})
	if _, err := client.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}
}
