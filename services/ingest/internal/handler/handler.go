package handler

// OTLPHandler handles POST /v1/traces for OTLP protobuf and JSON payloads.
type OTLPHandler struct{}

// NativeHandler handles POST /api/v1/spans for the native Lantern REST format.
type NativeHandler struct{}
