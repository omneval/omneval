package domain

import (
	"testing"
)

func TestAttributeValue(t *testing.T) {
	tests := []struct {
		name   string
		attrs  map[string]any
		key    string
		expect string
	}{
		{
			name:   "top-level key",
			attrs:  map[string]any{"user_id": "abc-123"},
			key:    "user_id",
			expect: "abc-123",
		},
		{
			name:   "top-level key missing",
			attrs:  map[string]any{},
			key:    "user_id",
			expect: "",
		},
		{
			name:   "top-level key nil value",
			attrs:  map[string]any{"user_id": nil},
			key:    "user_id",
			expect: "",
		},
		{
			name: "nested dot path",
			attrs: map[string]any{
				"metadata": `{"user_id": "usr-456"}`,
			},
			key:    "metadata.user_id",
			expect: "usr-456",
		},
		{
			name: "nested depth 3",
			attrs: map[string]any{
				"a": `{"b": {"c": "deep-value"}}`,
			},
			key:    "a.b.c",
			expect: "deep-value",
		},
		{
			name: "nested depth 5",
			attrs: map[string]any{
				"a": `{"b": {"c": {"d": {"e": "max-depth"}}}}`,
			},
			key:    "a.b.c.d.e",
			expect: "max-depth",
		},
		{
			name: "missing intermediate key",
			attrs: map[string]any{
				"metadata": `{"user_id": "abc"}`,
			},
			key:    "metadata.extra",
			expect: "",
		},
		{
			name: "non-JSON intermediate",
			attrs: map[string]any{
				"metadata": "just-a-string",
			},
			key:    "metadata.user_id",
			expect: "",
		},
		{
			name: "non-string intermediate value",
			attrs: map[string]any{
				"metadata": `{"count": 42}`,
			},
			key:    "metadata.count",
			expect: "42",
		},
		{
			name:   "nil attributes",
			attrs:  nil,
			key:    "user_id",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &Span{Attributes: tt.attrs}
			got := attributeValue(span, tt.key)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestEvalFilter_CompilePatterns(t *testing.T) {
	tests := []struct {
		name    string
		filters []AttributeRegexFilter
		wantErr bool
	}{
		{
			name: "valid patterns",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: `.*-123$`},
				{Key: "tier", Pattern: `premium|basic`},
			},
			wantErr: false,
		},
		{
			name: "empty pattern",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: ""},
			},
			wantErr: false,
		},
		{
			name: "invalid pattern",
			filters: []AttributeRegexFilter{
				{Key: "user_id", Pattern: `[invalid`},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := EvalFilter{AttributesMatch: tt.filters}
			err := f.CompilePatterns()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CompilePatterns() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvalFilter_ValidateDotPaths(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "top-level key", key: "user_id", wantErr: false},
		{name: "depth 5 OK", key: "a.b.c.d.e", wantErr: false},
		{name: "depth 6 exceeds limit", key: "a.b.c.d.e.f", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := EvalFilter{
				AttributesMatch: []AttributeRegexFilter{
					{Key: tt.key, Pattern: `.*`},
				},
			}
			err := f.ValidateDotPaths()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDotPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMatchesFilterAttributeRegex(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]any
		af    AttributeRegexFilter
		want  bool
	}{
		{
			name: "match",
			attrs: map[string]any{
				"metadata": `{"user_id": "abc-123"}`,
			},
			af:   AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`},
			want: true,
		},
		{
			name: "no match",
			attrs: map[string]any{
				"metadata": `{"user_id": "xyz-789"}`,
			},
			af:   AttributeRegexFilter{Key: "metadata.user_id", Pattern: `abc-123`},
			want: false,
		},
		{
			name:  "missing key",
			attrs: map[string]any{},
			af:    AttributeRegexFilter{Key: "user_id", Pattern: `.*`},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &Span{Attributes: tt.attrs}
			got := MatchesFilterAttributeRegex(span, tt.af)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
