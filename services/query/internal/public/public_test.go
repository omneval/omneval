package public

import "testing"

func TestIsPublic(t *testing.T) {
	tests := []struct {
		path    string
		isPublic bool
	}{
		{"/login", true},
		{"/logout", true},
		{"/healthz", true},
		{"/healthz/foo", true},
		{"/readyz", true},
		{"/readyz/deep", true},
		{"/metrics", true},
		{"/api/v1/scores", true},
		{"/api/v1/spans/query", false},
		{"/api/v1/traces/abc", false},
		{"/api/v1/projects", false},
		{"/", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := IsPublic(tc.path)
			if got != tc.isPublic {
				t.Errorf("IsPublic(%q) = %v, want %v", tc.path, got, tc.isPublic)
			}
		})
	}
}