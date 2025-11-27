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
	"fmt"
	"io"
	"regexp"
	"strings"

	"blake.io/linebased"
)

// JSON checks a JSON value at an RFC 6901 pointer path.
//
// It uses [Text] for comparison, supporting operators
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
	msg, ok := Text(path, op, "_", want)
	if !ok {
		return msg
	}
	got, err := jsonFind(body, jsontext.Pointer(path))
	if err != nil {
		return err.Error()
	}
	msg, _ = Text(path, op, got, want)
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

// Text compares got against want using the specified operator op
// and returns a failure message when the comparison does not hold.
// An empty string means the check passed.
//
// Supported operators:
//   - "==": equality
//   - "!=": inequality
//   - "~": regex match
//   - "!~": regex non-match
//   - "contains": substring presence
//   - "!contains": substring absence
//
// If valid is false, the message indicates an error in the check itself.
// If valid is true, the message indicates a failed check.
func Text(what, op, got, want string) (msg string, valid bool) {
	switch op {
	case "~", "!~":
		_, err := regexp.Compile(want)
		if err != nil {
			return fmt.Sprintf("error compiling regex %#q: %v", want, err), false
		}
	default:
		if want == "" {
			return "non-regex comparison requires non-empty want value", false
		}
	}

	switch op {
	case "==":
		if got != want {
			return fmt.Sprintf("%s = %#q, want %#q", what, got, want), true
		}
	case "!=":
		if got == want {
			return fmt.Sprintf("%s == %#q (but should not)", what, want), true
		}
	case "~":
		ok, err := regexp.MatchString(want, got)
		if err != nil {
			return fmt.Sprintf("error compiling regex %#q: %v", want, err), true
		}
		if !ok {
			return fmt.Sprintf("%s does not match %#q (but should)\t%s", what, want, indentText(got)), true
		}
	case "!~":
		ok, err := regexp.MatchString(want, got)
		if err != nil {
			return fmt.Sprintf("error compiling regex %#q: %v", want, err), true
		}
		if ok {
			return fmt.Sprintf("%s matches %#q (but should not)\t%s", what, want, indentText(got)), true
		}
	case "contains":
		if !strings.Contains(got, want) {
			return fmt.Sprintf("%s does not contain %#q (but should)\t%s", what, want, indentText(got)), true
		}
	case "!contains":
		if strings.Contains(got, want) {
			return fmt.Sprintf("%s contains %#q (but should not)\t%s", what, want, indentText(got)), true
		}
	default:
		return fmt.Sprintf("unknown operator %q", op), false
	}

	return "", true
}

// indentText formats text for inclusion in error messages.
func indentText(text string) string {
	if text == "" {
		return "(empty)"
	}
	if text == "\n" {
		return "(blank line)"
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return "(blank lines)"
	}
	text = strings.ReplaceAll(text, "\n", "\n\t")
	return text
}
