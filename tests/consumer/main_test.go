package consumer

import (
	"testing"

	"github.com/KLORQ/quarshell-sdk/go/klorq"
)

func TestPublicClientConstruction(t *testing.T) {
	client := klorq.New("http://127.0.0.1:7443")
	t.Cleanup(func() { _ = client.Close() })

	if client.Workspace != "" {
		t.Fatalf("new client Workspace = %q, want empty default", client.Workspace)
	}
}
