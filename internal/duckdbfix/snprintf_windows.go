// Package duckdbfix provides a build-time fix for a Windows/mingw linker
// gap introduced by duckdb-go/v2 v2.10503.1 (DuckDB 1.5.3): the bundled
// duckdb-go-bindings v0.10503.0 static libs for windows/amd64 reference the
// MSVC CRT symbol _snprintf (e.g. from strutil.cc), but the mingw
// toolchain's libmsvcrt/libucrt archives only export the unprefixed
// snprintf/__mingw_snprintf, and supplying -lmsvcrt-os via CGO_LDFLAGS
// doesn't help because it lands before -lduckdb_static on the link line
// (ld resolves left-to-right and won't revisit it).
//
// Every binary that links duckdb on windows/amd64 must import this package
// (blank import is sufficient) exactly once in its dependency graph so the
// linker finds _snprintf in this package's object file, which always
// precedes the static libs on the link line. Do not duplicate this shim in
// other packages — multiple definitions of _snprintf across the link will
// fail with a "multiple definition" error.
//
// TODO(#111): remove this shim if/when duckdb-go-bindings ships a
// windows-amd64 static lib that doesn't depend on _snprintf, or links
// against a CRT that provides it.
package duckdbfix

/*
#include <stdarg.h>
#include <stddef.h>

extern int __mingw_vsnprintf(char *buffer, size_t count, const char *format, va_list args);

int _snprintf(char *buffer, size_t count, const char *format, ...) {
	va_list args;
	va_start(args, format);
	int r = __mingw_vsnprintf(buffer, count, format, args);
	va_end(args);
	return r;
}
*/
import "C"
