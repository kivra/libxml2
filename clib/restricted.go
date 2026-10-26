package clib

/*
#include <stdlib.h>
#include <string.h>
#include <libxml/parser.h>
#include <libxml/xmlerror.h>
#include <libxml/xmlschemas.h>

typedef struct {
	char *url;
	void *data;
	size_t size;
} MY_SchemaResource;

typedef struct {
	MY_SchemaResource *resources;
	size_t count;
	int denied;
	int failure;
	int nested;
} MY_ResourcePolicy;

static MY_ResourcePolicy *MY_newResourcePolicy(size_t count) {
	MY_ResourcePolicy *p = calloc(1, sizeof(*p));
	if (p == NULL)
		return NULL;
	if (count != 0) {
		p->resources = calloc(count, sizeof(*p->resources));
		if (p->resources == NULL) {
			free(p);
			return NULL;
		}
	}
	p->count = count;
	return p;
}

static void MY_freeResourcePolicy(MY_ResourcePolicy *p) {
	if (p == NULL)
		return;
	for (size_t i = 0; i < p->count; i++) {
		free(p->resources[i].url);
		free(p->resources[i].data);
	}
	free(p->resources);
	free(p);
}

static int MY_setSchemaResource(MY_ResourcePolicy *p, size_t i,
		const char *url, const void *data, size_t size) {
	MY_SchemaResource *r = &p->resources[i];
	r->url = strdup(url);
	r->data = malloc(size + 1);
	if (r->url == NULL || r->data == NULL)
		return -1;
	if (size != 0)
		memcpy(r->data, data, size);
	((char *)r->data)[size] = 0;
	r->size = size;
	return 0;
}

static int MY_restrictedParsingSupported(void) {
#if LIBXML_VERSION >= 21400
	return 1;
#else
	return 0;
#endif
}

#if LIBXML_VERSION >= 21400
static void MY_restrictedError(void *data, const xmlError *error) {
	if (data != NULL && error != NULL)
		*(int *)data = error->code;
}

static xmlParserErrors MY_denyResource(void *data, const char *url,
		const char *publicId, xmlResourceType type, xmlParserInputFlags flags,
		xmlParserInput **out) {
	MY_ResourcePolicy *p = data;
	*out = NULL;
	p->denied = 1;
	return XML_IO_EACCES;
}

static void MY_rejectDTD(void *data, const xmlChar *name,
		const xmlChar *externalId, const xmlChar *systemId) {
	xmlParserCtxt *ctxt = data;
	MY_ResourcePolicy *p = ctxt->_private;
	p->denied = 1;
	xmlStopParser(ctxt);
}

static xmlParserErrors MY_loadSchemaResource(void *data, const char *url,
		const char *publicId, xmlResourceType type, xmlParserInputFlags flags,
		xmlParserInput **out) {
	MY_ResourcePolicy *p = data;
	*out = NULL;
	if (type == XML_RESOURCE_MAIN_DOCUMENT && url != NULL) {
		for (size_t i = 0; i < p->count; i++) {
			MY_SchemaResource *r = &p->resources[i];
			if (strcmp(url, r->url) == 0) {
				*out = xmlNewInputFromMemory(url, r->data, r->size, 0);
				if (*out == NULL)
					p->failure = XML_ERR_NO_MEMORY;
				return *out == NULL ? XML_ERR_NO_MEMORY : XML_ERR_OK;
			}
		}
	}
	return MY_denyResource(data, url, publicId, type, flags, out);
}

static int MY_hasSchemaReferences(xmlDoc *doc) {
	xmlNode *root = xmlDocGetRootElement(doc);
	if (root == NULL)
		return 0;
	for (xmlNode *n = root->children; n != NULL; n = n->next) {
		if (n->type == XML_ELEMENT_NODE && n->ns != NULL &&
			xmlStrEqual(n->ns->href, BAD_CAST "http://www.w3.org/2001/XMLSchema") &&
			(xmlStrEqual(n->name, BAD_CAST "import") ||
			 xmlStrEqual(n->name, BAD_CAST "include") ||
			 xmlStrEqual(n->name, BAD_CAST "redefine")) &&
			xmlHasNsProp(n, BAD_CAST "schemaLocation", NULL) != NULL)
			return 1;
	}
	return 0;
}
#endif

static xmlDoc *MY_readRestricted(const void *data, size_t size, const char *url,
		MY_ResourcePolicy *policy, int *error) {
#if LIBXML_VERSION >= 21400
	xmlParserCtxt *ctxt = xmlNewParserCtxt();
	if (ctxt == NULL) {
		*error = XML_ERR_NO_MEMORY;
		return NULL;
	}
	ctxt->_private = policy;
	ctxt->sax->internalSubset = MY_rejectDTD;
	xmlCtxtSetErrorHandler(ctxt, MY_restrictedError, error);
	xmlCtxtSetResourceLoader(ctxt, MY_denyResource, policy);
	xmlCtxtSetOptions(ctxt, XML_PARSE_NO_XXE | XML_PARSE_NONET);
	// Borrow synchronous input as xmlCtxtReadMemory does, preserving native text limits.
	xmlParserInput *input = xmlNewInputFromMemory(url, data, size, XML_INPUT_BUF_STATIC);
	xmlDoc *doc = NULL;
	if (input == NULL) {
		*error = XML_ERR_NO_MEMORY;
	} else {
		doc = xmlCtxtParseDocument(ctxt, input);
		if (doc == NULL) {
			const xmlError *e = xmlCtxtGetLastError(ctxt);
			*error = e == NULL ? XML_ERR_INTERNAL_ERROR : e->code;
		}
	}
	xmlFreeParserCtxt(ctxt);
	if (policy->denied && doc != NULL) {
		xmlFreeDoc(doc);
		doc = NULL;
	}
	return doc;
#else
	return NULL;
#endif
}

static xmlSchema *MY_parseRestrictedSchema(const char *url,
		MY_ResourcePolicy *policy, int *error) {
#if LIBXML_VERSION >= 21400
	for (size_t i = 0; i < policy->count; i++) {
		MY_SchemaResource *r = &policy->resources[i];
		xmlDoc *doc = MY_readRestricted(r->data, r->size, r->url, policy, error);
		if (doc == NULL)
			return NULL;
		// Nested schema contexts in libxml2 2.15.4 do not inherit the loader.
		if (i != 0 && MY_hasSchemaReferences(doc)) {
			policy->nested = 1;
			xmlFreeDoc(doc);
			return NULL;
		}
		xmlFreeDoc(doc);
	}
	xmlSchemaParserCtxt *ctxt = xmlSchemaNewParserCtxt(url);
	if (ctxt == NULL) {
		*error = XML_ERR_NO_MEMORY;
		return NULL;
	}
	xmlSchemaSetParserStructuredErrors(ctxt, MY_restrictedError, error);
	xmlSchemaSetResourceLoader(ctxt, MY_loadSchemaResource, policy);
	xmlSchema *schema = xmlSchemaParse(ctxt);
	xmlSchemaFreeParserCtxt(ctxt);
	if ((policy->denied || policy->failure) && schema != NULL) {
		xmlSchemaFree(schema);
		schema = NULL;
	}
	return schema;
#else
	return NULL;
#endif
}
*/
import "C"

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unsafe"
)

var (
	ErrRestrictedParsingUnsupported = errors.New("restricted parsing requires libxml2 2.14 or newer")
	ErrResourceDenied               = errors.New("DTD or external resource is not allowed")
	ErrNestedSchemaReferences       = errors.New("restricted schema resources must not contain nested schema locations")
)

// RestrictedParsingSupported reports support for the per-context loader APIs.
func RestrictedParsingSupported() bool {
	return C.MY_restrictedParsingSupported() != 0
}

// XMLReadRestricted parses without DTDs or access to external resources.
func XMLReadRestricted(buf []byte) (unsafe.Pointer, error) {
	if !RestrictedParsingSupported() {
		return nil, ErrRestrictedParsingUnsupported
	}
	if len(buf) == 0 {
		return nil, errors.New("empty XML input")
	}
	policy := C.MY_newResourcePolicy(0)
	if policy == nil {
		return nil, errors.New("failed to allocate resource policy")
	}
	defer C.MY_freeResourcePolicy(policy)

	code := C.int(C.XML_ERR_INTERNAL_ERROR)
	doc := C.MY_readRestricted(unsafe.Pointer(&buf[0]), C.size_t(len(buf)), nil, policy, &code)
	if policy.denied != 0 {
		return nil, ErrResourceDenied
	}
	if doc == nil {
		return nil, fmt.Errorf("restricted XML parse failed (libxml2 error %d)", int(code))
	}
	return unsafe.Pointer(doc), nil
}

// XMLSchemaParseRestricted compiles only the root and explicitly supplied resources.
func XMLSchemaParseRestricted(buf []byte, baseURI string, resources map[string][]byte) (unsafe.Pointer, error) {
	if !RestrictedParsingSupported() {
		return nil, ErrRestrictedParsingUnsupported
	}
	if err := validateResourceURI(baseURI); err != nil {
		return nil, err
	}
	if _, exists := resources[baseURI]; exists {
		return nil, errors.New("schema resources must not replace the root URI")
	}
	keys := make([]string, 0, len(resources))
	for key := range resources {
		if err := validateResourceURI(key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	keys = append([]string{baseURI}, keys...)

	policy := C.MY_newResourcePolicy(C.size_t(len(keys)))
	if policy == nil {
		return nil, errors.New("failed to allocate schema resource policy")
	}
	defer C.MY_freeResourcePolicy(policy)
	for i, key := range keys {
		content := resources[key]
		if i == 0 {
			content = buf
		}
		curi := C.CString(key)
		var data unsafe.Pointer
		if len(content) != 0 {
			data = unsafe.Pointer(&content[0])
		}
		result := C.MY_setSchemaResource(policy, C.size_t(i), curi, data, C.size_t(len(content)))
		C.free(unsafe.Pointer(curi))
		if result != 0 {
			return nil, errors.New("failed to allocate schema resource")
		}
	}
	curi := C.CString(baseURI)
	defer C.free(unsafe.Pointer(curi))
	code := C.int(C.XML_ERR_INTERNAL_ERROR)
	schema := C.MY_parseRestrictedSchema(curi, policy, &code)
	if policy.nested != 0 {
		return nil, ErrNestedSchemaReferences
	}
	if policy.denied != 0 {
		return nil, ErrResourceDenied
	}
	if policy.failure != 0 {
		return nil, fmt.Errorf("schema resource load failed (libxml2 error %d)", int(policy.failure))
	}
	if schema == nil {
		return nil, fmt.Errorf("restricted schema parse failed (libxml2 error %d)", int(code))
	}
	return unsafe.Pointer(schema), nil
}

func validateResourceURI(value string) error {
	uri, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid schema resource URI: %w", err)
	}
	if !uri.IsAbs() || uri.Fragment != "" || strings.ContainsRune(value, '\x00') {
		return errors.New("schema resource URI must be absolute, NUL-free, and without a fragment")
	}
	return nil
}
