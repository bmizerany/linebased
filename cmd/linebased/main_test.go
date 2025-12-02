package main

import (
	"testing"
	"testing/fstest"
)

func TestDocumentParse(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantDefs   []string
		wantErrors []string
	}{
		{
			name:     "simple define",
			text:     "define greet name\n\techo Hello, $name!\n",
			wantDefs: []string{"greet"},
		},
		{
			name:     "multiple defines",
			text:     "define foo a\n\tbar\ndefine baz x y\n\tqux\n",
			wantDefs: []string{"foo", "baz"},
		},
		{
			name:       "missing argument",
			text:       "define greet name\n\techo\ngreet\n",
			wantDefs:   []string{"greet"},
			wantErrors: []string{"greet requires 1 argument(s), got 0"},
		},
		{
			name:     "argument count ok with inline",
			text:     "define greet name\n\techo\ngreet Alice\n",
			wantDefs: []string{"greet"},
		},
		{
			name:     "argument count ok with continuation",
			text:     "define check tail\n\tverify\ncheck\n\thello\n",
			wantDefs: []string{"check"},
		},
		{
			name:       "syntax error",
			text:       " bad line\n",
			wantErrors: []string{"unexpected whitespace"},
		},
		{
			name:       "two params missing both",
			text:       "define add a b\n\tsum\nadd\n",
			wantDefs:   []string{"add"},
			wantErrors: []string{"add requires 2 argument(s), got 0"},
		},
		{
			name:       "two params missing one",
			text:       "define add a b\n\tsum\nadd 1\n",
			wantDefs:   []string{"add"},
			wantErrors: []string{"add requires 2 argument(s), got 1"},
		},
		{
			name:     "two params ok",
			text:     "define add a b\n\tsum\nadd 1 2\n",
			wantDefs: []string{"add"},
		},
		{
			name:       "used before defined",
			text:       "foo\n\ndefine foo\n\tbar\n",
			wantDefs:   []string{"foo"},
			wantErrors: []string{"template \"foo\" used before definition on line 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := newDocument("file:///test.lb", tt.text)

			// Check definitions
			var gotDefs []string
			for name := range doc.defs {
				gotDefs = append(gotDefs, name)
			}
			if len(gotDefs) != len(tt.wantDefs) {
				t.Errorf("defs: got %v, want %v", gotDefs, tt.wantDefs)
			}
			for _, want := range tt.wantDefs {
				if _, ok := doc.defs[want]; !ok {
					t.Errorf("missing def %q", want)
				}
			}

			// Check errors
			var gotErrors []string
			for _, e := range doc.errors {
				gotErrors = append(gotErrors, e.msg)
			}
			if len(gotErrors) != len(tt.wantErrors) {
				t.Errorf("errors: got %v, want %v", gotErrors, tt.wantErrors)
			}
			for i, want := range tt.wantErrors {
				if i < len(gotErrors) {
					if got := gotErrors[i]; got != want && !contains(got, want) {
						t.Errorf("error[%d]: got %q, want to contain %q", i, got, want)
					}
				}
			}
		})
	}
}

func TestSymbolAt(t *testing.T) {
	doc := newDocument("file:///test.lb", "define greet name\n\techo\ngreet Alice\n")

	tests := []struct {
		line, char int
		wantName   string
		wantOK     bool
	}{
		{0, 0, "define", true}, // on "define"
		{0, 5, "define", true}, // still on "define"
		{0, 7, "greet", true},  // on "greet" (the defined name)
		{0, 11, "greet", true}, // still on "greet"
		{0, 13, "", false},     // on "name" (parameter, not a symbol)
		{2, 0, "greet", true},  // on "greet" call
		{2, 4, "greet", true},  // still on "greet"
		{2, 6, "", false},      // on "Alice"
	}

	for _, tt := range tests {
		name, _, ok := doc.symbolAt(tt.line, tt.char)
		if ok != tt.wantOK {
			t.Errorf("symbolAt(%d, %d): ok=%v, want %v", tt.line, tt.char, ok, tt.wantOK)
		}
		if name != tt.wantName {
			t.Errorf("symbolAt(%d, %d): name=%q, want %q", tt.line, tt.char, name, tt.wantName)
		}
	}
}

func TestReferences(t *testing.T) {
	doc := newDocument("file:///test.lb", "define greet name\n\techo\ngreet Alice\ngreet Bob\n")

	refs := doc.references("greet", true)
	if len(refs) != 3 {
		t.Errorf("references(greet, true): got %d, want 3", len(refs))
	}

	refs = doc.references("greet", false)
	if len(refs) != 2 {
		t.Errorf("references(greet, false): got %d, want 2", len(refs))
	}
}

func TestSymbolAtInTemplateBody(t *testing.T) {
	// Test that symbolAt finds template calls within define bodies
	doc := newDocument("file:///test.lb", "define outer\n\tinner arg\ndefine inner x\n\techo $x\n")

	// Line 1 is "\tinner arg" - "inner" starts at char 1 (after tab)
	name, rng, ok := doc.symbolAt(1, 1)
	if !ok {
		t.Fatal("expected symbol at line 1, char 1")
	}
	if name != "inner" {
		t.Errorf("symbolAt(1, 1): got %q, want %q", name, "inner")
	}
	if rng.startChar != 1 || rng.endChar != 6 {
		t.Errorf("symbolAt(1, 1) range: got %d-%d, want 1-6", rng.startChar, rng.endChar)
	}

	// Char 0 (on tab) should not find symbol
	_, _, ok = doc.symbolAt(1, 0)
	if ok {
		t.Error("expected no symbol at line 1, char 0 (on tab)")
	}
}

func TestReferencesInTemplateBody(t *testing.T) {
	// Test that references finds calls within define bodies
	doc := newDocument("file:///test.lb", "define outer\n\tinner arg\ndefine inner x\n\techo $x\ninner foo\n")

	refs := doc.references("inner", true)
	// Should find: definition on line 2, call in body on line 1, call on line 4
	if len(refs) != 3 {
		t.Errorf("references(inner, true): got %d, want 3", len(refs))
		for _, r := range refs {
			t.Logf("  line %d, char %d-%d", r.span.startLine, r.span.startChar, r.span.endChar)
		}
	}

	refs = doc.references("inner", false)
	// Should find: call in body on line 1, call on line 4
	if len(refs) != 2 {
		t.Errorf("references(inner, false): got %d, want 2", len(refs))
	}
}

func TestReferencesAcrossIncludes(t *testing.T) {
	// Test that references finds calls in main file for templates defined in included files
	fsys := fstest.MapFS{
		"lib.linebased": &fstest.MapFile{Data: []byte("define helper\n\techo help\n")},
	}
	doc := newDocumentFS("file:///project/main.lb", "include lib\nhelper\n", fsys)

	refs := doc.references("helper", true)
	// Should find: definition in lib.linebased (line 0), call in main.lb (line 1)
	if len(refs) != 2 {
		t.Errorf("references(helper, true): got %d refs, want 2", len(refs))
		for _, r := range refs {
			t.Logf("  uri=%s line=%d", r.uri, r.span.startLine)
		}
	}

	// Verify we have refs from both files
	uris := make(map[string]bool)
	for _, r := range refs {
		uris[r.uri] = true
	}
	if !uris["file:///project/main.lb"] {
		t.Error("missing reference in main file")
	}
	if !uris["file:///project/lib.linebased"] {
		t.Error("missing reference in included file")
	}

	// Without declaration, should only find call in main file
	refs = doc.references("helper", false)
	if len(refs) != 1 {
		t.Errorf("references(helper, false): got %d refs, want 1", len(refs))
	}
	if len(refs) > 0 && refs[0].uri != "file:///project/main.lb" {
		t.Errorf("expected ref in main file, got %s", refs[0].uri)
	}
}

func TestSemanticTokens(t *testing.T) {
	doc := newDocument("file:///test.lb", "# comment\ndefine greet name\n\techo Hello, $name!\ngreet Alice\n")

	tokens := doc.semanticTokens()
	if len(tokens) == 0 {
		t.Error("semanticTokens: got empty")
	}
	// Each token is 5 uint32s
	if len(tokens)%5 != 0 {
		t.Errorf("semanticTokens: length %d not divisible by 5", len(tokens))
	}
	numTokens := len(tokens) / 5
	// Should have: comment, "define" keyword, "greet" function, "name" parameter,
	// "echo" function, "$name" variable, "greet" function call
	if numTokens < 6 {
		t.Errorf("semanticTokens: got %d tokens, want at least 6", numTokens)
	}
}

func TestSemanticTokensVariables(t *testing.T) {
	// Variables only highlighted inside template bodies
	// Outside template bodies, $foo is not highlighted
	doc := newDocument("file:///test.lb", "echo $foo ${bar} text\n")
	tokens := doc.semanticTokens()
	numTokens := len(tokens) / 5
	// Should have: echo (function) only - vars not in template body
	if numTokens != 1 {
		t.Errorf("semanticTokens outside template: got %d tokens, want 1", numTokens)
	}
}

func TestSemanticTokensDefineWithVars(t *testing.T) {
	// Test that template body expressions have function tokens for commands
	// and variable tokens for expansions
	doc := newDocument("file:///test.lb", "define foo x\n\thello ${x}. Nice to meet you $x\n")
	tokens := doc.semanticTokens()
	numTokens := len(tokens) / 5

	// Decode tokens to check types
	const (
		tokComment   = 0
		tokKeyword   = 1
		tokFunction  = 2
		tokString    = 3
		tokParameter = 4
		tokVariable  = 5
	)

	// Expected tokens:
	// line 0: define (keyword), foo (function), x (parameter)
	// line 1: hello (function - command in template body), ${x} (variable), $x (variable)
	if numTokens < 6 {
		t.Fatalf("expected at least 6 tokens, got %d", numTokens)
	}

	// Check that we have function tokens for commands and variable tokens for expansions
	funcCount := 0
	varCount := 0
	for i := 0; i < len(tokens); i += 5 {
		if tokens[i+3] == tokFunction {
			funcCount++
		}
		if tokens[i+3] == tokVariable {
			varCount++
		}
	}
	// Should have 2 function tokens: "foo" (template name) and "hello" (body command)
	if funcCount != 2 {
		t.Errorf("expected 2 function tokens, got %d", funcCount)
	}
	if varCount != 2 {
		t.Errorf("expected 2 variable tokens, got %d", varCount)
	}

	// Debug output
	t.Logf("Total tokens: %d", numTokens)
	line, char := 0, 0
	typeNames := []string{"comment", "keyword", "function", "string", "parameter", "variable"}
	for i := 0; i < len(tokens); i += 5 {
		line += int(tokens[i])
		if tokens[i] > 0 {
			char = int(tokens[i+1])
		} else {
			char += int(tokens[i+1])
		}
		typeName := "unknown"
		if int(tokens[i+3]) < len(typeNames) {
			typeName = typeNames[tokens[i+3]]
		}
		t.Logf("  line=%d char=%d len=%d type=%s", line, char, tokens[i+2], typeName)
	}
}

func TestSemanticTokensInclude(t *testing.T) {
	// include should be a keyword
	doc := newDocument("file:///test.lb", "include other.lb\n")
	tokens := doc.semanticTokens()
	if len(tokens) < 5 {
		t.Fatal("expected at least one token")
	}
	// First token type should be keyword (1)
	if tokens[3] != 1 {
		t.Errorf("include token type: got %d, want 1 (keyword)", tokens[3])
	}
}

func TestFormatComment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"# hello\n", "hello"},
		{"# hello\n# world\n", "hello\nworld"},
		{"#hello\n", "hello"},
		{"  # indented\n", "indented"},
		{"\n# after blank\n", "after blank"},
		{"# line1\n\n# line2\n", "line1\n\nline2"},
	}

	for _, tt := range tests {
		got := formatComment(tt.input)
		if got != tt.want {
			t.Errorf("formatComment(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUTF16Len(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"", 0},
		{"日本語", 3},          // 3 chars, each fits in 1 UTF-16 unit
		{"👋", 2},            // emoji needs 2 UTF-16 units (surrogate pair)
		{"hello👋world", 12}, // 5 + 2 + 5
	}

	for _, tt := range tests {
		got := utf16Len(tt.input)
		if got != tt.want {
			t.Errorf("utf16Len(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestExpandTrace(t *testing.T) {
	text := "# Greet someone\ndefine greet name\n\techo Hello, $name!\ngreet Alice\n"
	doc := newDocument("file:///test.linebased", text)

	// Get the definition
	def, ok := doc.defs["greet"]
	if !ok {
		t.Fatal("greet definition not found")
	}

	// Get the call expression
	info, ok := doc.exprAt(3) // line 3 is "greet Alice" (0-indexed)
	if !ok {
		t.Fatal("expression at line 3 not found")
	}

	trace := doc.expandTrace("greet", info.expr.Body, def)
	want := "echo Hello, Alice!\n"
	if trace != want {
		t.Errorf("expandTrace:\n got: %q\nwant: %q", trace, want)
	}
}

func TestExpandTraceMultiLine(t *testing.T) {
	// Template that expands to multiple lines
	text := "define setup\n\techo one\n\techo two\nsetup\n"
	doc := newDocument("file:///test.linebased", text)

	def, ok := doc.defs["setup"]
	if !ok {
		t.Fatal("setup definition not found")
	}

	info, ok := doc.exprAt(3) // line 3 is "setup"
	if !ok {
		t.Fatal("expression at line 3 not found")
	}

	trace := doc.expandTrace("setup", info.expr.Body, def)
	want := "echo one\necho two\n"
	if trace != want {
		t.Errorf("expandTrace multi-line:\n got: %q\nwant: %q", trace, want)
	}
}

func TestSpanToLSP(t *testing.T) {
	s := span{1, 2, 3, 4}
	got := s.toLSP()
	want := lspRange{
		Start: position{Line: 1, Character: 2},
		End:   position{Line: 3, Character: 4},
	}
	if got != want {
		t.Errorf("span.toLSP() = %v, want %v", got, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDefinitionFromInclude(t *testing.T) {
	// Test that go-to-definition finds templates defined in included files
	fsys := fstest.MapFS{
		"lib.linebased": &fstest.MapFile{
			Data: []byte("# Greets someone\ndefine greet name\n\techo Hello, $name!\n"),
		},
	}

	// Main file includes lib (without extension) and uses greet
	mainText := "include lib\ngreet Alice\n"
	doc := newDocumentFS("file:///main.lb", mainText, fsys)

	// Should find greet definition from lib.linebased
	def, ok := doc.defs["greet"]
	if !ok {
		t.Fatal("expected greet to be defined (from include)")
	}
	if def.uri != "file:///lib.linebased" {
		t.Errorf("greet definition uri: got %q, want %q", def.uri, "file:///lib.linebased")
	}
	if def.line != 1 { // 0-indexed, "define greet" is on line 2 (index 1)
		t.Errorf("greet definition line: got %d, want 1", def.line)
	}
	if def.doc != "Greets someone" {
		t.Errorf("greet definition doc: got %q, want %q", def.doc, "Greets someone")
	}
}

func TestNestedIncludes(t *testing.T) {
	// Test that nested includes are properly resolved
	fsys := fstest.MapFS{
		"a.linebased": &fstest.MapFile{
			Data: []byte("include b\n"),
		},
		"b.linebased": &fstest.MapFile{
			Data: []byte("define nested\n\techo nested!\n"),
		},
	}

	mainText := "include a\nnested\n"
	doc := newDocumentFS("file:///main.lb", mainText, fsys)

	def, ok := doc.defs["nested"]
	if !ok {
		t.Fatal("expected nested to be defined (from nested include)")
	}
	if def.uri != "file:///b.linebased" {
		t.Errorf("nested definition uri: got %q, want %q", def.uri, "file:///b.linebased")
	}
}

func TestIncludeCycleDetection(t *testing.T) {
	// Test that include cycles don't cause infinite loops
	fsys := fstest.MapFS{
		"a.linebased": &fstest.MapFile{
			Data: []byte("include b\ndefine from_a\n\techo a\n"),
		},
		"b.linebased": &fstest.MapFile{
			Data: []byte("include a\ndefine from_b\n\techo b\n"),
		},
	}

	mainText := "include a\n"
	doc := newDocumentFS("file:///main.lb", mainText, fsys)

	// Should have definitions from both files despite cycle
	if _, ok := doc.defs["from_a"]; !ok {
		t.Error("expected from_a to be defined")
	}
	if _, ok := doc.defs["from_b"]; !ok {
		t.Error("expected from_b to be defined")
	}
}

func TestLocalDefOverridesInclude(t *testing.T) {
	// Test that local definition takes precedence over included definition
	fsys := fstest.MapFS{
		"lib.linebased": &fstest.MapFile{
			Data: []byte("define greet\n\techo from lib\n"),
		},
	}

	// Local define before include
	mainText := "define greet\n\techo from main\ninclude lib\n"
	doc := newDocumentFS("file:///main.lb", mainText, fsys)

	def, ok := doc.defs["greet"]
	if !ok {
		t.Fatal("expected greet to be defined")
	}
	// First definition wins
	if def.uri != "file:///main.lb" {
		t.Errorf("greet definition uri: got %q, want %q", def.uri, "file:///main.lb")
	}
}

func TestReferencesAcrossSiblingFiles(t *testing.T) {
	// Test that references finds calls in sibling .linebased files
	// in the same directory, even without explicit include statements
	fsys := fstest.MapFS{
		"main.linebased": &fstest.MapFile{Data: []byte("define helper\n\techo help\n")},
		"other.linebased": &fstest.MapFile{Data: []byte("helper\n")},
	}
	doc := newDocumentFS("file:///main.linebased", "define helper\n\techo help\n", fsys)

	refs := doc.references("helper", true)
	// Should find: definition in main.linebased (line 0), call in other.linebased (line 0)
	if len(refs) != 2 {
		t.Errorf("references(helper, true): got %d refs, want 2", len(refs))
		for _, r := range refs {
			t.Logf("  uri=%s line=%d", r.uri, r.span.startLine)
		}
	}

	// Verify we have refs from both files
	uris := make(map[string]bool)
	for _, r := range refs {
		uris[r.uri] = true
	}
	if !uris["file:///main.linebased"] {
		t.Error("missing reference in main file")
	}
	if !uris["file:///other.linebased"] {
		t.Error("missing reference in sibling file")
	}
}

func TestRootedIncludes(t *testing.T) {
	// Test that include paths are rooted at the filesystem root.
	// Include paths must be simple names (no slashes) and the .linebased
	// extension is added automatically.
	fsys := fstest.MapFS{
		"utils.linebased": &fstest.MapFile{
			Data: []byte("include shared\ndefine util_fn\n\thelper\n"),
		},
		"shared.linebased": &fstest.MapFile{
			Data: []byte("define helper\n\techo shared\n"),
		},
	}

	// Main file includes utils (simple name, no extension)
	mainText := "include utils\nutil_fn\n"
	doc := newDocumentFS("file:///main.lb", mainText, fsys)

	// Should find util_fn from utils.linebased
	def, ok := doc.defs["util_fn"]
	if !ok {
		t.Fatal("expected util_fn to be defined")
	}
	if def.uri != "file:///utils.linebased" {
		t.Errorf("util_fn definition uri: got %q, want %q", def.uri, "file:///utils.linebased")
	}

	// Should find helper from shared.linebased (included from utils.linebased)
	def, ok = doc.defs["helper"]
	if !ok {
		t.Fatal("expected helper to be defined (from rooted include)")
	}
	if def.uri != "file:///shared.linebased" {
		t.Errorf("helper definition uri: got %q, want %q", def.uri, "file:///shared.linebased")
	}
}
