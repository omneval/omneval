package cursor

import (
	"testing"
	"time"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	original := Cursor{
		StartTime: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		SpanID:    "abc123def456",
	}

	encoded := Encode(original)
	if encoded == "" {
		t.Fatal("Encode returned empty string")
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	if !decoded.StartTime.Equal(original.StartTime) {
		t.Errorf("StartTime: got %v, want %v", decoded.StartTime, original.StartTime)
	}
	if decoded.SpanID != original.SpanID {
		t.Errorf("SpanID: got %q, want %q", decoded.SpanID, original.SpanID)
	}
}

func TestEncode_NanoPrecision(t *testing.T) {
	// Verify nanosecond precision is preserved.
	original := Cursor{
		StartTime: time.Date(2025, 6, 15, 10, 30, 45, 123456789, time.UTC),
		SpanID:    "span001",
	}

	encoded := Encode(original)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded.StartTime.UnixNano() != original.StartTime.UnixNano() {
		t.Errorf("UnixNano: got %d, want %d", decoded.StartTime.UnixNano(), original.StartTime.UnixNano())
	}
}

func TestDecode_InvalidBase64(t *testing.T) {
	_, err := Decode("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	// Valid base64 but invalid JSON.
	_, err := Decode("ewogICJmb28iOiAiYmFyIgogfQ==") // {"foo": "bar"}
	if err == nil {
		t.Error("expected error for invalid JSON structure")
	}
}

func TestDecode_Empty(t *testing.T) {
	_, err := Decode("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestEncode_EmptySpanID(t *testing.T) {
	// Edge case: empty span ID should still encode/decode correctly.
	c := Cursor{
		StartTime: time.Now(),
		SpanID:    "",
	}
	encoded := Encode(c)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if decoded.SpanID != "" {
		t.Errorf("SpanID: got %q, want empty", decoded.SpanID)
	}
}

func TestEncode_DecodingIsURLSafe(t *testing.T) {
	// Verify the encoding doesn't contain +, /, or = which would break URL safety.
	c := Cursor{
		StartTime: time.Date(2025, 1, 1, 0, 0, 0, 999999999, time.UTC),
		SpanID:    "span-with-dashes_and_underscores",
	}
	encoded := Encode(c)
	for _, r := range encoded {
		if r == '+' || r == '/' || r == '=' {
			t.Errorf("encoded cursor contains URL-unsafe character: %q", string(r))
		}
	}
}
