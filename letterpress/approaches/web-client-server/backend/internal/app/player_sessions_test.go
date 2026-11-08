package app

import "testing"

func TestNormalizePlayerName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		display    string
		normalized string
		valid      bool
	}{
		{name: "simple", input: "Chirayu", display: "Chirayu", normalized: "chirayu", valid: true},
		{name: "trim and collapse spaces", input: "  Blue   Fox  ", display: "Blue Fox", normalized: "blue fox", valid: true},
		{name: "punctuation allowed", input: "word_bot-2", display: "word_bot-2", normalized: "word_bot-2", valid: true},
		{name: "empty", input: "   ", valid: false},
		{name: "too long", input: "abcdefghijklmnopqrstuvwxy", valid: false},
		{name: "non ascii", input: "José", valid: false},
		{name: "tab", input: "Blue\tFox", valid: false},
		{name: "punctuation", input: "Blue!", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			display, normalized, err := normalizePlayerName(test.input)
			if test.valid && err != nil {
				t.Fatalf("normalize name: %v", err)
			}
			if !test.valid {
				if err == nil {
					t.Fatalf("normalize name = %q/%q, want error", display, normalized)
				}
				return
			}
			if display != test.display || normalized != test.normalized {
				t.Fatalf("normalize name = %q/%q, want %q/%q", display, normalized, test.display, test.normalized)
			}
		})
	}
}
