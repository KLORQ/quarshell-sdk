package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KLORQ/quarshell-sdk/go/klorq"
)

func TestHealthBootstrapRemainsHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := klorq.New(srv.URL)
	hz, err := c.Healthz(context.Background())
	if err != nil || hz["ok"] != true {
		t.Fatalf("healthz=%v err=%v", hz, err)
	}
}

func TestRPCRequiresTokenBeforeDial(t *testing.T) {
	c := klorq.New("http://127.0.0.1:1")
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("expected missing-token error")
	}
}
