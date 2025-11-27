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
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"blake.io/linebased"
	"github.com/ericchiang/css"
	"golang.org/x/net/html"
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

// HTML checks the inner HTML of elements matching a CSS selector.
//
// It uses [Text] for comparison, supporting operators
// like ==, !=, ~, !~, contains, and !contains.
//
// An additional "count" operator compares the number of matched elements
// against the expected value.
//
// The expression body should contain: selector op want.
// For example:
//
//	div.content == Hello World
//	h1 contains Welcome
//	ul>li count 5
//
// # Selectors
//
// Selectors must not contain spaces. CSS provides several combinators
// that can be used without spaces:
//
//   - "parent>child" selects direct children (e.g., "ul>li")
//   - "a~b" selects siblings of a that are b (general sibling)
//   - "a+b" selects the immediate sibling b after a (adjacent sibling)
//   - "a,b" selects elements matching either a or b
//
// For descendant selection (which normally uses a space), use the
// direct child combinator ">" when applicable, or compose multiple checks.
//
// # No Match Behavior
//
// If no elements match the selector, it returns an error saying
// "no elements match selector {selector}" (except for count operator,
// which returns 0 and only errors if the expected count is non-zero).
//
// Returns empty string on success, error message on failure.
func HTML(expr linebased.Expanded, body string) string {
	selector, op, want := linebased.ParseArgs3(expr.Body)
	msg, ok := Text(selector, op, "_", want)
	if !ok && op != "count" {
		return msg
	}

	sel, err := css.Parse(selector)
	if err != nil {
		return fmt.Sprintf("error parsing selector %q: %v", selector, err)
	}

	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return fmt.Sprintf("error parsing HTML: %v", err)
	}

	matches := sel.Select(doc)

	if op == "count" {
		if want == "" {
			return "count operator requires non-empty want value"
		}
		got := strconv.Itoa(len(matches))
		msg, _ := Text(selector, "==", got, want)
		return msg
	}

	if len(matches) == 0 {
		return fmt.Sprintf("no elements match selector %q", selector)
	}

	got := innerHTML(matches[0])
	msg, _ = Text(selector, op, got, want)
	return msg
}

// innerHTML returns the inner HTML of a node as a string.
func innerHTML(n *html.Node) string {
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		html.Render(&buf, c)
	}
	return buf.String()
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
