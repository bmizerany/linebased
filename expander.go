package linebased

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// ExpressionError reports an error that occurred while expanding or executing a
// linebased expression along with the expression that caused it.
type ExpressionError struct {
	Expanded       // The expression where the error occurred.
	Err      error // The error.
}

// Error reports the error message prefixed with the expression location.
func (e *ExpressionError) Error() string {
	return fmt.Sprintf("%s: %v", e.locationForError(), e.Err)
}

func (e *ExpressionError) Unwrap() error {
	return e.Err
}

// ExpandingDecoder reads expressions from a linebased script, expanding any
// templates defined in-line. It is analogous to [Decoder] but produces
// [Expanded] values with template expansion applied.
//
// Create an ExpandingDecoder with [NewExpandingDecoder], then call [ExpandingDecoder.Decode]
// repeatedly until it returns [io.EOF].
type ExpandingDecoder struct {
	fsys fs.FS
	defs map[string]template
	root string // prefix for file paths in error messages

	// decoderStack holds nested decoders for includes.
	// The last element is the current decoder.
	decoderStack []decoderFrame

	// pending holds expressions waiting to be returned.
	// Used when template expansion produces multiple expressions.
	pending []Expanded

	// callStack tracks template expansion for debugging and cycle detection.
	callStack stack

	// includeStack tracks included files for cycle detection.
	includeStack []string

	// err is a sticky error; once set, Decode returns it forever.
	err error
}

// decoderFrame holds a decoder and its associated file name.
type decoderFrame struct {
	dec  *Decoder
	file string
}

// NewExpandingDecoder creates an ExpandingDecoder that reads from the named file
// in fsys, expanding any templates defined in-line.
//
// Include paths are rooted at fsys. For example, if main.lb contains
// "include lib/utils.lb", the decoder opens "lib/utils.lb" from fsys directly.
// There is no relative path resolution - all includes are absolute paths
// within the filesystem.
//
// Expressions with names that do not match a template are passed through as-is.
// Invalid expansions are reported as [ExpressionError].
func NewExpandingDecoder(name string, fsys fs.FS) *ExpandingDecoder {
	d := &ExpandingDecoder{
		fsys: fsys,
		defs: make(map[string]template),
	}

	f, err := openRoot(fsys, name)
	if err != nil {
		d.err = &ExpressionError{
			Expanded: Expanded{Expression: Expression{Line: 1}, File: name},
			Err:      err,
		}
		return d
	}

	d.decoderStack = []decoderFrame{{dec: NewDecoder(f), file: name}}
	d.includeStack = []string{name}
	return d
}

// SetRoot sets a prefix for file paths in error messages. This is useful when
// the fsys is rooted at a subdirectory but you want error messages to show
// paths relative to a parent directory (e.g., the module root).
//
// For example, if fsys is rooted at "." but tests are in "pkg/testdata/",
// calling SetRoot("pkg/") will cause error messages to show "pkg/testdata/file.lb"
// instead of just "testdata/file.lb".
func (d *ExpandingDecoder) SetRoot(root string) {
	d.root = root
}

func (d *ExpandingDecoder) filePath(name string) string {
	return path.Join(d.root, name)
}

// Decode reads and returns the next expanded expression.
// It returns [io.EOF] when there are no more expressions.
// After Decode returns an error (other than io.EOF), subsequent calls
// return the same error.
func (d *ExpandingDecoder) Decode() (Expanded, error) {
	if d.err != nil {
		return Expanded{}, d.err
	}

	for {
		// Return pending expressions first (from template expansion).
		if len(d.pending) > 0 {
			expr := d.pending[0]
			d.pending = d.pending[1:]
			return expr, nil
		}

		// No current decoder means we're done.
		if len(d.decoderStack) == 0 {
			d.err = io.EOF
			return Expanded{}, io.EOF
		}

		// Read from current decoder.
		frame := &d.decoderStack[len(d.decoderStack)-1]
		rawExpr, err := frame.dec.Decode()
		if errors.Is(err, io.EOF) {
			// Pop this decoder and continue with parent.
			d.decoderStack = d.decoderStack[:len(d.decoderStack)-1]
			d.popInclude()
			continue
		}
		if err != nil {
			var synErr *SyntaxError
			if errors.As(err, &synErr) {
				d.err = &ExpressionError{
					Expanded: Expanded{Expression: Expression{Line: synErr.Line}, File: d.filePath(frame.file)},
					Err:      errors.New(synErr.Message),
				}
			} else {
				d.err = err
			}
			return Expanded{}, d.err
		}

		expr := Expanded{
			Expression: rawExpr,
			File:       d.filePath(frame.file),
		}

		// Handle the expression (may populate d.pending).
		result, err := d.expand(expr)
		if err != nil {
			d.err = err
			return Expanded{}, err
		}
		if result != nil {
			return *result, nil
		}
		// expand returned nil, meaning it handled the expression internally
		// (e.g., define or include). Loop to get the next one.
	}
}

// expand processes a single expression, handling builtins and template expansion.
// Returns the expression to yield, or nil if the expression was handled internally.
// May populate d.pending with additional expressions from template expansion.
func (d *ExpandingDecoder) expand(expr Expanded) (*Expanded, error) {
	switch expr.Name {
	case "define":
		if err := d.define(expr); err != nil {
			return nil, &ExpressionError{Expanded: expr, Err: err}
		}
		return nil, nil

	case "include":
		includePath := ParseArgs(expr.Body, 1).At(0)
		if includePath == "" {
			return nil, &ExpressionError{Expanded: expr, Err: errors.New("include: missing filename")}
		}
		if strings.Contains(includePath, "/") {
			return nil, &ExpressionError{Expanded: expr, Err: fmt.Errorf("include: path %q contains '/'; only root-level includes are allowed", includePath)}
		}
		if strings.HasSuffix(includePath, ".linebased") {
			return nil, &ExpressionError{Expanded: expr, Err: fmt.Errorf("include: path %q has .linebased extension; the extension is not required and will be added automatically", includePath)}
		}

		// Add .linebased extension
		includePath += ".linebased"

		if !d.pushInclude(includePath) {
			return nil, &ExpressionError{
				Expanded: expr,
				Err:      fmt.Errorf("include cycle detected: %s", d.includeCycle(includePath)),
			}
		}

		f, err := d.fsys.Open(includePath)
		if err != nil {
			d.popInclude()
			return nil, &ExpressionError{Expanded: expr, Err: err}
		}

		// Push new decoder onto stack.
		d.decoderStack = append(d.decoderStack, decoderFrame{dec: NewDecoder(f), file: includePath})
		return nil, nil

	case "":
		// Blank or comment-only line; pass through.
		return &expr, nil
	}

	// Check for template match.
	t, ok := d.defs[expr.Name]
	if !ok || t.body == "" {
		// No matching template or empty template; pass through.
		expr.Stack = d.callStack.framesCopy()
		return &expr, nil
	}

	// Expand template.
	return d.expandTemplate(t, expr)
}

// expandTemplate expands a template call, returning the first expression
// and queuing any additional expressions in d.pending.
func (d *ExpandingDecoder) expandTemplate(t template, callsite Expanded) (*Expanded, error) {
	// Set the stack before checking for recursion so error messages are correct.
	callsite.Stack = d.callStack.framesCopy()

	if !d.callStack.push(callsite) {
		var b strings.Builder
		b.WriteString("recursion detected in template ")
		b.WriteString(callsite.Name)
		b.WriteString(":\n")
		writeStack(&b, "    ", d.callStack.frames)
		msg := strings.TrimSuffix(b.String(), "\n")
		return nil, &ExpressionError{Expanded: callsite, Err: errors.New(msg)}
	}
	defer d.callStack.pop()

	args := callArgs(callsite.Body, len(t.params))
	if len(args) < t.required {
		if t.required == len(t.params) {
			return nil, &ExpressionError{
				Expanded: callsite,
				Err:      fmt.Errorf("template %q expects %d arguments, got %d", t.name, len(t.params), len(args)),
			}
		}
		return nil, &ExpressionError{
			Expanded: callsite,
			Err:      fmt.Errorf("template %q expects at least %d arguments, got %d", t.name, t.required, len(args)),
		}
	}
	for len(args) < len(t.params) {
		args = append(args, "")
	}

	lookup := func(name string) string {
		if i := t.params.index(name); i >= 0 {
			return args.At(i)
		}
		return ""
	}

	// Decode template body and substitute parameters.
	dec := NewDecoder(strings.NewReader(t.body))
	var expanded []Expanded

	for {
		rawExpr, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var synErr *SyntaxError
			if errors.As(err, &synErr) {
				return nil, &ExpressionError{
					Expanded: Expanded{Expression: Expression{Line: synErr.Line}, File: t.File},
					Err:      errors.New(synErr.Message),
				}
			}
			return nil, err
		}

		expr := Expanded{
			Expression: rawExpr,
			File:       t.File,
		}

		// Substitute parameters.
		var unknown string
		expr.Name, unknown = t.expand(expr.Name, lookup)
		if unknown == "" {
			expr.Body, unknown = t.expand(expr.Body, lookup)
		}
		if unknown != "" {
			return nil, &ExpressionError{
				Expanded: t.Expanded,
				Err:      fmt.Errorf("unknown parameter reference: %q", unknown),
			}
		}

		if expr.Name == "define" {
			return nil, &ExpressionError{
				Expanded: callsite,
				Err:      fmt.Errorf("expansion of %q contains illegal nested define: %q", t.name, expr.String()),
			}
		}

		// Recursively expand this expression.
		result, err := d.expand(expr)
		if err != nil {
			return nil, err
		}
		if result != nil {
			// Only set Stack if not already set by a nested expansion.
			if len(result.Stack) == 0 {
				result.Stack = d.callStack.framesCopy()
			}
			expanded = append(expanded, *result)
		}
		// Collect any pending expressions from nested template expansion
		// to maintain correct ordering.
		expanded = append(expanded, d.pending...)
		d.pending = nil
	}

	if len(expanded) == 0 {
		return nil, nil
	}

	// Queue all but the first for later.
	d.pending = append(d.pending, expanded[1:]...)
	return &expanded[0], nil
}

func (d *ExpandingDecoder) define(expr Expanded) error {
	t, err := makeTemplate(expr)
	if err != nil {
		return err
	}
	if prev, ok := d.defs[t.name]; ok {
		return fmt.Errorf("template %q redefined; previous define: %s:%d", t.name, prev.File, prev.Line)
	}
	d.defs[t.name] = t
	return nil
}

func openRoot(fsys fs.FS, name string) (fs.File, error) {
	f, err := fsys.Open(name)
	if err == nil {
		return f, nil
	}
	stem, ok := strings.CutSuffix(name, ".lb")
	if !ok {
		return nil, err
	}
	if f, err2 := fsys.Open(stem + ".linebased"); err2 == nil {
		return f, nil
	}
	return nil, err
}

func (d *ExpandingDecoder) pushInclude(name string) bool {
	if slices.Contains(d.includeStack, name) {
		return false
	}
	d.includeStack = append(d.includeStack, name)
	return true
}

func (d *ExpandingDecoder) popInclude() {
	if len(d.includeStack) == 0 {
		return
	}
	d.includeStack = d.includeStack[:len(d.includeStack)-1]
}

func (d *ExpandingDecoder) includeCycle(next string) string {
	var b strings.Builder
	for _, name := range d.includeStack {
		fmt.Fprintf(&b, "%s -> ", name)
	}
	b.WriteString(next)
	return b.String()
}

type template struct {
	Expanded

	name     string
	params   params
	required int
	body     string
}

func makeTemplate(decl Expanded) (template, error) {
	if decl.Name != "define" {
		panic(fmt.Sprintf("internal error: expected 'define', got %q", decl.Name))
	}
	head, body, _ := strings.Cut(decl.Body, "\n")
	name, rest := cutField(head)

	if name == "" {
		return template{}, fmt.Errorf("define: missing name argument")
	}

	if strings.ContainsAny(name, " \r\v\f") {
		return template{}, fmt.Errorf("define: name contains invalid characters: %q", name)
	}

	params := parseParams(rest)
	required, err := params.required()
	if err != nil {
		return template{}, err
	}
	t := template{
		Expanded: decl,
		name:     name,
		params:   params,
		required: required,
		body:     body,
	}
	return t, nil
}

func callArgs(s string, n int) Args {
	args := ParseArgs(s, n)
	for len(args) > 0 && strings.TrimSpace(args[len(args)-1]) == "" {
		args = args[:len(args)-1]
	}
	return args
}

type param string

func (p param) optional() bool {
	return strings.HasSuffix(string(p), "?")
}

type params []param

func parseParams(s string) params {
	fields := strings.Fields(s)
	ps := make(params, len(fields))
	for i, field := range fields {
		ps[i] = param(field)
	}
	return ps
}

func (p params) index(name string) int {
	return slices.Index(p, param(name))
}

func (p params) contains(name string) bool {
	return p.index(name) >= 0
}

func (p params) required() (int, error) {
	required := len(p)
	var optional param
	for i, param := range p {
		if param.optional() {
			if optional == "" {
				optional = param
				required = i
			}
			continue
		}
		if optional != "" {
			return 0, fmt.Errorf("define: required parameter %q follows optional parameter %q", param, optional)
		}
	}
	return required, nil
}

func (t template) expand(s string, lookup func(string) string) (string, string) {
	var unknown string
	out := os.Expand(t.braceOptionalRefs(s), func(name string) string {
		if !t.params.contains(name) && unknown == "" {
			unknown = name
		}
		return lookup(name)
	})
	return out, unknown
}

func (t template) braceOptionalRefs(s string) string {
	var b strings.Builder
	var changed bool
	start := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '$' || s[i+1] == '{' || !isNameStart(s[i+1]) {
			continue
		}
		j := i + 2
		for j < len(s) && isNameContinue(s[j]) {
			j++
		}
		if j >= len(s) || s[j] != '?' {
			continue
		}
		name := s[i+1 : j+1]
		if !t.params.contains(name) {
			continue
		}
		if !changed {
			b.Grow(len(s) + 2)
			changed = true
		}
		b.WriteString(s[start:i])
		b.WriteString("${")
		b.WriteString(name)
		b.WriteByte('}')
		i = j
		start = j + 1
	}
	if !changed {
		return s
	}
	b.WriteString(s[start:])
	return b.String()
}

func isNameStart(c byte) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isNameContinue(c byte) bool {
	return isNameStart(c) || '0' <= c && c <= '9'
}

// stack tracks template expansion call chains for debugging and error reporting.
type stack struct {
	frames []Expanded
	seen   map[string]bool
}

func (s *stack) push(expr Expanded) (initial bool) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.frames = append(s.frames, expr)
	initial = !s.seen[expr.Name]
	s.seen[expr.Name] = true
	return initial
}

func (s *stack) pop() Expanded {
	if len(s.frames) == 0 {
		panic("pop called on empty stack")
	}
	expr := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	delete(s.seen, expr.Name)
	return expr
}

func (s *stack) framesCopy() []Expanded {
	return slices.Clone(s.frames)
}

func writeStack(w io.Writer, prefix string, frames []Expanded) error {
	for _, expr := range frames {
		_, err := fmt.Fprint(w, prefix, expr.Where(), "> ", expr.String())
		if err != nil {
			return err
		}
	}
	return nil
}
