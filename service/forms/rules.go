package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type FieldRules struct {
	MinLength *int   `json:"minLength,omitempty"`
	MaxLength *int   `json:"maxLength,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Min       string `json:"min,omitempty"`
	Max       string `json:"max,omitempty"`
}

func (r *FieldRules) empty() bool {
	return r.MinLength == nil && r.MaxLength == nil && r.Pattern == "" && r.Min == "" && r.Max == ""
}

func (r *FieldRules) validateFor(fieldType string) error {
	r.Min, r.Max = strings.TrimSpace(r.Min), strings.TrimSpace(r.Max)
	hasTextRules := r.MinLength != nil || r.MaxLength != nil || r.Pattern != ""
	hasBounds := r.Min != "" || r.Max != ""
	switch fieldType {
	case "text":
		if hasBounds {
			return errors.New("text fields use length limits and a pattern")
		}
		if !validLengthLimit(r.MinLength) || !validLengthLimit(r.MaxLength) {
			return errors.New("length limits must be between 0 and 4,000 characters")
		}
		if r.MinLength != nil && r.MaxLength != nil && *r.MinLength > *r.MaxLength {
			return errors.New("minimum length must be at most the maximum length")
		}
		if len(r.Pattern) > 256 {
			return errors.New("the regex pattern must be at most 256 bytes")
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return errors.New("enter a valid Go regular expression")
		}
	case "integer", "numeric", "date", "timestamptz":
		if hasTextRules {
			return errors.New("length and regex rules apply only to text fields")
		}
		if r.Min != "" {
			if _, ok := compareBoundValues(fieldType, r.Min, r.Min); !ok {
				return errors.New("enter a valid minimum value")
			}
		}
		if r.Max != "" {
			if _, ok := compareBoundValues(fieldType, r.Max, r.Max); !ok {
				return errors.New("enter a valid maximum value")
			}
		}
		if r.Min != "" && r.Max != "" {
			if order, _ := compareBoundValues(fieldType, r.Min, r.Max); order > 0 {
				return errors.New("minimum value must be at most the maximum value")
			}
		}
	case "boolean":
		if hasTextRules || hasBounds {
			return errors.New("yes/no fields support required or optional answers")
		}
	}
	return nil
}

func validLengthLimit(value *int) bool {
	return value == nil || (*value >= 0 && *value <= 4000)
}

func (r FieldRules) answerError(fieldType string, raw json.RawMessage) string {
	value := string(raw)
	if fieldType != "integer" {
		_ = json.Unmarshal(raw, &value)
	}
	if fieldType == "text" {
		length := utf8.RuneCountInString(value)
		if r.MinLength != nil && length < *r.MinLength {
			return fmt.Sprintf("Enter at least %d characters.", *r.MinLength)
		}
		if r.MaxLength != nil && length > *r.MaxLength {
			return fmt.Sprintf("Enter at most %d characters.", *r.MaxLength)
		}
		if r.Pattern != "" {
			matches, err := regexp.MatchString(r.Pattern, value)
			if err != nil || !matches {
				return "Enter a value that matches the expected format."
			}
		}
		return ""
	}

	if r.Min != "" {
		if order, ok := compareBoundValues(fieldType, value, r.Min); ok && order < 0 {
			return "Enter a value on or above " + r.Min + "."
		}
	}
	if r.Max != "" {
		if order, ok := compareBoundValues(fieldType, value, r.Max); ok && order > 0 {
			return "Enter a value on or below " + r.Max + "."
		}
	}
	return ""
}

func compareBoundValues(fieldType, left, right string) (int, bool) {
	switch fieldType {
	case "integer":
		a, errA := strconv.ParseInt(left, 10, 32)
		b, errB := strconv.ParseInt(right, 10, 32)
		if errA != nil || errB != nil {
			return 0, false
		}
		return compareInt64(a, b), true
	case "numeric":
		if !validDecimal(left) || !validDecimal(right) {
			return 0, false
		}
		a, okA := new(big.Rat).SetString(left)
		b, okB := new(big.Rat).SetString(right)
		if !okA || !okB {
			return 0, false
		}
		return a.Cmp(b), true
	case "date", "timestamptz":
		layout := time.DateOnly
		if fieldType == "timestamptz" {
			layout = time.RFC3339Nano
		}
		a, errA := time.Parse(layout, left)
		b, errB := time.Parse(layout, right)
		if errA != nil || errB != nil || a.Year() < 1 || b.Year() < 1 {
			return 0, false
		}
		return a.Compare(b), true
	default:
		return 0, false
	}
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func validDecimal(value string) bool {
	return len(value) <= 100 && decimalPattern.MatchString(value)
}
