package handlers_test

import (
	"net/http/httptest"
	"testing"

	"github.com/omneval/omneval/internal/handlers"
)

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name          string
		xAPIKey       string
		authorization string
		wantKey       string
	}{
		{
			name:    "X-API-Key only",
			xAPIKey: "oev_proj_test123",
			wantKey: "oev_proj_test123",
		},
		{
			name:          "Authorization Bearer only",
			authorization: "Bearer oev_proj_bearer456",
			wantKey:       "oev_proj_bearer456",
		},
		{
			name:          "both present — X-API-Key wins",
			xAPIKey:       "oev_proj_primary",
			authorization: "Bearer oev_proj_secondary",
			wantKey:       "oev_proj_primary",
		},
		{
			name:    "neither present",
			wantKey: "",
		},
		{
			name:          "malformed Authorization — no Bearer prefix",
			authorization: "Basic YWJj",
			wantKey:       "",
		},
		{
			name:          "malformed Authorization — Bearer with no token",
			authorization: "Bearer",
			wantKey:       "",
		},
		{
			name:          "malformed Authorization — Bearer with spaces only",
			authorization: "Bearer  ",
			wantKey:       "",
		},
		{
			name:    "service key via X-API-Key",
			xAPIKey: "oev_svc_writer",
			wantKey: "oev_svc_writer",
		},
		{
			name:          "service key via Bearer",
			authorization: "Bearer oev_svc_reader",
			wantKey:       "oev_svc_reader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/traces", nil)
			if tt.xAPIKey != "" {
				r.Header.Set("X-API-Key", tt.xAPIKey)
			}
			if tt.authorization != "" {
				r.Header.Set("Authorization", tt.authorization)
			}

			got := handlers.ExtractAPIKey(r)
			if got != tt.wantKey {
				t.Errorf("ExtractAPIKey() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}
