package jsonpointer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func Example() {
	var document = []byte(`{
		"foo": {
			"bar": {
				"baz": [0,"hello!"]
			}
		}
	}`)

	// unmarshal our document into generic go structs
	parsed := map[string]interface{}{}
	// be sure to handle errors in real-world code!
	json.Unmarshal(document, &parsed)

	// parse a json pointer. Pointers can also be url fragments
	// the following are equivelent pointers:
	// "/foo/bar/baz/1"
	// "#/foo/bar/baz/1"
	// "http://example.com/document.json#/foo/bar/baz/1"
	ptr, _ := New("/foo/bar/baz/1")

	// evaluate the pointer against the document
	// evaluation always starts at the root of the document
	got, _ := ptr.Get(parsed)

	fmt.Println(got)
	// Output: hello!
}

// doc pulled from spec:
var docBytes = []byte(`{
	"foo": ["bar", "baz"],
	"": 0,
	"a/b": 1,
	"c%d": 2,
	"e^f": 3,
	"g|h": 4,
	"i\\j": 5,
	"k\"l": 6,
	" ": 7,
	"m~n": 8
}`)

func TestParse(t *testing.T) {
	cases := []struct {
		raw    string
		parsed string
		err    string
	}{
		{"#/", "/", ""},
		{"#/foo", "/foo", ""},
		{"#/foo/", "/foo/", ""},

		{"://", "", "invalid pointer: failed to parse URL: missing protocol scheme"},
		{"#7", "", "invalid pointer: non-empty references must begin with a '/' character"},
		{"", "", ""},
		{"https://example.com#", "", ""},
	}

	for _, c := range cases {
		got, err := New(c.raw)
		if assertError(t, c.raw, err, c.err) {
			continue
		}

		if c.err == "" && got.String() != c.parsed {
			t.Errorf("%s: string output mismatch: expected: '%s', got: '%s'", c.raw, c.parsed, got.String())
			continue
		}
	}
}

func TestEval(t *testing.T) {
	doc := map[string]interface{}{}
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		t.Errorf("error unmarshaling document json: %s", err.Error())
		return
	}

	cases := []struct {
		ptrstring string
		expect    interface{}
		err       string
	}{
		// "raw" references
		{"", doc, ""},
		{"/foo", doc["foo"], ""},
		{"/foo/0", "bar", ""},
		{"/", float64(0), ""},
		{"/a~1b", float64(1), ""},
		{"/c%d", float64(2), ""},
		{"/e^f", float64(3), ""},
		{"/g|h", float64(4), ""},
		{"/i\\j", float64(5), ""},
		{"/k\"l", float64(6), ""},
		{"/ ", float64(7), ""},
		{"/m~0n", float64(8), ""},

		// url fragment references
		{"#", doc, ""},
		{"#/foo", doc["foo"], ""},
		{"#/foo/0", "bar", ""},
		{"#/", float64(0), ""},
		{"#/a~1b", float64(1), ""},
		{"#/c%25d", float64(2), ""},
		{"#/e%5Ef", float64(3), ""},
		{"#/g%7Ch", float64(4), ""},
		{"#/i%5Cj", float64(5), ""},
		{"#/k%22l", float64(6), ""},
		{"#/%20", float64(7), ""},
		{"#/m~0n", float64(8), ""},

		{"https://example.com#/m~0n", float64(8), ""},

		// bad references
		{"/foo/bar", nil, "get: invalid array index: bar"},
		{"/foo/3", nil, "get: index 3 exceeds array length of 2"},
		{"/bar/baz", nil, "get: map has no key 'bar'"},
	}

	for _, c := range cases {
		ptr, err := New(c.ptrstring)
		if err != nil {
			t.Errorf("%s: expected no error, got: %s", c.ptrstring, err.Error())
			continue
		}

		got, err := ptr.Get(doc)
		if assertError(t, c.ptrstring, err, c.err) {
			continue
		}

		if !reflect.DeepEqual(got, c.expect) {
			t.Errorf("%s: value mismatch, expected: %#v, got: %#v", c.ptrstring, c.expect, got)
		}
	}
}

func assertError(t *testing.T, key string, err error, expected string) (_break bool) {
	if err != nil {
		if expected != "" {
			if expected != err.Error() {
				t.Errorf("%s: expected error message: `%s`, got: `%s`", key, expected, err.Error())
			}
		} else {
			t.Errorf("%s: expected no error, got: %s", key, err.Error())
		}
		return true

	} else if expected != "" {
		t.Errorf("%s: expected error with message: %s", key, expected)
	}

	return false
}

func TestJoin(t *testing.T) {
	cases := []struct {
		parent string
		path   string
		parsed string
		err    string
	}{
		{"", "/0", "/0", ""},
		{"#/", "/0", "//0", ""},
		{"/0", "/0", "/0/0", ""},
		{"/foo", "/0", "/foo/0", ""},
		{"/foo", "/0", "/foo/0", ""},
		{"/foo/0", "/0", "/foo/0/0", ""},
	}

	for _, c := range cases {
		p, err := New(c.parent)
		if err != nil {
			t.Errorf("%s: expected no error, got: %s", c.parent, err.Error())
			continue
		}

		desc, err := p.Join(c.path)
		if assertError(t, c.parent, err, c.err) {
			continue
		}

		if desc.String() != c.parsed {
			t.Errorf("%s: expected: %s, got: %s", c.parent, c.parsed, desc.String())
			continue
		}
	}
}

func BenchmarkEval(b *testing.B) {
	document := []byte(`{
		"foo": {
		"bar": {
			"baz": [0,"hello!"]
		}
		}
	}`)

	parsed := map[string]interface{}{}
	json.Unmarshal(document, &parsed)
	ptr, _ := New("/foo/bar/baz/1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ptr.Get(parsed); err != nil {
			b.Errorf("error evaluating: %s", err.Error())
			continue
		}

	}
}

func TestEscapeToken(t *testing.T) {
	cases := []struct {
		input  string
		output string
	}{
		{"/abc~1/~/0/~0/", "/abc~1/~/0/~0/"},
	}
	for i, c := range cases {
		got := unescapeToken(escapeToken(c.input))
		if got != c.output {
			t.Errorf("case %d result mismatch.  expected: '%s', got: '%s'", i, c.output, got)
		}
	}
}
