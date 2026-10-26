package libxml2_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kivra/libxml2"
	"github.com/kivra/libxml2/clib"
	"github.com/kivra/libxml2/parser"
	"github.com/kivra/libxml2/types"
	"github.com/kivra/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputSizeLimit(t *testing.T) {
	input := strings.Repeat(" ", libxml2.MaxInputBytes+1)
	tests := []struct {
		name  string
		parse func() error
	}{
		{"xml bytes", func() error { _, err := libxml2.Parse([]byte(input)); return err }},
		{"xml string", func() error { _, err := libxml2.ParseString(input); return err }},
		{"xml huge option", func() error { _, err := libxml2.ParseString(input, parser.XMLParseHuge); return err }},
		{"xml context", func() error { _, err := parser.NewCtxt(input, 0); return err }},
		{"xml read memory", func() error { _, err := clib.XMLCtxtReadMemory(nil, input, "", "", 0); return err }},
		{"xml fragment", func() error { _, err := clib.XMLParseInNodeContext(nil, input, 0); return err }},
		{"html bytes", func() error { _, err := libxml2.ParseHTML([]byte(input)); return err }},
		{"html string", func() error { _, err := libxml2.ParseHTMLString(input); return err }},
		{"schema", func() error { _, err := xsd.Parse([]byte(input)); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.parse(), libxml2.ErrInputTooLarge)
		})
	}
	t.Run("exact limit", func(t *testing.T) {
		ctx, err := parser.NewCtxt(input[:libxml2.MaxInputBytes], 0)
		require.NoError(t, err)
		require.NoError(t, ctx.Free())
	})
	t.Run("reader exact limit", func(t *testing.T) {
		reader := io.MultiReader(strings.NewReader("<root/>"), strings.NewReader(input[:libxml2.MaxInputBytes-7]))
		doc, err := libxml2.ParseReader(reader, parser.XMLParseHuge)
		require.NoError(t, err)
		doc.Free()
	})
}

func TestReaderSizeLimit(t *testing.T) {
	for _, html := range []bool{false, true} {
		name := "xml"
		if html {
			name = "html"
		}
		t.Run(name, func(t *testing.T) {
			reader := &sizedReader{remaining: libxml2.MaxInputBytes * 2}
			var err error
			if html {
				_, err = libxml2.ParseHTMLReader(reader)
			} else {
				_, err = libxml2.ParseReader(reader)
			}
			assert.ErrorIs(t, err, libxml2.ErrInputTooLarge)
			assert.Equal(t, libxml2.MaxInputBytes-1, reader.remaining)
		})
	}
}

func TestReaderErrorsAndSmallInputs(t *testing.T) {
	readErr := errors.New("read failed")
	for _, parse := range []func(io.Reader) (types.Document, error){
		func(r io.Reader) (types.Document, error) { return libxml2.ParseReader(r) },
		func(r io.Reader) (types.Document, error) { return libxml2.ParseHTMLReader(r) },
	} {
		doc, err := parse(strings.NewReader("<root/>"))
		require.NoError(t, err)
		doc.Free()

		_, err = parse(errorReader{err: readErr})
		assert.ErrorIs(t, err, readErr)
	}
}

type sizedReader struct{ remaining int }

func (r *sizedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = ' '
	}
	r.remaining -= n
	return n, nil
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }
