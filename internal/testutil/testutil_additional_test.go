package testutil

import (
	"strings"
	"testing"
)

func TestSetupTestNATSProvidesWorkingConnection(t *testing.T) {
	conn, url := SetupTestNATS(t)
	if conn == nil {
		t.Fatal("expected non-nil nats connection")
	}
	if !strings.HasPrefix(url, "nats://") {
		t.Fatalf("expected nats:// url, got %q", url)
	}
	if !conn.IsConnected() {
		t.Fatal("expected connected nats client")
	}
}
