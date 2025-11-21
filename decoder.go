package linebased

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SyntaxError represents a syntax error in the input.
type SyntaxError struct {
	Line    int    // line number (1-indexed)
	Message string // error message without line prefix
	Err     error  // underlying error, if any
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%d: %s", e.Line, e.Message)
}

func (e *SyntaxError) Unwrap() error {
	return e.Err
}

// Expression represents a line-based expression consisting of an optional command
// with continuation lines, preceded by zero or more comment lines.
type Expression struct {
	// Line is the line number where the expression body starts (1-indexed).
	// This is the line number of the command or blank line, not the preceding comments.
	Line int

	// Comment contains any leading comment lines (including the leading '#')
	// and blank lines that immediately preceded this expression.
	// Each line is terminated with a newline when present.
	Comment string

	// Name is the command name of the expression,
	// which is the first word of the command line.
	Name string

	// Body is everything in the expression after the command name,
	// including continuation lines, each without their leading tab.
	Body string
}

// ParseArgs splits the tail into at most n whitespace-separated arguments.
// The final argument contains any remaining text after the first n-1 splits.
// Returns empty if n is zero or the tail is empty.
func (e Expression) ParseArgs(n int) Args {
	return ParseArgs(e.Body, n)
}

// Decoder reads line-based expressions from an input stream.
type Decoder struct {
	r    *bufio.Reader
	line int // current line number (1-indexed)
}

// NewDecoder creates a new Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Decode reads the next Expression from the input and returns it.
// It returns io.EOF when there are no more expressions to read.
// It returns an error if the input is malformed.
func (d *Decoder) Decode() (Expression, error) {
	var comments strings.Builder
	var body strings.Builder

	for {
		line, err := d.readLine()
		if err != nil && line == "" {
			// We have read all input, if any.
			// Handle any accumulated comments/body,
			// or return the error (usually io.EOF).
			if comments.Len()+body.Len() > 0 {
				// Return what we have so far;
				// The next call to Decode will return the sticky err.
				c := comments.String()
				exprLine := d.line
				if !strings.HasSuffix(c, "\n") {
					// Content without trailing newline stays on its line.
					exprLine--
				}
				return makeExpr(exprLine, c, body.String()), nil
			}
			return Expression{}, err
		}

		switch line[0] {
		case '\n':
			return makeExpr(d.line, comments.String(), line), nil
		case '#':
			comments.WriteString(line)
			if err != nil && !errors.Is(err, io.EOF) {
				return Expression{}, err
			}
		case ' ', '\t':
			return Expression{}, &SyntaxError{
				Line:    d.line,
				Message: "unexpected whitespace at start of line",
			}
		default:
			body.WriteString(line)
			startingLine := d.line

			// Read continuation lines, if any.
			for {
				b, err := d.peek()
				if err != nil && !errors.Is(err, io.EOF) {
					return Expression{}, err
				}
				if b != '\t' {
					break
				}
				line, err := d.readLine()
				if err != nil && !errors.Is(err, io.EOF) {
					return Expression{}, err
				}
				body.WriteString(line[1:]) // strip leading tab
				if errors.Is(err, io.EOF) {
					break
				}
			}

			return makeExpr(startingLine, comments.String(), body.String()), nil
		}
	}
}

// makeExpr constructs an Expression from raw body text, extracting name and tail.
func makeExpr(line int, comment, body string) Expression {
	name, tail := parseBody(body)
	return Expression{
		Line:    line,
		Comment: comment,
		Name:    name,
		Body:    tail,
	}
}

// parseBody extracts the command name and tail from a body string.
func parseBody(body string) (name, tail string) {
	i := strings.IndexAny(body, " \t\n")
	if i < 0 {
		return strings.TrimSpace(body), ""
	}
	name = strings.TrimSpace(body[:i])
	tail = strings.TrimLeft(body[i:], " \t")
	return name, tail
}

// peek returns the next byte without consuming it.
func (d *Decoder) peek() (byte, error) {
	b, err := d.r.Peek(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// readLine reads the next line from the input, updating the line counter.
func (d *Decoder) readLine() (string, error) {
	d.line++
	return d.r.ReadString('\n')
}
