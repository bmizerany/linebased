package linebased

import (
	"fmt"
	"regexp"
	"strings"
)

// ParseArgs splits s into at most n whitespace-separated arguments.
// The final argument contains any remaining text after the first n-1 splits.
// Returns empty if n is zero or s is empty.
func ParseArgs(s string, n int) Args {
	if n == 0 {
		return nil
	}
	var args Args
	unlimited := n < 0
	for s != "" {
		if n == 1 && !unlimited {
			args = append(args, s)
			break
		}
		var arg string
		arg, s = cutField(s)
		args = append(args, arg)
		n--
	}
	return args
}

// ParseArgs2 splits s into two whitespace-separated arguments.
// The second argument contains any remaining text after the first split.
func ParseArgs2(s string) (a, b string) {
	args := ParseArgs(s, 2)
	return args.At(0), args.At(1)
}

// ParseArgs3 splits s into three whitespace-separated arguments.
// The third argument contains any remaining text after the first two splits.
func ParseArgs3(s string) (a, b, c string) {
	args := ParseArgs(s, 3)
	return args.At(0), args.At(1), args.At(2)
}

// Args represents a list of arguments extracted from an expression's tail.
type Args []string

// At returns the i-th argument from the Args,
// trimmed of any trailing newline,
// or the empty string if i is out of bounds.
func (a Args) At(i int) string {
	if i < 0 || i >= len(a) {
		return ""
	}
	return strings.TrimSuffix(a[i], "\n") // remove trailing newline if present
}

// CheckText compares got against want using the specified operator op
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
func CheckText(what, op, got, want string) (msg string, valid bool) {
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
