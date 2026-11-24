package forms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5"
)

var errAlreadySubmitted = errors.New("you already submitted this form")
var errPublishedVersionChanged = errors.New("a new version was published; review it before submitting")
var errSubmissionChanged = errors.New("this submission changed; reload it before continuing")

type submission struct {
	ID          int64                      `json:"id"`
	Version     int                        `json:"version"`
	Revision    int                        `json:"revision"`
	Status      string                     `json:"status"`
	SubmittedAt time.Time                  `json:"submittedAt"`
	Definition  Definition                 `json:"definition"`
	Values      map[string]json.RawMessage `json:"values"`
	Changes     map[string]string          `json:"changes,omitempty"`
	Comment     []reviewPart               `json:"comment,omitempty"`
}

type submissionInput struct {
	Version  int                        `json:"version"`
	Revision int                        `json:"revision,omitempty"`
	Values   map[string]json.RawMessage `json:"values"`
}

type answerErrors map[string]string

func (answerErrors) Error() string { return "correct the highlighted answers" }

func (h handler) getMySubmission(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	user, _ := middlewares.Session(r.Context())
	result, err := h.store.getSubmission(r.Context(), id, user.Id)
	writeResult(w, http.StatusOK, result, err)
}

func (h handler) submitAnswers(revision bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := readID(w, r)
		if !ok {
			return
		}
		var input submissionInput
		if !readJSONWithLimit(w, r, &input, 3<<20) {
			return
		}
		if input.Version <= 0 || (revision && input.Revision <= 0) || (!revision && input.Revision != 0) || input.Values == nil {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("send the form version, answers and, for revisions, the submission revision"))
			return
		}
		user, _ := middlewares.Session(r.Context())
		status := http.StatusCreated
		var result submission
		var err error
		if revision {
			status = http.StatusOK
			result, err = h.store.resubmit(r.Context(), id, user.Id, input)
		} else {
			result, err = h.store.submit(r.Context(), id, user.Id, input)
		}

		var invalid answerErrors
		switch {
		case errors.As(err, &invalid):
			utils.WriteJson(w, http.StatusUnprocessableEntity, map[string]any{"error": invalid.Error(), "errors": invalid})
		case errors.Is(err, errAlreadySubmitted):
			utils.WriteJson(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "already_submitted"})
		case errors.Is(err, errPublishedVersionChanged):
			utils.WriteJson(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "version_changed"})
		case errors.Is(err, errSubmissionChanged):
			utils.WriteJson(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "submission_changed"})
		default:
			writeResult(w, status, result, err)
		}
	}
}

const submissionByUser = `
    SELECT s.id, s.form_version, s.revision, s.status, s.submitted_at, v.definition, s.answers, s.requested_changes
    FROM form_submissions s
    JOIN form_versions v ON v.form_id = s.form_id AND v.version = s.form_version
    WHERE s.form_id = $1 AND s.user_id = $2
`

func scanSubmission(row pgx.Row) (submission, error) {
	var result submission
	var feedback []byte
	err := row.Scan(&result.ID, &result.Version, &result.Revision, &result.Status, &result.SubmittedAt, &result.Definition, &result.Values, &feedback)
	if err != nil {
		return result, err
	}
	return result, result.readReview(feedback)
}

func (s store) getSubmission(ctx context.Context, id int64, user int) (*submission, error) {
	result, err := scanSubmission(s.db.QueryRow(ctx, submissionByUser, id, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s store) submit(ctx context.Context, id int64, user int, input submissionInput) (submission, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return submission{}, err
	}
	defer tx.Rollback(ctx)

	// Share the form lock with other submitters and serialize against publishing.
	var result submission
	err = tx.QueryRow(ctx, `
        SELECT published_version FROM forms
        WHERE id = $1 AND published_version IS NOT NULL FOR SHARE
    `, id).Scan(&result.Version)
	if err != nil {
		return submission{}, replaceNoRows(err, errNotFound)
	}
	if result.Version != input.Version {
		return submission{}, errPublishedVersionChanged
	}
	err = tx.QueryRow(ctx, `
        SELECT definition FROM form_versions WHERE form_id = $1 AND version = $2
    `, id, result.Version).Scan(&result.Definition)
	if err != nil {
		return submission{}, err
	}

	result.Values, err = checkedAnswers(result.Definition, input.Values)
	if err != nil {
		return submission{}, err
	}
	err = tx.QueryRow(ctx, `
        INSERT INTO form_submissions (form_id, user_id, form_version, answers)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (form_id, user_id) DO NOTHING
        RETURNING id, status, revision, submitted_at
    `, id, user, result.Version, result.Values).Scan(&result.ID, &result.Status, &result.Revision, &result.SubmittedAt)
	if err != nil {
		return submission{}, replaceNoRows(err, errAlreadySubmitted)
	}
	return result, tx.Commit(ctx)
}

func (s store) resubmit(ctx context.Context, id int64, user int, input submissionInput) (submission, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return submission{}, err
	}
	defer tx.Rollback(ctx)
	result, err := scanSubmission(tx.QueryRow(ctx, submissionByUser+" FOR UPDATE OF s", id, user))
	if err != nil {
		return submission{}, replaceNoRows(err, errNotFound)
	}
	if result.Status != "changes_requested" || result.Version != input.Version || result.Revision != input.Revision {
		return submission{}, errSubmissionChanged
	}

	result.Values, err = checkedAnswers(result.Definition, input.Values)
	if err != nil {
		return submission{}, err
	}
	err = tx.QueryRow(ctx, `
        UPDATE form_submissions
        SET answers = $2, status = 'submitted', revision = revision + 1, submitted_at = CURRENT_TIMESTAMP
        WHERE id = $1 RETURNING status, revision, submitted_at
    `, result.ID, result.Values).Scan(&result.Status, &result.Revision, &result.SubmittedAt)
	if err != nil {
		return submission{}, err
	}
	return result, tx.Commit(ctx)
}

func checkedAnswers(definition Definition, values map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if problems := definition.validateValues(values); len(problems) != 0 {
		return nil, answerErrors(problems)
	}
	answers := make(map[string]json.RawMessage, len(definition.Fields))
	for _, field := range definition.Fields {
		if value, exists := values[field.ID]; exists {
			answers[field.ID] = value
		}
	}
	return answers, nil
}
