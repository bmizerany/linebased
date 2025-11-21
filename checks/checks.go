// Package checks provides helpers for checking values in linebased scripts.
//
// Example usage with a JSON check:
//
//	expr := linebased.Expanded{
//		Expression: linebased.Expression{
//			Name: "json",
//			Body: "/name == \"Alice\"",
//		},
//	}
//	body := `{"name": "Alice", "age": 30}`
//	if msg := checks.JSON(expr, body); msg != "" {
//		log.Fatal(msg)
//	}
package checks

import (
	"encoding/json/jsontext"
	"errors"
	"io"
	"strings"

	"blake.io/linebased"
)

// JSON checks a JSON value at an RFC 6901 pointer path.
//
// It uses [linebased.CheckText] for comparison, supporting operators
// like ==, !=, ~, !~, contains, and !contains.
//
// The expression body should contain: path op want.
// For example:
//
//	/foo/bar == "baz"
//
// Values are compared as strings. Strings include their quotes, making it
// easy to check types using string comparison operators:
//
//	/foo ~ ^"                            # value is a string
//	/foo ~ ^\[                           # value is an array
//	/foo ~ ^\{                           # value is an object
//	/foo == true                         # boolean true
//	/foo == null                         # null
//	/foo == 42                           # integer
//	/foo == 3.14                         # float
//	/foo == []                           # empty array
//	/foo == {}                           # empty object
//
// # Undefined
//
// If a path does not exist, the value is "undefined". This is distinct from
// any valid JSON value, making it safe to test for missing keys:
//
//	/missing == undefined
//
// # Composing checks
//
// Compose checks to express complex constraints:
//
//	/foo ~ ^\[                           # is an array
//	/foo/9 != undefined                  # has at least 10 items
//	/foo/10 == undefined                 # has at most 10 items
//
// Returns empty string on success, error message on failure.
func JSON(expr linebased.Expanded, body string) string {
	path, op, want := linebased.ParseArgs3(expr.Body)
	msg, ok := linebased.CheckText(path, op, "_", want)
	if !ok {
		return msg
	}
	got, err := jsonFind(body, jsontext.Pointer(path))
	if err != nil {
		return err.Error()
	}
	msg, _ = linebased.CheckText(path, op, got, want)
	return msg
}

func jsonFind(body string, target jsontext.Pointer) (string, error) {
	dec := jsontext.NewDecoder(strings.NewReader(body))
	readValue := func() (string, error) {
		v, err := dec.ReadValue()
		return strings.TrimSpace(v.String()), err
	}

	if target == "" || target == "/" {
		return readValue()
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "undefined", nil
			}
			return "", err
		}
		if dec.StackPointer() == target {
			k, _ := dec.StackIndex(dec.StackDepth())
			switch k {
			case '{':
				return readValue()
			default:
				if tok.Kind() == '"' {
					b, err := jsontext.AppendQuote(nil, tok.String())
					return string(b), err
				}
				return tok.String(), nil
			}
		}
	}
}
