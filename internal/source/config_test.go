package source

import "testing"

// TestFrontMatterFlowQuotes checks that an apostrophe inside a bare flow
// value does not open a string that swallows the following lines, and that
// a value that is not a whole flow collection stays a string.
func TestFrontMatterFlowQuotes(t *testing.T) {
	d, err := SplitDoc("---\nconfig: {theme: dark, themeVariables: {fontFamily: Bob's Font}}\ntitle: Hello\n---\ngraph LR\nA-->B")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Hello" || d.Theme != "dark" || d.Vars["fontFamily"] != "Bob's Font" {
		t.Errorf("title %q theme %q vars %v", d.Title, d.Theme, d.Vars)
	}
	d, err = SplitDoc("---\ntitle: [Draft] Plan\n---\ngraph LR\nA-->B")
	if err != nil || d.Title != "[Draft] Plan" {
		t.Errorf("title %q, %v", d.Title, err)
	}
	d, err = SplitDoc("---\nconfig: {theme: forest,\n  themeVariables: {primaryColor: '#ff0000'}}\ntitle: T\n---\ngraph LR\nA-->B")
	if err != nil || d.Theme != "forest" || d.Vars["primaryColor"] != "#ff0000" || d.Title != "T" {
		t.Errorf("multi-line flow: %+v, %v", d, err)
	}
}

// TestFrontMatterFlowComments checks that comments after and inside a flow
// collection are not read as part of it: a bracket in a comment made a
// valid collection "unclosed".
func TestFrontMatterFlowComments(t *testing.T) {
	d, err := SplitDoc("---\nconfig:\n  themeVariables: {primaryColor: '#f00'} # [x\ntitle: T\n---\ngraph LR\nA-->B")
	if err != nil || d.Vars["primaryColor"] != "#f00" || d.Title != "T" {
		t.Errorf("comment after a collection: %+v, %v", d, err)
	}
	d, err = SplitDoc("---\nconfig: {theme: forest, # the theme [\n  themeVariables: {primaryColor: '#0f0'}} # {\n---\ngraph LR\nA-->B")
	if err != nil || d.Theme != "forest" || d.Vars["primaryColor"] != "#0f0" {
		t.Errorf("comments in a multi-line collection: %+v, %v", d, err)
	}
	d, err = SplitDoc("---\ntitle: [Draft] Plan # note\n---\ngraph LR\nA-->B")
	if err != nil || d.Title != "[Draft] Plan" {
		t.Errorf("comment after a bracketed title: %q, %v", d.Title, err)
	}
}
