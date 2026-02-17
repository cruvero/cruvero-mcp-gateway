package registration

import (
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
