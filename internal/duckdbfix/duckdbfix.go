// Package duckdbfix provides a build-time fix for a Windows/mingw linker
// gap in duckdb-go/v2 v2.10503.1. See snprintf_windows.go for details. On
// non-Windows platforms this package is a no-op.
package duckdbfix
