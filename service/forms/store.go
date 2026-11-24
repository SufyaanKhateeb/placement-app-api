package forms

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errConflict = errors.New("this form changed; review the latest draft before continuing")
var errNotFound = errors.New("form not found")
var errEmptyForm = errors.New("add at least one field before publishing")

type Summary struct {
	ID               int64   `json:"id"`
	Title            string  `json:"title"`
	HasDraft         bool    `json:"hasDraft"`
	PublishedVersion int     `json:"publishedVersion"`
	SubmissionStatus *string `json:"submissionStatus,omitempty"`
}

type Form struct {
	ID               int64       `json:"id"`
	Revision         int64       `json:"revision"`
	Draft            *Definition `json:"draft"`
	PublishedVersion int         `json:"publishedVersion"`
	Published        *Definition `json:"published"`
}

type PublishedForm struct {
	ID         int64      `json:"id"`
	Version    int        `json:"version"`
	Definition Definition `json:"definition"`
}

type store struct {
	db *pgxpool.Pool
}

func (s store) list(ctx context.Context, admin bool, userID *int) ([]Summary, error) {
	rows, err := s.db.Query(ctx, `
		SELECT f.id,
		       COALESCE(CASE WHEN $1 THEN f.draft->>'title' END, v.definition->>'title'),
		       $1 AND f.draft IS NOT NULL, COALESCE(f.published_version, 0),
		       CASE WHEN $2::integer IS NOT NULL THEN COALESCE(s.status, 'not_submitted') END
		FROM forms f
		LEFT JOIN form_versions v ON v.form_id = f.id AND v.version = f.published_version
		LEFT JOIN form_submissions s ON s.form_id = f.id AND s.user_id = $2
		WHERE $1 OR f.published_version IS NOT NULL
		ORDER BY f.id DESC
	`, admin, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	forms := []Summary{}
	for rows.Next() {
		var form Summary
		if err := rows.Scan(&form.ID, &form.Title, &form.HasDraft, &form.PublishedVersion, &form.SubmissionStatus); err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, rows.Err()
}

func (s store) create(ctx context.Context) (Form, error) {
	draft := Definition{Title: "Untitled form", Fields: []Field{}}
	form := Form{Draft: &draft}
	err := s.db.QueryRow(ctx, `
		INSERT INTO forms (draft) VALUES ($1) RETURNING id, draft_revision
	`, draft).Scan(&form.ID, &form.Revision)
	return form, err
}

func (s store) get(ctx context.Context, id int64) (Form, error) {
	var form Form
	err := s.db.QueryRow(ctx, `
		SELECT f.id, f.draft_revision, f.draft, COALESCE(f.published_version, 0), v.definition
		FROM forms f
		LEFT JOIN form_versions v ON v.form_id = f.id AND v.version = f.published_version
		WHERE f.id = $1
	`, id).Scan(&form.ID, &form.Revision, &form.Draft, &form.PublishedVersion, &form.Published)
	return form, replaceNoRows(err, errNotFound)
}

func (s store) getPublished(ctx context.Context, id int64) (PublishedForm, error) {
	var form PublishedForm
	err := s.db.QueryRow(ctx, `
		SELECT f.id, v.version, v.definition
		FROM forms f
		JOIN form_versions v ON v.form_id = f.id AND v.version = f.published_version
		WHERE f.id = $1
	`, id).Scan(&form.ID, &form.Version, &form.Definition)
	return form, replaceNoRows(err, errNotFound)
}

func (s store) save(ctx context.Context, id, revision int64, draft Definition) (int64, error) {
	var next int64
	err := s.db.QueryRow(ctx, `
		UPDATE forms SET draft = $3, draft_revision = draft_revision + 1
		WHERE id = $1 AND draft_revision = $2 AND draft IS NOT NULL
		RETURNING draft_revision
	`, id, revision, draft).Scan(&next)
	return next, replaceNoRows(err, errConflict)
}

func (s store) editPublished(ctx context.Context, id, revision int64) (Form, error) {
	form := Form{ID: id}
	err := s.db.QueryRow(ctx, `
		UPDATE forms f
		SET draft = v.definition, draft_revision = f.draft_revision + 1
		FROM form_versions v
		WHERE f.id = $1 AND f.draft_revision = $2 AND f.draft IS NULL
		  AND v.form_id = f.id AND v.version = f.published_version
		RETURNING f.draft_revision, f.draft, f.published_version
	`, id, revision).Scan(&form.Revision, &form.Draft, &form.PublishedVersion)
	if err == nil {
		form.Published = form.Draft
	}
	return form, replaceNoRows(err, errConflict)
}

func (s store) publish(ctx context.Context, id, revision int64) (Form, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Form{}, err
	}
	defer tx.Rollback(ctx)

	form := Form{ID: id}
	// Claim the revision first. The row lock is held until the snapshot and
	// pointer are committed together.
	err = tx.QueryRow(ctx, `
		UPDATE forms SET draft_revision = draft_revision + 1
		WHERE id = $1 AND draft_revision = $2 AND draft IS NOT NULL
		RETURNING draft_revision, draft, COALESCE(published_version, 0) + 1
	`, id, revision).Scan(&form.Revision, &form.Published, &form.PublishedVersion)
	if err != nil {
		return Form{}, replaceNoRows(err, errConflict)
	}
	if len(form.Published.Fields) == 0 {
		return Form{}, errEmptyForm
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO form_versions (form_id, version, definition) VALUES ($1, $2, $3)
	`, id, form.PublishedVersion, form.Published); err != nil {
		return Form{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forms SET draft = NULL, published_version = $2 WHERE id = $1
	`, id, form.PublishedVersion); err != nil {
		return Form{}, err
	}
	return form, tx.Commit(ctx)
}

func replaceNoRows(err, replacement error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return replacement
	}
	return err
}
