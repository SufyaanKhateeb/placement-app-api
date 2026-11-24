package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Field struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Type     string      `json:"type"`
	Required bool        `json:"required"`
	Rules    *FieldRules `json:"rules,omitempty"`
}

type Definition struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

var fieldIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)

func (d *Definition) validate() error {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" || utf8.RuneCountInString(d.Title) > 120 {
		return errors.New("enter a form title of 1 to 120 characters")
	}
	if utf8.RuneCountInString(d.Description) > 2000 || len(d.Fields) > 100 {
		return errors.New("a form can have up to 100 fields and 2,000 description characters")
	}
	if d.Fields == nil {
		d.Fields = []Field{}
	}
	seen := make(map[string]bool, len(d.Fields))
	for i := range d.Fields {
		field := &d.Fields[i]
		field.Label = strings.TrimSpace(field.Label)
		if !fieldIDPattern.MatchString(field.ID) || seen[field.ID] {
			return errors.New("field IDs must be valid and unique")
		}
		seen[field.ID] = true
		if field.Label == "" || utf8.RuneCountInString(field.Label) > 120 {
			return errors.New("each field needs a label of 1 to 120 characters")
		}
		switch field.Type {
		case "text", "boolean", "integer", "numeric", "date", "timestamptz":
		default:
			return errors.New("each field needs a supported type")
		}
		if field.Rules != nil {
			rules := *field.Rules
			if err := rules.validateFor(field.Type); err != nil {
				return fmt.Errorf("%s: %w", field.Label, err)
			}
			if rules.empty() {
				field.Rules = nil
			} else {
				field.Rules = &rules
			}
		}
	}
	return nil
}

func (d Definition) validateValues(values map[string]json.RawMessage) map[string]string {
	problems := make(map[string]string)
	for _, field := range d.Fields {
		value := values[field.ID]
		if len(value) == 0 || string(value) == "null" || string(value) == `""` {
			if field.Required {
				problems[field.ID] = "This field is required."
			}
			continue
		}
		if !validFieldValue(field.Type, value) {
			problems[field.ID] = "Enter a valid " + field.Type + " value."
			continue
		}
		if field.Rules != nil {
			if message := field.Rules.answerError(field.Type, value); message != "" {
				problems[field.ID] = message
			}
		}
	}
	return problems
}

func validFieldValue(fieldType string, raw json.RawMessage) bool {
	switch fieldType {
	case "boolean":
		return string(raw) == "true" || string(raw) == "false"
	case "integer":
		_, err := strconv.ParseInt(string(raw), 10, 32)
		return err == nil
	}

	var value string
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch fieldType {
	case "text":
		return strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 4000
	case "numeric":
		return validDecimal(value)
	case "date":
		date, err := time.Parse(time.DateOnly, value)
		return err == nil && date.Year() >= 1
	case "timestamptz":
		stamp, err := time.Parse(time.RFC3339Nano, value)
		return err == nil && stamp.Year() >= 1
	default:
		return false
	}
}
