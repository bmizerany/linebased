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
