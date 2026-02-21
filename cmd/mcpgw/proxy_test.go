package main

import (
	"strings"
	"testing"
)

func TestMCPProxyCommand_MissingGatewayURL(t *testing.T) {
	err := mcpProxyCommand(nil)
	if err == nil {
		t.Fatalf("expected error for missing gateway-url")
	}
	if !strings.Contains(err.Error(), "gateway-url") {
		t.Fatalf("expected gateway-url error, got: %v", err)
	}
}

func TestExtractSSEData(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single data line",
			input: "data: {\"result\":\"ok\"}\n\n",
			want:  "{\"result\":\"ok\"}",
		},
		{
			name:  "multiple data lines",
			input: "data: line1\ndata: line2\n\n",
			want:  "line1\nline2",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "non-data lines ignored",
			input: "event: message\ndata: payload\nid: 1\n\n",
			want:  "payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(extractSSEData([]byte(tt.input)))
			if got != tt.want {
				t.Fatalf("extractSSEData() = %q, want %q", got, tt.want)
			}
		})
	}
}
