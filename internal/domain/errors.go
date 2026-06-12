package domain

import "errors"

// ErrNotFound is returned by metadata stores when a requested entity does not
// exist. It lives in the domain package (a leaf in the import graph) so that
// store implementations and the metadata factory can share the sentinel
// without an import cycle. Callers typically reference it via the
// metadata.ErrNotFound alias.
var ErrNotFound = errors.New("metadata: not found")
