package xsd

import "github.com/kivra/libxml2/clib"

// ParseRestricted compiles a trusted schema bundle without filesystem or network
// access. baseURI identifies buf; resources maps fully resolved import/include
// URIs to trusted bytes supplied by the caller. Root references may be relative
// to baseURI. Resource documents must not specify further schema locations:
// these return libxml2.ErrNestedSchemaReferences because native nested schema
// contexts do not reliably inherit resource loaders. Namespace-only imports
// remain supported. Missing resources fail closed, even when libxml2 would
// otherwise treat a missing import as a warning.
//
// Every supplied document must be well-formed and DTD-free. Do not construct the
// bundle from untrusted document attributes. Requires libxml2 2.14 or newer.
func ParseRestricted(buf []byte, baseURI string, resources map[string][]byte) (*Schema, error) {
	ptr, err := clib.XMLSchemaParseRestricted(buf, baseURI, resources)
	if err != nil {
		return nil, err
	}
	return &Schema{ptr: ptr}, nil
}
