package forms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
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
	GrantedRole *access.RoleChoice         `json:"grantedRole,omitempty"`
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

func (h handler) submitAnswers(revision, includeLocation bool) http.HandlerFunc {
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
			if err == nil && includeLocation {
				w.Header().Set("Location", fmt.Sprintf("/api/v1/forms/%d/submissions/mine", id))
			}
		}

		var invalid answerErrors
		switch {
		case errors.As(err, &invalid):
			utils.WriteAPIError(w, http.StatusUnprocessableEntity, "invalid_answers", invalid, invalid)
		case errors.Is(err, errAlreadySubmitted):
			utils.WriteAPIError(w, http.StatusConflict, "already_submitted", err, nil)
		case errors.Is(err, errPublishedVersionChanged):
			utils.WriteAPIError(w, http.StatusConflict, "version_changed", err, nil)
		case errors.Is(err, errSubmissionChanged):
			utils.WriteAPIError(w, http.StatusConflict, "submission_changed", err, nil)
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
	if action := result.Definition.ApprovalAction; result.Status == "approved" && action != nil && action.Kind == "assign_role" {
		role, err := (access.Store{DB: s.db}).SubmissionRole(ctx, result.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			result.GrantedRole = &role
		}
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
	var lockedID int64
	err = tx.QueryRow(ctx, `SELECT id FROM forms WHERE id = $1 FOR SHARE`, id).Scan(&lockedID)
	if err != nil {
		return submission{}, replaceNoRows(err, errNotFound)
	}
	published, err := readPublished(ctx, tx, id, &user, false)
	if err != nil {
		return submission{}, err
	}
	result := submission{Version: published.Version, Definition: published.Definition}
	if result.Version != input.Version {
		return submission{}, errPublishedVersionChanged
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
