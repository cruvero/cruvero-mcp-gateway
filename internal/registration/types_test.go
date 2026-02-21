package registration

import (
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestRegistrationRequestValidate(t *testing.T) {
	t.Parallel()

	valid := RegistrationRequest{
		ServiceName: "svc-alpha",
		Version:     "1.0.0",
		Listen:      ListenConfig{Host: "svc-alpha.default.svc", Port: 8443, Protocol: "https"},
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
	}

	tests := []struct {
		name    string
		mutate  func(r RegistrationRequest) RegistrationRequest
		wantErr bool
	}{
		{
			name: "valid request",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				return r
			},
			wantErr: false,
		},
		{
			name: "invalid service name",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.ServiceName = " bad name "
				return r
			},
			wantErr: true,
		},
		{
			name: "missing host",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Listen.Host = " "
				return r
			},
			wantErr: true,
		},
		{
			name: "port below range",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Listen.Port = 0
				return r
			},
			wantErr: true,
		},
		{
			name: "port above range",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Listen.Port = 65536
				return r
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Listen.Protocol = "grpc"
				return r
			},
			wantErr: true,
		},
		{
			name: "empty capabilities",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Capabilities.Tools = nil
				return r
			},
			wantErr: true,
		},
		{
			name: "empty tool entry",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Capabilities.Tools = []string{" "}
				return r
			},
			wantErr: true,
		},
		{
			name: "empty resource entry",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Capabilities.Tools = nil
				r.Capabilities.Resources = []string{" "}
				return r
			},
			wantErr: true,
		},
		{
			name: "empty prompt entry",
			mutate: func(r RegistrationRequest) RegistrationRequest {
				r.Capabilities.Tools = nil
				r.Capabilities.Prompts = []string{" "}
				return r
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := tt.mutate(valid)
			err := req.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidateHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		wantErr bool
		errMsg  string
	}{
		// Allowed hosts.
		{"private IPv4", "10.0.0.5", false, ""},
		{"k8s service DNS", "my-server.internal", false, ""},
		{"private 192.168", "192.168.1.100", false, ""},
		{"private 172.16", "172.16.0.10", false, ""},
		{"public IP", "203.0.113.50", false, ""},
		// Blocked hosts.
		{"loopback IPv4", "127.0.0.1", true, "loopback"},
		{"loopback IPv6", "::1", true, "loopback"},
		{"localhost", "localhost", true, "localhost not allowed"},
		{"sub.localhost", "sub.localhost", true, "localhost not allowed"},
		{"LOCALHOST upper", "LOCALHOST", true, "localhost not allowed"},
		{"cloud metadata IPv4", "169.254.169.254", true, "cloud metadata"},
		{"cloud metadata google", "metadata.google.internal", true, "cloud metadata"},
		{"cloud metadata goog", "metadata.goog", true, "cloud metadata"},
		{"unspecified IPv4", "0.0.0.0", true, "unspecified"},
		{"unspecified IPv6", "::", true, "unspecified"},
		{"link-local IPv4", "169.254.1.1", true, "link-local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateHost(tt.host)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for host %q", tt.host)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for host %q: %v", tt.host, err)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Fatalf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestRegistrationRequestValidate_SSRFHosts(t *testing.T) {
	t.Parallel()

	base := RegistrationRequest{
		ServiceName: "svc-test",
		Version:     "1.0.0",
		Listen:      ListenConfig{Port: 8443, Protocol: "https"},
		Capabilities: types.Capability{
			Tools: []string{"tool.test"},
		},
	}

	blocked := []string{"127.0.0.1", "localhost", "169.254.169.254", "metadata.google.internal", "0.0.0.0", "::1"}
	for _, host := range blocked {
		t.Run("blocked/"+host, func(t *testing.T) {
			t.Parallel()
			req := base
			req.Listen.Host = host
			err := req.Validate()
			if err == nil {
				t.Fatalf("expected validation error for host %q", host)
			}
			if !strings.Contains(err.Error(), "listen.host") {
				t.Fatalf("expected listen.host in error, got: %v", err)
			}
		})
	}

	allowed := []string{"10.0.0.5", "my-server.default.svc", "192.168.1.100"}
	for _, host := range allowed {
		t.Run("allowed/"+host, func(t *testing.T) {
			t.Parallel()
			req := base
			req.Listen.Host = host
			err := req.Validate()
			if err != nil {
				t.Fatalf("unexpected validation error for host %q: %v", host, err)
			}
		})
	}
}
