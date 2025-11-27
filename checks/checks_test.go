package checks_test

import (
	"testing"

	"blake.io/linebased"
	"blake.io/linebased/checks"
)

func TestJSON(t *testing.T) {
	body := `{"foo": {"bar": "baz"}, "num": 42, "arr": [1, 2, 3], "null": null}`

	tests := []struct {
		expr    string
		wantMsg bool
	}{
		{`/foo/bar == "baz"`, false},
		{`/foo/bar != "qux"`, false},
		{`/foo/bar == "wrong"`, true},
		{`/num == 42`, false},
		{`/num == 99`, true},
		{`/arr/0 == 1`, false},
		{`/arr == [1, 2, 3]`, false},
		{`/missing == undefined`, false},
		{`/null == null`, false},
		{`/foo/bar ~ ^"baz"$`, false},
		{`/foo/bar contains baz`, false},
	}

	for _, tt := range tests {
		expr := linebased.Expanded{Expression: linebased.Expression{Name: "json", Body: tt.expr}}
		msg := checks.JSON(expr, body)
		if tt.wantMsg && msg == "" {
			t.Errorf("JSON(%q): expected error message, got none", tt.expr)
		}
		if !tt.wantMsg && msg != "" {
			t.Errorf("JSON(%q): unexpected error: %s", tt.expr, msg)
		}
	}
}

func TestJSONMalformed(t *testing.T) {
	expr := linebased.Expanded{Expression: linebased.Expression{Name: "json", Body: `/foo == "bar"`}}
	msg := checks.JSON(expr, `{invalid`)
	if msg == "" {
		t.Error("expected error for malformed JSON")
	}
}

func TestHTML(t *testing.T) {
	body := `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
	<h1 class="title">Welcome</h1>
	<div id="content">Hello World</div>
	<ul>
		<li>Item 1</li>
		<li>Item 2</li>
		<li>Item 3</li>
	</ul>
	<p class="empty"></p>
</body>
</html>`

	tests := []struct {
		expr    string
		wantMsg bool
	}{
		// Equality operator
		{`h1.title == Welcome`, false},
		{`h1.title == Wrong`, true},

		// Inequality operator
		{`h1.title != Wrong`, false},
		{`h1.title != Welcome`, true},

		// Regex match operator
		{`h1.title ~ ^Wel`, false},
		{`h1.title ~ ^Wrong`, true},

		// Regex non-match operator
		{`h1.title !~ ^Wrong`, false},
		{`h1.title !~ ^Wel`, true},

		// Contains operator
		{`#content contains World`, false},
		{`#content contains Missing`, true},

		// Not contains operator
		{`#content !contains Missing`, false},
		{`#content !contains World`, true},

		// Count operator
		{`li count 3`, false},
		{`li count 5`, true},
		{`.nonexistent count 0`, false},
		{`.nonexistent count 1`, true},

		// No elements match selector (non-count operators)
		{`.nonexistent == anything`, true},

		// ID selector
		{`#content == Hello World`, false},

		// Class selector
		{`.title == Welcome`, false},

		// Child selector (no spaces in selector)
		{`ul>li count 3`, false},

		// Empty content
		{`p.empty == `, true}, // empty want requires non-regex op error
	}

	for _, tt := range tests {
		expr := linebased.Expanded{Expression: linebased.Expression{Name: "html", Body: tt.expr}}
		msg := checks.HTML(expr, body)
		if tt.wantMsg && msg == "" {
			t.Errorf("HTML(%q): expected error message, got none", tt.expr)
		}
		if !tt.wantMsg && msg != "" {
			t.Errorf("HTML(%q): unexpected error: %s", tt.expr, msg)
		}
	}
}

func TestHTMLNoElementsMatch(t *testing.T) {
	body := `<div>content</div>`

	// Non-count operators should return "no elements match selector" error
	expr := linebased.Expanded{Expression: linebased.Expression{Name: "html", Body: `.missing == anything`}}
	msg := checks.HTML(expr, body)
	want := `no elements match selector ".missing"`
	if msg != want {
		t.Errorf("HTML(.missing == anything): got %q, want %q", msg, want)
	}

	// Count operator should return 0 and not error for zero count
	expr = linebased.Expanded{Expression: linebased.Expression{Name: "html", Body: `.missing count 0`}}
	msg = checks.HTML(expr, body)
	if msg != "" {
		t.Errorf("HTML(.missing count 0): unexpected error: %s", msg)
	}
}

func TestHTMLInvalidSelector(t *testing.T) {
	body := `<div>content</div>`
	expr := linebased.Expanded{Expression: linebased.Expression{Name: "html", Body: `[invalid == test`}}
	msg := checks.HTML(expr, body)
	if msg == "" {
		t.Error("expected error for invalid CSS selector")
	}
}

func TestHTMLCountFailureMessage(t *testing.T) {
	body := `<ul><li>1</li><li>2</li></ul>`
	expr := linebased.Expanded{Expression: linebased.Expression{Name: "html", Body: `li count 5`}}
	msg := checks.HTML(expr, body)
	// Should look like Text failure message using %#q format
	want := "li = `2`, want `5`"
	if msg != want {
		t.Errorf("HTML(li count 5): got %q, want %q", msg, want)
	}
}

func TestHTMLIgnoresExprName(t *testing.T) {
	body := `<div id="test">content</div>`
	// Use a different name - should still work because HTML ignores expr.Name
	expr := linebased.Expanded{Expression: linebased.Expression{Name: "something-else", Body: `#test == content`}}
	msg := checks.HTML(expr, body)
	if msg != "" {
		t.Errorf("HTML with different expr.Name: unexpected error: %s", msg)
	}
}
