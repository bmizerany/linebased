// Package linebased parses line-based scripts and provides template expansion.
//
// A linebased script is a sequence of expressions. Each expression has a command
// name (the first word) and a body (everything that follows). No quotes, no
// escaping. The simplicity is the point: what you write is what you get.
//
//	echo hello world
//	set path /usr/local/bin
//
// Multi-line bodies use tab-indented continuation lines:
//
//	sql query users
//		SELECT id, name
//		FROM users
//		WHERE active = true
//
// Templates let you define reusable expressions with parameters:
//
//	define greet name
//		echo Hello, $name!
//	greet Alice
//	greet Bob
//
// And includes let you compose scripts from multiple files:
//
//	include common.lb
//
// # Syntax
//
// The grammar in EBNF:
//
//	script       = { expression } .
//	expression   = { comment } ( command | blankline ) .
//	comment      = "#" text newline .
//	command      = name [ whitespace text ] newline { continuation } .
//	continuation = tab text newline .
//	blankline    = newline .
//	name         = nonwhitespace { nonwhitespace } .
//	whitespace   = " " | tab .
//	nonwhitespace = (* any character except space, tab, newline *) .
//	text         = (* any characters except newline *) .
//	tab          = "\t" .
//	newline      = "\n" .
//
// A command line begins with a name (one or more non-whitespace characters).
// Everything after the name through the newline becomes part of the body.
// Continuation lines must start with exactly one tab, which is stripped.
// Lines starting with space are syntax errors.
//
// # Comments
//
// Lines starting with # are comments. They attach to the following expression
// and are available in the Comment field of [Expanded]:
//
//	# Set the greeting message.
//	# This supports multiple lines.
//	echo Hello, World!
//
// Comments inside template bodies work the same way.
//
// # Templates
//
// Define templates with the "define" builtin. The first line names the template
// and lists parameters. Continuation lines form the template body:
//
//	define greet name
//		echo Hello, $name!
//
// Invoke templates by name. Arguments are split by whitespace, with the final
// argument consuming remaining text:
//
//	greet Alice           # echo Hello, Alice!
//	greet "Bob Smith"     # echo Hello, "Bob Smith"!   (quotes are literal)
//
// Parameter references use $name or ${name}. The braced form allows adjacent text:
//
//	define shout word
//		echo ${word}!!!
//
// Templates can invoke other templates:
//
//	define inner x
//		echo $x
//	define outer y
//		inner $y
//	outer hello           # echo hello
//
// Constraints:
//   - Recursion is forbidden.
//   - Templates cannot be redefined.
//   - Expanded templates cannot contain "define".
//
// # Includes
//
// The "include" builtin reads another file and processes it inline:
//
//	include helpers.lb
//	greet World
//
// Included files can define templates used by the including file. Include cycles
// are detected and reported as errors.
//
// # Blank Lines
//
// Blank lines produce expressions with empty names. This preserves the visual
// structure of the source:
//
//	echo first
//
//	echo second
//
// The blank line between commands appears in the expression stream.
// Like commands, blank lines can have preceding comments.
//
// # Unknown Commands
//
// Commands that are not templates pass through unchanged. This allows scripts
// to define their own command vocabulary:
//
//	define echo tail
//	echo hello           # your code interprets "echo"
//	custom arg1 arg2     # your code interprets "custom"
//
// # Error Handling
//
// Errors during parsing or expansion are reported as [ExpressionError] with
// location information:
//
//	for expr, err := range linebased.Expand("script.lb", fsys) {
//		if err != nil {
//			log.Fatal(err)  // includes file:line
//		}
//		// process expr
//	}
package linebased

import (
	"cmp"
	"fmt"
	"strings"
	"unicode"
)

// Expanded represents a parsed expression from the input stream, capturing
// both its content and context within the template expansion process.
//
// Expressions are intended to be produced by [Expressions] or [Expand], not built manually.
type Expanded struct {
	Expression

	// File returns the source filename where this expression was parsed.
	File string

	// Stack contains the call stack of the template expansions that
	// produced this expression.
	Stack []Expanded
}

// String formats the expression as parseable source text.
// Reconstructs the original syntax with normalized whitespace,
// preserving continuation line structure when the tail starts with newlines.
//
// For example:
//
//	echo hello world
//
// The Name becomes "echo" and Tail becomes "hello world\n".
// String returns "echo hello world\n".
//
//	define greet name
//		echo Hello, $name!
//
// The Name becomes "define" and Tail becomes "greet name\necho Hello, $name!\n".
// String returns "define greet name\necho Hello, $name!\n".
func (e *Expanded) String() string {
	var b strings.Builder
	b.WriteString(e.Name)

	tail := strings.TrimSuffix(e.Body, "\n")             // add back at end
	tailStartsOnNewline := strings.HasPrefix(tail, "\n") // false if e.tail was only "\n"

	if tailStartsOnNewline {
		// Preserve continuation line structure.
		b.WriteString(tail)
	} else {
		// Place a single space between name and inlined tail,
		// if tail is not empty.
		tail = strings.TrimLeftFunc(tail, unicode.IsSpace)
		if tail != "" {
			b.WriteByte(' ')
			b.WriteString(tail)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// Caller returns the immediate template call that produced the expression,
// or the zero Expression if the expression is top-level.
func (e *Expanded) Caller() Expanded {
	frames := e.Stack
	if len(frames) > 0 {
		// in an expansion - use the original call line and current template context
		return frames[len(frames)-1]
	}
	return Expanded{}
}

// Where returns a location string showing where this expression appears and executes.
// The format is filename:line: template@localline where template identifies the
// execution context and localline shows the line within that template.
//
// For example, "example.txt:42: bar@5" means the expression appears at
// line 42 of "example.txt" and executes as line 5 within template "bar".
// Top-level expressions show "main" as the template.
func (e *Expanded) Where() string {
	file := cmp.Or(e.File, "<unknown>")
	frames := e.Stack
	if len(frames) > 0 {
		// in an expansion - use the original call line and current template context
		bottom := frames[0]
		caller := frames[len(frames)-1]
		return fmt.Sprintf("%s:%d: %s@%d", file, bottom.Line, caller.Name, e.Line)
	} else {
		return fmt.Sprintf("%s:%[2]d: main@%[2]d", file, e.Line)
	}
}

// locationForError formats a callsite for error reporting.
// Top-level expressions omit the synthetic "main@line" suffix; template
// expansions retain the template context for debuggability.
func (e *Expanded) locationForError() string {
	if len(e.Stack) == 0 {
		file := cmp.Or(e.File, "<unknown>")
		if e.Line == 0 {
			return file
		}
		return fmt.Sprintf("%s:%d", file, e.Line)
	}
	return e.Where()
}

// cutField slices s around the first run of whitespace,
// returning the text before and after the run.
func cutField(s string) (string, string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	before, after := s[:i], strings.TrimLeftFunc(s[i:], unicode.IsSpace)
	return before, after
}

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
