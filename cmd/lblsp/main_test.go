package main

import (
	"testing"
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
	// Test that template body is string, expansions are variable
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
	// line 1: body (string), ${x} (variable), $x (variable)
	if numTokens < 6 {
		t.Fatalf("expected at least 6 tokens, got %d", numTokens)
	}

	// Check that we have 1 string token (template body) and 2 variable tokens
	stringCount := 0
	varCount := 0
	for i := 0; i < len(tokens); i += 5 {
		if tokens[i+3] == tokString {
			stringCount++
		}
		if tokens[i+3] == tokVariable {
			varCount++
		}
	}
	if stringCount != 1 {
		t.Errorf("expected 1 string token (body), got %d", stringCount)
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
