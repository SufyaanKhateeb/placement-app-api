package forms

import (
	"encoding/json"
	"testing"
)

func TestDefinitionValidatesTypedAnswers(t *testing.T) {
	definition := Definition{
		Title: "Access request",
		Fields: []Field{
			{ID: "name", Label: "Name", Type: "text", Required: true},
			{ID: "active", Label: "Active", Type: "boolean", Required: true},
			{ID: "days", Label: "Days", Type: "integer"},
			{ID: "amount", Label: "Amount", Type: "numeric"},
			{ID: "date", Label: "Date", Type: "date"},
			{ID: "start", Label: "Start", Type: "timestamptz"},
		},
	}
	if err := definition.validate(); err != nil {
		t.Fatal(err)
	}
	valid := map[string]json.RawMessage{
		"name": json.RawMessage(`"Sam"`), "active": json.RawMessage(`false`),
		"days": json.RawMessage(`0`), "amount": json.RawMessage(`"1234567890.123456789"`),
		"date": json.RawMessage(`"2026-11-22"`), "start": json.RawMessage(`"2026-11-22T09:00:00+05:30"`),
	}
	if problems := definition.validateValues(valid); len(problems) != 0 {
		t.Fatalf("valid answers rejected: %v", problems)
	}

	delete(valid, "name")
	valid["active"] = json.RawMessage(`"false"`)
	valid["days"] = json.RawMessage(`2147483648`)
	valid["amount"] = json.RawMessage(`"1e2"`)
	valid["date"] = json.RawMessage(`"2026-02-30"`)
	valid["start"] = json.RawMessage(`"2026-11-22T09:00:00"`)
	problems := definition.validateValues(valid)
	for _, field := range definition.Fields {
		if problems[field.ID] == "" {
			t.Errorf("expected a validation error for %s", field.ID)
		}
	}
}

func TestDefinitionRejectsDuplicateFieldIDs(t *testing.T) {
	field := Field{ID: "duplicate", Label: "Duplicate field", Type: "text"}
	definition := Definition{Title: "Example", Fields: []Field{field, field}}
	if definition.validate() == nil {
		t.Fatal("duplicate field IDs were accepted")
	}
}
