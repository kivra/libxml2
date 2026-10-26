package libxml2_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kivra/libxml2"
	"github.com/kivra/libxml2/parser"
	"github.com/kivra/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireRestrictedParsing(t *testing.T) {
	t.Helper()
	if !libxml2.RestrictedParsingSupported() {
		t.Skip("per-context resource loaders require libxml2 >= 2.14")
	}
}

func TestRestrictedUnsupportedVersionFailsClosed(t *testing.T) {
	if libxml2.RestrictedParsingSupported() {
		t.Skip("this test covers older native libraries")
	}
	_, err := libxml2.ParseRestricted([]byte("<root/>"))
	assert.ErrorIs(t, err, libxml2.ErrRestrictedParsingUnsupported)
	reader := strings.NewReader("<root/>")
	_, err = libxml2.ParseRestrictedReader(reader, 100)
	assert.ErrorIs(t, err, libxml2.ErrRestrictedParsingUnsupported)
	assert.Equal(t, 7, reader.Len(), "unsupported parsing must not consume input")
	_, err = xsd.ParseRestricted([]byte("<root/>"), "https://schemas.invalid/root.xsd", nil)
	assert.ErrorIs(t, err, libxml2.ErrRestrictedParsingUnsupported)
	doc, err := libxml2.ParseString("<root/>")
	require.NoError(t, err)
	doc.Free()
}

func TestRestrictedDocumentResources(t *testing.T) {
	requireRestrictedParsing(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "sensitive-resource")
	}))
	defer server.Close()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("sensitive-resource"), 0600))
	tests := []struct {
		name string
		xml  string
	}{
		{"external DTD", `<!DOCTYPE root SYSTEM "` + server.URL + `/root.dtd"><root/>`},
		{"external file entity", `<!DOCTYPE root [<!ENTITY secret SYSTEM "file://` + secret + `">]><root>&secret;</root>`},
		{"external HTTP entity", `<!DOCTYPE root [<!ENTITY secret SYSTEM "` + server.URL + `/secret">]><root>&secret;</root>`},
		{"parameter entity", `<!DOCTYPE root [<!ENTITY % x SYSTEM "` + server.URL + `/external.dtd">%x;]><root/>`},
		{"internal DTD", `<!DOCTYPE root [<!ENTITY x "expanded">]><root>&x;</root>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := libxml2.ParseRestricted([]byte(tc.xml))
			assert.Nil(t, doc)
			assert.ErrorIs(t, err, libxml2.ErrResourceDenied)
		})
	}
	utf16 := []byte{0xff, 0xfe}
	for _, ch := range tests[0].xml {
		utf16 = binary.LittleEndian.AppendUint16(utf16, uint16(ch))
	}
	_, err := libxml2.ParseRestricted(utf16)
	assert.ErrorIs(t, err, libxml2.ErrResourceDenied, "DTD rejection must not depend on UTF-8 scanning")

	doc, err := libxml2.ParseRestricted([]byte(`<root xmlns:xi="http://www.w3.org/2001/XInclude"><xi:include href="` + server.URL + `/include"/><xi:include href="file://` + secret + `" parse="text"/></root>`))
	require.NoError(t, err, "XInclude markup is not executed")
	assert.Contains(t, doc.String(), "xi:include")
	assert.NotContains(t, doc.String(), "sensitive-resource")
	doc.Free()
	assert.Zero(t, requests.Load())

	legacy, err := libxml2.ParseString(tests[4].xml, parser.XMLParseNoEnt)
	require.NoError(t, err, "restricted parsing must not change legacy parser behavior")
	legacy.Free()
	legacyFile, err := libxml2.ParseString(tests[1].xml, parser.XMLParseNoEnt)
	require.NoError(t, err, "the legacy file loader must remain unchanged")
	assert.Contains(t, legacyFile.String(), "sensitive-resource")
	legacyFile.Free()
}

func TestRestrictedReaderCallerLimit(t *testing.T) {
	for _, limit := range []int64{-1, 0, math.MaxInt64} {
		_, err := libxml2.ParseRestrictedReader(strings.NewReader("<root/>"), limit)
		assert.ErrorIs(t, err, libxml2.ErrInvalidInputLimit)
	}
	requireRestrictedParsing(t)
	doc, err := libxml2.ParseRestrictedReader(strings.NewReader("<root/>"), 7)
	require.NoError(t, err)
	doc.Free()
	reader := strings.NewReader("<root/> trailing bytes")
	_, err = libxml2.ParseRestrictedReader(reader, 7)
	assert.ErrorIs(t, err, libxml2.ErrInputTooLarge)
	assert.Equal(t, len("<root/> trailing bytes")-8, reader.Len())

	readErr := errors.New("read failed")
	_, err = libxml2.ParseRestrictedReader(restrictedErrorReader{err: readErr}, 100)
	assert.ErrorIs(t, err, readErr)
	_, err = libxml2.ParseRestricted([]byte("<root/> trailing"))
	assert.Error(t, err, "recovery must not be enabled")
	_, err = libxml2.ParseRestricted(nil)
	assert.Error(t, err)
	large := []byte("<root>" + strings.Repeat("x", 11<<20) + "</root>")
	_, legacyErr := libxml2.Parse(large)
	assert.Error(t, legacyErr, "legacy native text limit baseline")
	_, err = libxml2.ParseRestricted(large)
	assert.Error(t, err, "native HUGE limits must remain enabled")
}

const restrictedSchemaRoot = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
<xs:include schemaLocation="parts/types.xsd"/>
<xs:element name="root" type="Value"/>
</xs:schema>`

func TestRestrictedSchemaBundle(t *testing.T) {
	requireRestrictedParsing(t)
	const base = "https://schemas.invalid/bundle/"
	resources := map[string][]byte{
		base + "parts/types.xsd": []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:import namespace="urn:unused"/><xs:simpleType name="Value"><xs:restriction base="xs:positiveInteger"/></xs:simpleType></xs:schema>`),
	}
	schema, err := xsd.ParseRestricted([]byte(restrictedSchemaRoot), base+"root.xsd", resources)
	require.NoError(t, err)
	defer schema.Free()
	resources[base+"parts/types.xsd"] = []byte("changed after compilation")

	doc, err := libxml2.ParseRestricted([]byte("<root>42</root>"))
	require.NoError(t, err)
	defer doc.Free()
	require.NoError(t, schema.Validate(doc))
	invalid, err := libxml2.ParseRestricted([]byte("<root>-1</root>"))
	require.NoError(t, err)
	defer invalid.Free()
	assert.Error(t, schema.Validate(invalid))
}

func TestRestrictedSchemaRejectsNestedReferences(t *testing.T) {
	requireRestrictedParsing(t)
	const base = "https://schemas.invalid/bundle/"
	file := filepath.Join(t.TempDir(), "unlisted.xsd")
	require.NoError(t, os.WriteFile(file, []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:simpleType name="Value"><xs:restriction base="xs:string"/></xs:simpleType></xs:schema>`), 0600))
	for _, kind := range []string{"include", "import", "redefine"} {
		resource := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:` + kind + ` schemaLocation="file://` + file + `"/></xs:schema>`
		schema, err := xsd.ParseRestricted([]byte(restrictedSchemaRoot), base+"root.xsd", map[string][]byte{
			base + "parts/types.xsd": []byte(resource),
		})
		assert.Nil(t, schema)
		assert.ErrorIs(t, err, libxml2.ErrNestedSchemaReferences)
	}
}

func TestRestrictedSchemaMissingImportsFailClosed(t *testing.T) {
	requireRestrictedParsing(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	}))
	defer server.Close()
	for _, location := range []string{server.URL + "/missing.xsd", "file:///etc/forbidden.xsd", "../outside.xsd"} {
		body := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:import namespace="urn:unused" schemaLocation="` + location + `"/></xs:schema>`
		schema, err := xsd.ParseRestricted([]byte(body), server.URL+"/root.xsd", nil)
		assert.Nil(t, schema)
		assert.ErrorIs(t, err, libxml2.ErrResourceDenied, "missing imports must not succeed with a warning")
	}
	assert.Zero(t, requests.Load())

	schema, err := xsd.ParseRestricted([]byte(restrictedSchemaRoot), server.URL+"/root.xsd", map[string][]byte{
		server.URL + "/parts/types.xsd": []byte(`<!DOCTYPE xs:schema SYSTEM "` + server.URL + `/forbidden.dtd"><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`),
	})
	assert.Nil(t, schema)
	assert.ErrorIs(t, err, libxml2.ErrResourceDenied)
	assert.Zero(t, requests.Load())
}

func TestRestrictedSchemaDoesNotFetchInstanceHints(t *testing.T) {
	requireRestrictedParsing(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	schema, err := xsd.ParseRestricted([]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="root" type="xs:string"/></xs:schema>`), server.URL+"/trusted.xsd", nil)
	require.NoError(t, err)
	defer schema.Free()
	doc, err := libxml2.ParseRestricted([]byte(`<root xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="` + server.URL + `/untrusted.xsd">ok</root>`))
	require.NoError(t, err)
	defer doc.Free()
	require.NoError(t, schema.Validate(doc))
	assert.Zero(t, requests.Load())
}

func TestRestrictedSchemaMappedFileUsesOnlyProvidedBytes(t *testing.T) {
	requireRestrictedParsing(t)
	file := filepath.Join(t.TempDir(), "types.xsd")
	require.NoError(t, os.WriteFile(file, []byte("not an XML schema"), 0600))
	uri := "file://" + file
	root := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:include schemaLocation="` + uri + `"/><xs:element name="root" type="Value"/></xs:schema>`
	_, err := xsd.ParseRestricted([]byte(root), "https://schemas.invalid/root.xsd", nil)
	require.ErrorIs(t, err, libxml2.ErrResourceDenied)
	schema, err := xsd.ParseRestricted([]byte(root), "https://schemas.invalid/root.xsd", map[string][]byte{
		uri: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:simpleType name="Value"><xs:restriction base="xs:integer"/></xs:simpleType></xs:schema>`),
	})
	require.NoError(t, err)
	defer schema.Free()
	doc, err := libxml2.ParseRestricted([]byte("<root>42</root>"))
	require.NoError(t, err)
	defer doc.Free()
	assert.NoError(t, schema.Validate(doc), "file URI must use supplied bytes, not the file")
}

func TestRestrictedSchemaIgnoresForeignInstanceFileHints(t *testing.T) {
	requireRestrictedParsing(t)
	file := filepath.Join(t.TempDir(), "foreign.xsd")
	require.NoError(t, os.WriteFile(file, []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:foreign" elementFormDefault="qualified"><xs:element name="item" type="xs:integer"/></xs:schema>`), 0600))
	schema, err := xsd.ParseRestricted([]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="root"><xs:complexType><xs:sequence><xs:any namespace="##other" processContents="lax"/></xs:sequence></xs:complexType></xs:element></xs:schema>`), "https://schemas.invalid/root.xsd", nil)
	require.NoError(t, err)
	defer schema.Free()
	doc, err := libxml2.ParseRestricted([]byte(`<root xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:f="urn:foreign" xsi:schemaLocation="urn:foreign file://` + file + `"><f:item>not-an-integer</f:item></root>`))
	require.NoError(t, err)
	defer doc.Free()
	assert.NoError(t, schema.Validate(doc), "instance hint must not load the foreign integer schema")
}

func TestRestrictedSchemaInvalidConfiguration(t *testing.T) {
	requireRestrictedParsing(t)
	root := []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	const base = "https://schemas.invalid/root.xsd"
	for _, uri := range []string{"", "relative.xsd", base + "#fragment", base + "\x00suffix"} {
		_, err := xsd.ParseRestricted(root, uri, nil)
		assert.Error(t, err)
	}
	_, err := xsd.ParseRestricted(root, base, map[string][]byte{"relative.xsd": root})
	assert.Error(t, err)
	_, err = xsd.ParseRestricted(root, base, map[string][]byte{base: root})
	assert.Error(t, err)
	_, err = xsd.ParseRestricted(nil, base, nil)
	assert.Error(t, err)
}

func TestRestrictedSchemaPoliciesAreIndependent(t *testing.T) {
	requireRestrictedParsing(t)
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			const base = "https://schemas.invalid/"
			value := fmt.Sprintf("value-%d", i)
			body := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:include schemaLocation="type.xsd"/><xs:element name="root" type="Value"/></xs:schema>`
			types := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:simpleType name="Value"><xs:restriction base="xs:string"><xs:enumeration value="` + value + `"/></xs:restriction></xs:simpleType></xs:schema>`
			schema, err := xsd.ParseRestricted([]byte(body), base+"root.xsd", map[string][]byte{base + "type.xsd": []byte(types)})
			require.NoError(t, err)
			defer schema.Free()
			doc, err := libxml2.ParseRestricted([]byte("<root>" + value + "</root>"))
			require.NoError(t, err)
			defer doc.Free()
			assert.NoError(t, schema.Validate(doc))
		})
	}
}

type restrictedErrorReader struct{ err error }

func (r restrictedErrorReader) Read([]byte) (int, error) { return 0, r.err }
