package forms

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const commentKey = "$comment"

type reviewPart struct {
	Text    string `json:"text,omitempty"`
	FieldID string `json:"fieldId,omitempty"`
}

func (result *submission) readReview(raw []byte) error {
	var feedback map[string]json.RawMessage
	if err := json.Unmarshal(raw, &feedback); err != nil {
		return err
	}
	if comment, ok := feedback[commentKey]; ok {
		return json.Unmarshal(comment, &result.Comment)
	}
	return json.Unmarshal(raw, &result.Changes)
}

func checkedReviewComment(fields []Field, comment []reviewPart) (map[string]any, error) {
	labels := make(map[string]string, len(fields))
	for _, field := range fields {
		labels[field.ID] = field.Label
	}
	hasMention, hasText := false, false
	length := 0
	for _, part := range comment {
		if part.FieldID != "" {
			label, exists := labels[part.FieldID]
			if !exists || part.Text != "" {
				return nil, errors.New("mentions must refer to fields on the submitted version")
			}
			hasMention = true
			length += utf8.RuneCountInString(label) + 1
			continue
		}
		if part.Text == "" || strings.ContainsRune(part.Text, 0) {
			return nil, errors.New("enter a valid review comment")
		}
		hasText = hasText || strings.TrimSpace(part.Text) != ""
		length += utf8.RuneCountInString(part.Text)
	}
	if !hasText {
		return nil, errors.New("describe what needs to change")
	}
	if !hasMention || length > 4000 {
		return nil, errors.New("mention at least one field and keep the comment under 4,000 characters")
	}
	return map[string]any{commentKey: comment}, nil
}
