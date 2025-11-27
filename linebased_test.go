package linebased

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"unicode"

	"kr.dev/diff"
)

func FuzzExpressions(f *testing.F) {
	f.Add("foo bar baz\n")
	f.Add("foo bar baz\n# comment\n")
	f.Add("foo bar baz\n# comment\nfoo bar baz\n")
	f.Add(" foo bar baz\n# comment\nfoo bar baz\n\n\n")
	f.Fuzz(func(t *testing.T, input string) {
		linenos := make(map[int]bool)
		dec := NewDecoder(strings.NewReader(input))
		for {
			expr, err := dec.Decode()
			if err != nil {
				break
			}

			// Line invariants
			if expr.Line <= 0 {
				t.Errorf("Line number <= 0: %d", expr.Line)
			}
			if linenos[expr.Line] {
				t.Errorf("Line number %d already seen", expr.Line)
			}
			linenos[expr.Line] = true
		}
	})
}

func TestCutField(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantTail string
	}{
		{
			name:     "simple field",
			input:    "hello world",
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "field with tab",
			input:    "hello\tworld",
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "field with multiple spaces",
			input:    "hello   world",
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "single word",
			input:    "hello",
			wantName: "hello",
			wantTail: "",
		},
		{
			name:     "empty string",
			input:    "",
			wantName: "",
			wantTail: "",
		},
		{
			name:     "only spaces",
			input:    "   ",
			wantName: "",
			wantTail: "",
		},
		{
			name:     "only tabs",
			input:    "\t\t\t",
			wantName: "",
			wantTail: "",
		},
		{
			name:     "mixed whitespace only",
			input:    " \t \n ",
			wantName: "",
			wantTail: "",
		},
		{
			name:     "leading spaces",
			input:    "   hello world",
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "leading tabs",
			input:    "\t\thello world",
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "trailing spaces",
			input:    "hello world   ",
			wantName: "hello",
			wantTail: "world   ",
		},
		{
			name:     "command with only spaces after",
			input:    "cmd   ",
			wantName: "cmd",
			wantTail: "",
		},
		{
			name:     "newline in tail",
			input:    "hello world\nmore",
			wantName: "hello",
			wantTail: "world\nmore",
		},
		{
			name:     "preserves newlines in tail",
			input:    "cmd arg1\narg2\n",
			wantName: "cmd",
			wantTail: "arg1\narg2\n",
		},
		{
			name:     "unicode whitespace",
			input:    "hello\u00A0world", // non-breaking space
			wantName: "hello",
			wantTail: "world",
		},
		{
			name:     "complex mixed whitespace",
			input:    " \t hello \t world \n more ",
			wantName: "hello",
			wantTail: "world \n more ",
		},
		{
			name:     "potential infinite loop case - whitespace with non-ascii",
			input:    "\u2000\u2001\u2002", // various unicode spaces
			wantName: "",
			wantTail: "",
		},
		{
			name:     "zero-width spaces",
			input:    "\u200B\u200C\u200D", // zero-width spaces
			wantName: "\u200B\u200C\u200D", // these should not be treated as whitespace by unicode.IsSpace
			wantTail: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotTail := cutField(tt.input)
			if gotName != tt.wantName {
				t.Errorf("cutField(%q) name = %q, want %q", tt.input, gotName, tt.wantName)
			}
			if gotTail != tt.wantTail {
				t.Errorf("cutField(%q) tail = %q, want %q", tt.input, gotTail, tt.wantTail)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		n    int // name of the expression
		s    string
		want Args
	}{
		{-1, "", nil}, // no args
		{0, "", nil},  // no args
		{1, "", nil},  // no args
		{-1, "arg1 arg2", []string{"arg1", "arg2"}},
		{0, "arg1 arg2", nil},
		{1, "arg1 arg2", []string{"arg1 arg2"}},
		{2, "arg1 arg2", []string{"arg1", "arg2"}},
		{3, "arg1 arg2", []string{"arg1", "arg2"}},
		{1, "arg1\targ2", []string{"arg1\targ2"}},
		{1, "arg1 arg2\t\t\t", []string{"arg1 arg2\t\t\t"}},
		{2, "arg1 arg2\t\t\t", []string{"arg1", "arg2\t\t\t"}},
		{3, "arg1 arg2\t\t\t", []string{"arg1", "arg2"}},
		{2, "arg1\n\targ2", []string{"arg1", "arg2"}},
	}
	for _, tt := range cases {
		got := ParseArgs(tt.s, tt.n)
		if !slices.Equal(got, tt.want) {
			t.Errorf("Expression.Args(%d, %q) = %#v, want %#v", tt.n, tt.s, got, tt.want)
		}
	}
}

func TestExpressionString(t *testing.T) {
	tests := []struct {
		head string
		tail string
		want string
	}{
		{"cmd", "arg1 arg2\n", "cmd arg1 arg2\n"},
		{"cmd", "arg1\targ2\n", "cmd arg1\targ2\n"},
		{"cmd", "\n\targ1 arg2\n", "cmd\n\targ1 arg2\n"},
		{"", "arg1 arg2\n", " arg1 arg2\n"}, // empty command name
		{"cmd", "", "cmd\n"},                // add newline
		{"cmd", "arg1", "cmd arg1\n"},       // add newline
		{"cmd", "\narg1", "cmd\narg1\n"},    // trailing spaces preserved

		{"cmd", "\targ1", "cmd arg1\n"}, // join with single whitespace
		{"greet", "\n\tAlice", "greet\n\tAlice\n"},
	}

	for _, tt := range tests {
		got := (&Expanded{Expression: Expression{Name: tt.head, Body: tt.tail}}).String()
		if got != tt.want {
			t.Errorf("Expression.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestExpressionsCommentCapture(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "command with comments",
			input: "" +
				"# first\n" +
				"# second\n" +
				"cmd tail\n",
			want: []string{
				`line=3 name="cmd" comment="# first\n# second\n"`,
			},
		},
		{
			name: "blank line with comments",
			input: "" +
				"# heading\n" +
				"\n",
			want: []string{
				`line=2 name="" comment="# heading\n"`,
			},
		},
		{
			name: "multiple expressions",
			input: "" +
				"# first\n" +
				"one tail\n" +
				"two tail\n" +
				"# third\n" +
				"# comment\n" +
				"three tail\n",
			want: []string{
				`line=2 name="one" comment="# first\n"`,
				`line=3 name="two" comment=""`,
				`line=6 name="three" comment="# third\n# comment\n"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			dec := NewDecoder(strings.NewReader(tt.input))
			for {
				expr, err := dec.Decode()
				if err != nil {
					break
				}
				if expr.Line == 0 {
					continue
				}
				got = append(got, fmt.Sprintf("line=%d name=%q comment=%q", expr.Line, expr.Name, expr.Comment))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("unexpected expressions:\n got: %v\nwant: %v", got, tt.want)
			}
		})
	}
}

func ExampleExpand() {
	const script = "" +
		"define echo tail\n" +
		"define greet name\n" +
		"\techo Hello, $name!\n" +
		"\n" +
		"greet Alice\n" +
		"greet Bob\n"

	f := fstest.MapFS{
		"example.lb": &fstest.MapFile{Data: []byte(script)},
	}

	d := NewExpandingDecoder("example.lb", f)
	for {
		expr, err := d.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println(err)
			return
		}
		if expr.Name == "" {
			continue // skip blank lines
		}
		fmt.Printf("%s", expr.String())
	}

	// Output:
	// echo Hello, Alice!
	// echo Hello, Bob!
}

// ExampleExpand_include shows how includes let you share templates
// across scripts. The main script includes a library file that defines
// reusable templates, then uses those templates.
func ExampleExpand_include() {
	fsys := fstest.MapFS{
		"main.lb": &fstest.MapFile{Data: []byte("" +
			"include greetings.lb\n" +
			"hello Alice\n" +
			"goodbye Bob\n",
		)},
		"greetings.lb": &fstest.MapFile{Data: []byte("" +
			"define say tail\n" +
			"define hello name\n" +
			"\tsay Hello, $name!\n" +
			"define goodbye name\n" +
			"\tsay Goodbye, $name!\n",
		)},
	}

	d := NewExpandingDecoder("main.lb", fsys)
	for {
		expr, err := d.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println(err)
			return
		}
		if expr.Name == "" {
			continue
		}
		fmt.Printf("%s", expr.String())
	}

	// Output:
	// say Hello, Alice!
	// say Goodbye, Bob!
}

func TestExpandInclude(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		fsys := fstest.MapFS{
			"main.lb":  &fstest.MapFile{Data: []byte("include other.lb\n")},
			"other.lb": &fstest.MapFile{Data: []byte("define echo tail\necho hi\n")},
		}

		var got []string
		d := NewExpandingDecoder("main.lb", fsys)
		for {
			expr, err := d.Decode()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Expanded error: %v", err)
			}
			if expr.Name == "" {
				continue
			}
			got = append(got, expr.String())
		}

		want := []string{"echo hi\n"}
		if !slices.Equal(got, want) {
			t.Fatalf("include output: got %v, want %v", got, want)
		}
	})

	t.Run("missing", func(t *testing.T) {
		fsys := fstest.MapFS{
			"main.lb": &fstest.MapFile{Data: []byte("include missing.lb\n")},
		}
		var gotErr error
		d := NewExpandingDecoder("main.lb", fsys)
		for {
			_, err := d.Decode()
			if err == io.EOF {
				break
			}
			if err != nil {
				gotErr = err
				break
			}
		}

		if gotErr == nil {
			t.Fatalf("expected error for missing include, got nil")
		}
		var pe *fs.PathError
		if !errors.As(gotErr, &pe) {
			t.Fatalf("expected PathError, got %T: %v", gotErr, gotErr)
		}
		if pe.Path != "missing.lb" {
			t.Fatalf("unexpected PathError path: %q", pe.Path)
		}
	})

	t.Run("cycle", func(t *testing.T) {
		fsys := fstest.MapFS{
			"main.lb":  &fstest.MapFile{Data: []byte("include other.lb\n")},
			"other.lb": &fstest.MapFile{Data: []byte("include main.lb\n")},
		}
		var gotErr error
		d := NewExpandingDecoder("main.lb", fsys)
		for {
			_, err := d.Decode()
			if err == io.EOF {
				break
			}
			if err != nil {
				gotErr = err
				break
			}
		}

		if gotErr == nil {
			t.Fatalf("expected cycle error, got nil")
		}
		want := "other.lb:1: include cycle detected: main.lb -> other.lb -> main.lb"
		if gotErr.Error() != want {
			t.Fatalf("unexpected cycle error:\n got %q\nwant %q", gotErr.Error(), want)
		}
	})
}

var useAbsPaths = sync.OnceValue(func() bool {
	f := flag.Lookup("test.fullpath")
	return f != nil && f.Value.String() == "true"
})

//go:embed testdata/*.lb
var scripts embed.FS

func TestExpandingDecoder(t *testing.T) {
	files, err := fs.Glob(scripts, "testdata/*.lb")
	if err != nil {
		t.Fatalf("glob testdata/*.txt: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("no testdata files found")
	}

	includes := func() (includes fstest.MapFS) {
		files, err := fs.Glob(scripts, "testdata/_*.lb")
		if err != nil {
			t.Fatalf("glob testdata/_*.lb: %v", err)
		}
		includes = make(fstest.MapFS)
		for _, file := range files {
			data, err := fs.ReadFile(scripts, file)
			if err != nil {
				t.Fatalf("reading include %s: %v", file, err)
			}
			includes[filepath.Base(file)] = &fstest.MapFile{Data: data}
		}
		return includes
	}()

	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), "_") {
			continue // skip files starting with _
		}
		t.Run(filepath.Base(file), func(t *testing.T) {
			d := NewExpandingDecoder(file, scripts)

			next := func() Expanded {
				for {
					t.Helper()
					cmd, err := d.Decode()
					if err == io.EOF {
						return Expanded{}
					}
					if err != nil {
						fmt.Fprint(t.Output(), err)
						t.FailNow()
					}
					if cmd.Line > 0 && cmd.Name == "" {
						continue // skip empty lines
					}
					if cmd.Name == "define" {
						continue // template helpers at top level
					}
					return cmd
				}
			}

			var checks int
			for {
				// record
				cmd := next()
				if cmd.Line == 0 {
					break // end of input
				}
				if cmd.Name != "record" && cmd.Name != "record!" {
					t.Fatalf("\n%s: expected 'record' or 'record!', got %q", cmd.Where(), cmd.Name)
				}

				var record strings.Builder
				name, tail, _ := strings.Cut(cmd.Body, "\n")
				name = strings.ReplaceAll(name, " ", "_")
				if name == "" {
					name = "record"
				}

				// Make fake fs with just this one file and any includes
				fsys := fstest.MapFS{
					name: &fstest.MapFile{Data: []byte(tail)},
				}
				maps.Copy(fsys, includes)

				d := NewExpandingDecoder(name, fsys)
				for {
					expr, err := d.Decode()
					if err == io.EOF {
						break
					}
					if err != nil {
						fmt.Fprintf(&record, "%v\n", err)
						break
					}
					if cmd.Name == "record!" {
						writeStack(&record, "", []Expanded{expr})
					} else {
						record.WriteString(expr.String())
					}
				}

				// check
				rec, cmd := cmd, next()
				if cmd.Name != "check" {
					if cmd.Name == "" {
						t.Fatalf("%s: record without check", rec.Where())
					} else {
						t.Fatalf("\n%s: expected 'check', got %q", cmd.Where(), cmd.Name)
					}
				}

				checks++

				got := strings.TrimLeftFunc(record.String(), unicode.IsSpace)
				want := strings.TrimLeftFunc(cmd.Body, unicode.IsSpace)
				var wroteHeader bool
				errorf := func(format string, args ...any) {
					if !wroteHeader {
						if useAbsPaths() {
							cmd.File, _ = filepath.Abs(file)
						}
						format = fmt.Sprintf("\n%s:\n%s", cmd.Where(), format)
						wroteHeader = true
					}
					fmt.Fprintf(t.Output(), format, args...)
					t.Fail()
				}
				diff.Test(t, errorf, got, want)
			}

			if checks == 0 {
				t.Fatalf("no record/check pairs found in %s", file)
			}
		})
	}
}
