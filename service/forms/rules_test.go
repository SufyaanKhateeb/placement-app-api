package forms

import (
	"encoding/json"
	"testing"
)

func countLimit(n int) *int { return &n }

func TestFieldRulesRejectInvalidSettings(t *testing.T) {
	for _, tc := range []struct {
		name, fieldType string
		rules           FieldRules
	}{
		{"broken regex", "text", FieldRules{Pattern: "["}},
		{"reversed lengths", "text", FieldRules{MinLength: countLimit(8), MaxLength: countLimit(2)}},
		{"unsupported length", "text", FieldRules{MaxLength: countLimit(4001)}},
		{"reversed numeric range", "numeric", FieldRules{Min: "0.0000000000000000002", Max: "0.0000000000000000001"}},
		{"fractional integer bound", "integer", FieldRules{Min: "1.5"}},
		{"invalid date", "date", FieldRules{Max: "2026-02-30"}},
		{"timestamp without offset", "timestamptz", FieldRules{Min: "2026-11-22T01:00:00"}},
		{"regex on numeric field", "numeric", FieldRules{Pattern: "^1"}},
		{"bounds on boolean field", "boolean", FieldRules{Min: "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := Definition{Title: "Rules", Fields: []Field{
				{ID: "sample", Label: "Sample", Type: tc.fieldType, Rules: &tc.rules},
			}}
			if definition.validate() == nil {
				t.Fatal("invalid field rules were accepted")
			}
		})
	}
}

func TestFieldRulesValidateAnswers(t *testing.T) {
	for _, tc := range []struct {
		name, fieldType, answer string
		rules                   FieldRules
		want                    string
	}{
		{"unicode length and regex", "text", `"éA"`, FieldRules{MinLength: countLimit(2), MaxLength: countLimit(2), Pattern: "^é[A-Z]$"}, ""},
		{"too short", "text", `"é"`, FieldRules{MinLength: countLimit(2)}, "Enter at least 2 characters."},
		{"too long", "text", `"abcd"`, FieldRules{MaxLength: countLimit(3)}, "Enter at most 3 characters."},
		{"regex mismatch", "text", `"ab-123"`, FieldRules{Pattern: "^[A-Z]{2}-[0-9]{3}$"}, "Enter a value that matches the expected format."},
		{"integer lower bound is inclusive", "integer", `0`, FieldRules{Min: "0", Max: "12"}, ""},
		{"integer above range", "integer", `13`, FieldRules{Max: "12"}, "Enter a value on or below 12."},
		{"numeric comparison retains precision", "numeric", `"1.00000000000000000001"`, FieldRules{Min: "1.00000000000000000002"}, "Enter a value on or above 1.00000000000000000002."},
		{"date before range", "date", `"2026-11-21"`, FieldRules{Min: "2026-11-22"}, "Enter a value on or above 2026-11-22."},
		{"timestamp compares equivalent instants", "timestamptz", `"2026-11-22T05:30:00+05:30"`, FieldRules{Max: "2026-11-22T00:00:00Z"}, ""},
		{"optional answer remains optional", "text", `null`, FieldRules{MinLength: countLimit(2)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := Definition{Title: "Rules", Fields: []Field{
				{ID: "sample", Label: "Sample", Type: tc.fieldType, Rules: &tc.rules},
			}}
			if err := definition.validate(); err != nil {
				t.Fatal(err)
			}
			problems := definition.validateValues(map[string]json.RawMessage{"sample": json.RawMessage(tc.answer)})
			if got := problems["sample"]; got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
