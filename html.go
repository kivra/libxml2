package libxml2

import (
	"bytes"
	"io"

	"github.com/kivra/libxml2/clib"
	"github.com/kivra/libxml2/dom"
	"github.com/kivra/libxml2/parser"
	"github.com/kivra/libxml2/types"
	"github.com/pkg/errors"
)

// ParseHTML parses an HTML document. You can omit the options
// argument, or you can provide one bitwise-or'ed option
func ParseHTML(content []byte, options ...parser.HTMLOption) (types.Document, error) {
	if len(content) > MaxInputBytes {
		return nil, ErrInputTooLarge
	}
	return ParseHTMLString(string(content), options...)
}

// ParseHTMLString parses an HTML document. You can omit the options
// argument, or you can provide one bitwise-or'ed option
func ParseHTMLString(content string, options ...parser.HTMLOption) (types.Document, error) {
	var option parser.HTMLOption
	if len(options) > 0 {
		option = options[0]
	} else {
		option = parser.DefaultHTMLOptions
	}
	docptr, err := clib.HTMLReadDoc(content, "", "", int(option))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read document")
	}

	if docptr == nil {
		return nil, errors.Wrap(clib.ErrInvalidDocument, "failed to get valid document pointer")
	}
	return dom.WrapDocument(docptr), nil
}

// ParseHTMLReader parses an HTML document. You can omit the options
// argument, or you can provide one bitwise-or'ed option
func ParseHTMLReader(in io.Reader, options ...parser.HTMLOption) (types.Document, error) {
	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(io.LimitReader(in, MaxInputBytes+1)); err != nil {
		return nil, errors.Wrap(err, "failed to rea from io.Reader")
	}
	if buf.Len() > MaxInputBytes {
		return nil, ErrInputTooLarge
	}

	return ParseHTMLString(buf.String(), options...)
}
