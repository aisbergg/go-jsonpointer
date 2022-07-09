// Package jsonpointer implements IETF rfc6901.
//
// JSON Pointers are a string syntax for identifying a specific value within a
// JavaScript Object Notation (JSON) document [RFC4627]. JSON Pointer is
// intended to be easily expressed in JSON string values as well as Uniform
// Resource Identifier (URI) [RFC3986] fragment identifiers.
package jsonpointer

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Pointer represents a parsed JSON pointer
type Pointer []string

// New creates a new JSON pointer from string, *url.URL or another Pointer. If a
// string is given and that string contains an URL, it will use the URL's
// fragment as the pointer (the bit after the '#' symbol).
func New(val interface{}) (Pointer, error) {
	switch v := val.(type) {
	case Pointer:
		newPtr := make([]string, len(v))
		copy(newPtr, v)
		return newPtr, nil

	case string:
		// fast paths that skip url parse step
		if len(v) == 0 || v == "#" {
			return Pointer{}, nil
		} else if v[0] == '/' {
			return parse(v)
		}

		u, err := url.Parse(v)
		if err != nil {
			uerr := err.(*url.Error)
			return nil, wrapError(uerr.Err, ErrInvalidJSONPointer, "failed to parse URL: %s", uerr.Err)
		}
		return parse(u.Fragment)

	case *url.URL:
		return parse(v.Fragment)

	default:
		return nil, newError(ErrInvalidJSONPointer, "invalid value for pointer: %T", v)
	}
}

// String returns a string representation of the pointer.
func (p Pointer) String() (str string) {
	if len(p) == 0 {
		return ""
	}
	escapedTokens := make([]string, 0, len(p))
	for _, tok := range p {
		escapedTokens = append(escapedTokens, escapeToken(tok))
	}
	return "/" + strings.Join(escapedTokens, "/")
}

// IsEmpty indicates whether the pointer is empty.
func (p Pointer) IsEmpty() bool {
	return len(p) == 0
}

// Parent returns the parent reference of the pointer.
func (p Pointer) Parent() Pointer {
	if p.IsEmpty() {
		return Pointer{}
	}
	newPtr := make(Pointer, len(p))
	copy(newPtr[:len(p)-1], p)
	return newPtr
}

// Join joins a pointer with a string.
func (p Pointer) Join(elems ...interface{}) (Pointer, error) {
	newPtr := make([]string, len(p))
	copy(newPtr, p)
	for _, elm := range elems {
		switch e := elm.(type) {
		case Pointer:
			newPtr = append(newPtr, e...)

		case string, *url.URL:
			elmPtr, err := New(e)
			if err != nil {
				return nil, err
			}
			newPtr = append(newPtr, elmPtr...)

		default:
			return nil, fmt.Errorf("invalid value for pointer: %T", e)
		}
	}
	return newPtr, nil
}

// RelativeTo returns a pointer that is relative to the given pointer.
func (p Pointer) RelativeTo(other interface{}) (Pointer, error) {
	var otherPtr Pointer
	switch o := other.(type) {
	case Pointer:
		otherPtr = o

	case string, *url.URL:
		var err error
		otherPtr, err = New(o)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("invalid value for pointer: %T", o)
	}

	if len(otherPtr) > len(p) {
		return nil, fmt.Errorf("%s does not start with %s", p, otherPtr)
	}

	var numCmnParts int
	for i, part := range otherPtr {
		if p[i] != part {
			return p, fmt.Errorf("%s does not start with %s", p, otherPtr)
		}
		numCmnParts = i
	}

	cmnParts := p[numCmnParts+1:]
	newPtr := make([]string, len(cmnParts))
	copy(newPtr, cmnParts)
	return newPtr, nil
}

// Get returns the value from the given document that the pointer points to.
func (p Pointer) Get(doc interface{}) (interface{}, error) {
	var err error
	resultVal := reflect.ValueOf(doc)
	for _, part := range p {
		if resultVal, err = getValue(resultVal, part); err != nil {
			return nil, err
		}
	}
	if !resultVal.CanInterface() {
		return nil, newError(ErrGet, "cannot get document value")
	}
	return resultVal.Interface(), nil
}

// Set sets the value at the given pointer in the given document.
func (p Pointer) Set(doc interface{}, value interface{}) (err error) {
	// get the value in the document we want to set
	docVal := reflect.ValueOf(doc)
	for _, part := range p {
		if docVal, err = getValue(docVal, part); err != nil {
			return err
		}
	}

	// set value to pointer
	return setValue(docVal, value)
}

func setValue(doc reflect.Value, value interface{}) error {
	if doc.Kind() == reflect.Interface {
		doc = doc.Elem()
	}
	if !doc.IsValid() {
		return errors.New("cannot set value on invalid document")
	}
	if !doc.CanSet() {
		return errors.New("cannot set value on unaddressable document or unexported field")
	}

	srcVal := reflect.ValueOf(value)
	if !srcVal.IsValid() {
		return errors.New("cannot set value on invalid value")
	}
	indSrcVal := indirect(srcVal)

	switch doc.Kind() {
	// -------------------------------------------------------------------------
	// Pointer, Array, Slice, Map, Struct
	// -------------------------------------------------------------------------
	case reflect.Pointer, reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
		if doc.Type() != srcVal.Type() {
			return newError(ErrSet, "cannot set document value of type %s to value of type %s", doc.Type(), srcVal.Type())
		}
		doc.Set(srcVal)
		return nil

	// -------------------------------------------------------------------------
	// String
	// -------------------------------------------------------------------------
	case reflect.String:
		switch indSrcVal.Kind() {
		case reflect.String:
			doc.SetString(indSrcVal.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetString(strconv.FormatInt(indSrcVal.Int(), 10))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetString(strconv.FormatUint(indSrcVal.Uint(), 10))
		case reflect.Float32, reflect.Float64:
			doc.SetString(strconv.FormatFloat(indSrcVal.Float(), 'f', -1, 64))
		case reflect.Complex64, reflect.Complex128:
			doc.SetString(strconv.FormatComplex(indSrcVal.Complex(), 'f', -1, 128))
		case reflect.Bool:
			if indSrcVal.Bool() {
				doc.SetString("true")
			} else {
				doc.SetString("false")
			}
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil

	// -------------------------------------------------------------------------
	// Int, Int8, Int16, Int32, Int64
	// -------------------------------------------------------------------------
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch indSrcVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetInt(indSrcVal.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetInt(int64(indSrcVal.Uint()))
		case reflect.Float32, reflect.Float64:
			doc.SetInt(int64(indSrcVal.Float()))
		case reflect.Complex64, reflect.Complex128:
			doc.SetInt(int64(real(indSrcVal.Complex())))
		case reflect.Bool:
			if indSrcVal.Bool() {
				doc.SetInt(1)
			} else {
				doc.SetInt(0)
			}
		case reflect.String:
			i, err := strconv.ParseInt(indSrcVal.String(), 10, 64)
			if err != nil {
				return newError(ErrSet, "conversion failed (string ➜ int)")
			}
			doc.SetInt(i)
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil

	// -------------------------------------------------------------------------
	// Uint, Uint8, Uint16, Uint32, Uint64
	// -------------------------------------------------------------------------
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch indSrcVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetUint(uint64(indSrcVal.Int()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetUint(indSrcVal.Uint())
		case reflect.Float32, reflect.Float64:
			doc.SetUint(uint64(indSrcVal.Float()))
		case reflect.Complex64, reflect.Complex128:
			doc.SetUint(uint64(real(indSrcVal.Complex())))
		case reflect.Bool:
			if indSrcVal.Bool() {
				doc.SetUint(1)
			} else {
				doc.SetUint(0)
			}
		case reflect.String:
			i, err := strconv.ParseUint(indSrcVal.String(), 10, 64)
			if err != nil {
				return newError(ErrSet, "conversion failed (string ➜ uint)")
			}
			doc.SetUint(i)
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil

	// -------------------------------------------------------------------------
	// Float32, Float64
	// -------------------------------------------------------------------------
	case reflect.Float32, reflect.Float64:
		switch indSrcVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetFloat(float64(indSrcVal.Int()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetFloat(float64(indSrcVal.Uint()))
		case reflect.Float32, reflect.Float64:
			doc.SetFloat(indSrcVal.Float())
		case reflect.Complex64, reflect.Complex128:
			doc.SetFloat(float64(real(indSrcVal.Complex())))
		case reflect.Bool:
			if indSrcVal.Bool() {
				doc.SetFloat(1)
			} else {
				doc.SetFloat(0)
			}
		case reflect.String:
			f, err := strconv.ParseFloat(indSrcVal.String(), 64)
			if err != nil {
				return newError(ErrSet, "conversion failed (string ➜ float)")
			}
			doc.SetFloat(f)
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil

	// -------------------------------------------------------------------------
	// Bool
	// -------------------------------------------------------------------------
	case reflect.Bool:
		switch indSrcVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetBool(indSrcVal.Int() != 0)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetBool(indSrcVal.Uint() != 0)
		case reflect.Float32, reflect.Float64:
			doc.SetBool(indSrcVal.Float() != 0.0)
		case reflect.Complex64, reflect.Complex128:
			doc.SetBool(real(indSrcVal.Complex()) != 0.0)
		case reflect.Bool:
			doc.SetBool(indSrcVal.Bool())
		case reflect.String:
			b, err := strconv.ParseBool(indSrcVal.String())
			if err != nil {
				return newError(ErrSet, "conversion failed (string ➜ bool)")
			}
			doc.SetBool(b)
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil

	// -------------------------------------------------------------------------
	// Complex64, Complex128
	// -------------------------------------------------------------------------
	case reflect.Complex64, reflect.Complex128:
		switch indSrcVal.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			doc.SetComplex(complex(float64(indSrcVal.Int()), 0))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			doc.SetComplex(complex(float64(indSrcVal.Uint()), 0))
		case reflect.Float32, reflect.Float64:
			doc.SetComplex(complex(indSrcVal.Float(), 0))
		case reflect.Complex64, reflect.Complex128:
			doc.SetComplex(indSrcVal.Complex())
		case reflect.Bool:
			if indSrcVal.Bool() {
				doc.SetComplex(complex(1, 0))
			} else {
				doc.SetComplex(complex(0, 0))
			}
		case reflect.String:
			c, err := strconv.ParseComplex(indSrcVal.String(), 128)
			if err != nil {
				return newError(ErrSet, "conversion failed (string ➜ complex)")
			}
			doc.SetComplex(c)
		default:
			return newError(ErrSet, "type mismatch (%s ➜ %s)", indSrcVal.Kind(), doc.Kind())
		}
		return nil
	}

	return newError(ErrSet, "unsupported type (%s)", doc.Kind())
}

func indirect(val reflect.Value) reflect.Value {
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		return indirect(val.Elem())
	}
	return val
}

// getValue returns the value for the given key from the given document.
func getValue(doc reflect.Value, key string) (reflect.Value, error) {
	if !doc.IsValid() {
		return reflect.Value{}, newError(ErrGet, "document value is invalid")
	}

	switch doc.Kind() {
	// -------------------------------------------------------------------------
	// Pointer, Interface
	// -------------------------------------------------------------------------
	case reflect.Pointer, reflect.Interface:
		if doc.IsNil() {
			return reflect.Value{}, newError(ErrGet, "document value is nil")
		}
		return getValue(doc.Elem(), key)

	// -------------------------------------------------------------------------
	// Array, Slice
	// -------------------------------------------------------------------------
	case reflect.Array, reflect.Slice:
		i, err := strconv.Atoi(key)
		if err != nil {
			return reflect.Value{}, newError(ErrGet, "invalid array index: %s", key)
		}
		if i >= doc.Len() {
			return reflect.Value{}, newError(ErrGet, "index %d exceeds array length of %d", i, doc.Len())
		}
		return doc.Index(i), nil

	// -------------------------------------------------------------------------
	// Map
	// -------------------------------------------------------------------------
	case reflect.Map:
		elmVal := doc.MapIndex(reflect.ValueOf(key))
		if !elmVal.IsValid() {
			return reflect.Value{}, newError(ErrGet, "map has no key '%s'", key)
		}
		return elmVal, nil

	// -------------------------------------------------------------------------
	// Struct
	// -------------------------------------------------------------------------
	case reflect.Struct:
		// try to get value field name
		f := doc.FieldByName(key)
		if f.IsValid() {
			return f, nil
		}

		// try to get value by json tag
		st := doc.Type()
		for i := 0; i < st.NumField(); i++ {
			sf := st.Field(i)
			if jsonTag := sf.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
				var commaIdx int
				if commaIdx = strings.Index(jsonTag, ","); commaIdx < 0 {
					commaIdx = len(jsonTag)
				}
				fieldName := jsonTag[:commaIdx]
				if fieldName != "" && fieldName == key {
					f = doc.Field(i)
					return f, nil
				}
			}
		}

		return reflect.Value{}, newError(ErrGet, "struct has no field '%s'", key)

	// -------------------------------------------------------------------------
	// Primitive
	// -------------------------------------------------------------------------
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return reflect.Value{}, newError(ErrGet, "primitive value has no fields")
	}

	return reflect.Value{}, newError(ErrGet, "unsupported document type %s", doc.Kind())
}

// The ABNF syntax of a JSON Pointer is:
// json-pointer    = *( "/" reference-token )
// reference-token = *( unescaped / escaped )
// unescaped       = %x00-2E / %x30-7D / %x7F-10FFFF
//    ; %x2F ('/') and %x7E ('~') are excluded from 'unescaped'
// escaped         = "~" ( "0" / "1" )
//   ; representing '~' and '/', respectively
func parse(str string) (Pointer, error) {
	if len(str) == 0 {
		return Pointer{}, nil
	}

	if str[0] != '/' {
		return nil, newError(ErrInvalidJSONPointer, "non-empty references must begin with a '/' character")
	}
	str = str[1:]

	toks := strings.Split(str, separator)
	for i, t := range toks {
		toks[i] = unescapeToken(t)
	}
	return Pointer(toks), nil
}

const (
	separator        = "/"
	escapedSeparator = "~1"
	tilde            = "~"
	escapedTilde     = "~0"
)

func unescapeToken(tok string) string {
	tok = strings.Replace(tok, escapedSeparator, separator, -1)
	return strings.Replace(tok, escapedTilde, tilde, -1)
}

func escapeToken(tok string) string {
	tok = strings.Replace(tok, tilde, escapedTilde, -1)
	return strings.Replace(tok, separator, escapedSeparator, -1)
}
