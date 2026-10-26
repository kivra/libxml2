package libxml2

import (
	"errors"
	"io"
	"math"

	"github.com/kivra/libxml2/clib"
	"github.com/kivra/libxml2/dom"
	"github.com/kivra/libxml2/types"
)

var (
	ErrRestrictedParsingUnsupported = clib.ErrRestrictedParsingUnsupported
	ErrResourceDenied               = clib.ErrResourceDenied
	ErrNestedSchemaReferences       = clib.ErrNestedSchemaReferences
	ErrInputTooLarge                = errors.New("input exceeds caller-specified limit")
	ErrInvalidInputLimit            = errors.New("input limit must be positive and less than MaxInt64")
)

// RestrictedParsingSupported reports whether the linked libxml2 supports the
// required per-context resource loaders. Restricted APIs fail closed otherwise.
func RestrictedParsingSupported() bool {
	return clib.RestrictedParsingSupported()
}

// ParseRestricted rejects DTDs and denies all external resources. It does not
// enable entity substitution, DTD processing, XInclude, recovery, or HUGE.
// It requires libxml2 2.14 or newer and does not impose an application size policy.
func ParseRestricted(buf []byte) (types.Document, error) {
	ptr, err := clib.XMLReadRestricted(buf)
	if err != nil {
		return nil, err
	}
	return dom.WrapDocument(ptr), nil
}

// ParseRestrictedReader reads at most maxInputBytes+1 bytes and rejects oversized
// input rather than parsing a truncated document. The caller chooses the limit.
func ParseRestrictedReader(reader io.Reader, maxInputBytes int64) (types.Document, error) {
	if maxInputBytes <= 0 || maxInputBytes == math.MaxInt64 {
		return nil, ErrInvalidInputLimit
	}
	if !RestrictedParsingSupported() {
		return nil, ErrRestrictedParsingUnsupported
	}
	buf, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxInputBytes {
		return nil, ErrInputTooLarge
	}
	return ParseRestricted(buf)
}
