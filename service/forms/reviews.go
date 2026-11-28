package forms

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SufyaanKhateeb/college-placement-app-api/middlewares"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5"
)

const reviewPageSize = 50

type reviewSummary struct {
	ID          int64     `json:"id"`
	FormTitle   string    `json:"formTitle"`
	Version     int       `json:"version"`
	Status      string    `json:"status"`
	SubmittedAt time.Time `json:"submittedAt"`
	UserName    string    `json:"userName"`
	UserEmail   string    `json:"userEmail"`
}

type reviewPage struct {
	Items      []reviewSummary `json:"items"`
	NextBefore int64           `json:"nextBefore,omitempty"`
}

type reviewDetail struct {
	submission
	UserName              string `json:"userName"`
	UserEmail             string `json:"userEmail"`
	ApprovalRoleAvailable *bool  `json:"approvalRoleAvailable,omitempty"`
}

type reviewInput struct {
	Revision int               `json:"revision"`
	Decision string            `json:"decision"`
	Changes  map[string]string `json:"changes"`
	Comment  []reviewPart      `json:"comment"`
}

func (h handler) listReviews(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	search := strings.TrimSpace(params.Get("q"))
	if utf8.RuneCountInString(search) > 256 || strings.ContainsRune(search, 0) {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid submission search"))
		return
	}
	status := params.Get("status")
	switch status {
	case "", "submitted", "changes_requested", "approved":
	default:
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid submission status"))
		return
	}
	var before *int64
	if raw := params.Get("before"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			utils.WriteJsonError(w, http.StatusBadRequest, errors.New("invalid submission cursor"))
			return
		}
		before = &value
	}
	page, err := h.store.listReviews(r.Context(), before, search, status)
	writeResult(w, http.StatusOK, page, err)
}

func (h handler) getReview(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	result, err := h.store.getReview(r.Context(), id)
	if action := result.Definition.ApprovalAction; err == nil && result.Status == "submitted" && action != nil && action.Kind == "assign_role" {
		_, err = access.ApprovalRole(r.Context(), h.store.db, action.RoleID, action.Role)
		available := err == nil
		result.ApprovalRoleAvailable = &available
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
	}
	writeResult(w, http.StatusOK, result, err)
}

func (h handler) reviewSubmission(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var input reviewInput
	if !readJSONWithLimit(w, r, &input, 512<<10) {
		return
	}
	if input.Revision <= 0 {
		utils.WriteJsonError(w, http.StatusBadRequest, errors.New("a submission revision is required"))
		return
	}
	result, err := h.store.getReview(r.Context(), id)
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}

	changes, err := input.validate(result.Definition)
	if err != nil {
		utils.WriteJsonError(w, http.StatusBadRequest, err)
		return
	}
	tx, err := h.store.db.Begin(r.Context())
	if err != nil {
		writeResult(w, http.StatusOK, nil, err)
		return
	}
	defer tx.Rollback(r.Context())
	var feedback []byte
	err = tx.QueryRow(r.Context(), `
        UPDATE form_submissions
        SET status = $3, requested_changes = $4, revision = revision + 1
        WHERE id = $1 AND revision = $2 AND status = 'submitted'
        RETURNING status, revision, requested_changes
    `, id, input.Revision, input.Decision, changes).Scan(&result.Status, &result.Revision, &feedback)
	if err = replaceNoRows(err, errSubmissionChanged); errors.Is(err, errSubmissionChanged) {
		utils.WriteAPIError(w, http.StatusConflict, "submission_changed", err, nil)
		return
	}
	if err == nil {
		result.Changes = nil
		result.Comment = nil
		err = result.readReview(feedback)
	}
	if err == nil && input.Decision == "approved" {
		var roleID *int64
		roleID, err = recordVerification(r.Context(), tx, id, result.Definition.ApprovalAction)
		if action := result.Definition.ApprovalAction; action != nil && action.Kind == "assign_role" {
			granted := roleID != nil
			result.ApprovalRoleAvailable = &granted
		}
		if err == nil && roleID != nil {
			actor, _ := middlewares.Session(r.Context())
			err = access.Audit(r.Context(), tx, actor, "approval.granted", map[string]any{"submissionId": id, "roleId": *roleID})
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	writeResult(w, http.StatusOK, result, err)
}

func (input reviewInput) validate(definition Definition) (map[string]any, error) {
	changes := make(map[string]any)
	switch input.Decision {
	case "approved":
		if len(input.Changes) > 0 || len(input.Comment) > 0 {
			return nil, errors.New("approval cannot include requested changes")
		}
	case "changes_requested":
		if len(input.Comment) > 0 {
			if len(input.Changes) > 0 {
				return nil, errors.New("send one review comment")
			}
			return checkedReviewComment(definition.Fields, input.Comment)
		}
		if len(input.Changes) == 0 || len(input.Changes) > len(definition.Fields) {
			return nil, errors.New("specify which fields need changes")
		}
		for _, field := range definition.Fields {
			note, present := input.Changes[field.ID]
			if !present {
				continue
			}
			note = strings.TrimSpace(note)
			if note == "" || utf8.RuneCountInString(note) > 500 || strings.ContainsRune(note, 0) {
				return nil, errors.New("each change request must contain 1 to 500 characters")
			}
			changes[field.ID] = note
		}
		if len(changes) != len(input.Changes) {
			return nil, errors.New("change requests must refer to fields on the submitted version")
		}
	default:
		return nil, errors.New("choose approve or request changes")
	}
	return changes, nil
}

func (s store) listReviews(ctx context.Context, before *int64, search, status string) (reviewPage, error) {
	page := reviewPage{Items: make([]reviewSummary, 0)}
	rows, err := s.db.Query(ctx, `
        SELECT s.id, v.definition->>'title', s.form_version, s.status, s.submitted_at,
               concat_ws(' ', u.firstname, u.lastname), u.email
        FROM form_submissions s
        JOIN form_versions v ON v.form_id = s.form_id AND v.version = s.form_version
        JOIN app_user u ON u.id = s.user_id
        WHERE ($1::bigint IS NULL OR s.id < $1)
          AND ($2::text = '' OR s.status = $2)
          AND ($3::text = ''
               OR strpos(lower(v.definition->>'title'), lower($3)) > 0
               OR strpos(lower(concat_ws(' ', u.firstname, u.lastname)), lower($3)) > 0
               OR strpos(lower(u.email), lower($3)) > 0)
        ORDER BY s.id DESC LIMIT $4
    `, before, status, search, reviewPageSize+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item reviewSummary
		if err := rows.Scan(&item.ID, &item.FormTitle, &item.Version, &item.Status, &item.SubmittedAt, &item.UserName, &item.UserEmail); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if len(page.Items) > reviewPageSize {
		page.Items = page.Items[:reviewPageSize]
		page.NextBefore = page.Items[reviewPageSize-1].ID
	}
	return page, rows.Err()
}

func (s store) getReview(ctx context.Context, id int64) (reviewDetail, error) {
	var result reviewDetail
	var feedback []byte
	err := s.db.QueryRow(ctx, `
        SELECT s.id, s.form_version, s.revision, s.status, s.submitted_at,
               v.definition, s.answers, s.requested_changes,
               concat_ws(' ', u.firstname, u.lastname), u.email
        FROM form_submissions s
        JOIN form_versions v ON v.form_id = s.form_id AND v.version = s.form_version
        JOIN app_user u ON u.id = s.user_id
        WHERE s.id = $1
    `, id).Scan(&result.ID, &result.Version, &result.Revision, &result.Status, &result.SubmittedAt,
		&result.Definition, &result.Values, &feedback, &result.UserName, &result.UserEmail)
	if err != nil {
		return result, replaceNoRows(err, errNotFound)
	}
	return result, result.readReview(feedback)
}
