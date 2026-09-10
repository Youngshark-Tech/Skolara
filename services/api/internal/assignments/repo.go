package assignments

import (
	"context"
	"errors"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the assignments repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

func (r *pgRepo) RosteredLearner(ctx context.Context, classGroupID, learnerID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM roster_entries WHERE class_group_id = $1 AND learner_id = $2)`,
		classGroupID, learnerID).Scan(&exists)
	return exists, err
}

const assignmentCols = `id, school_id, class_group_id, subject_id, teacher_id, title, instructions,
		due_date::text, status, published_at, closed_at, created_at`

func scanAssignment(row pgx.Row) (*Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.SchoolID, &a.ClassGroupID, &a.SubjectID, &a.TeacherID,
		&a.Title, &a.Instructions, &a.DueDate, &a.Status, &a.PublishedAt, &a.ClosedAt, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (r *pgRepo) CreateAssignment(ctx context.Context, a *Assignment) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO assignments (id, school_id, class_group_id, subject_id, teacher_id, title, instructions, due_date)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::date) RETURNING `+assignmentCols,
		a.ID, a.SchoolID, a.ClassGroupID, a.SubjectID, a.TeacherID, a.Title, a.Instructions, a.DueDate).
		Scan(&a.ID, &a.SchoolID, &a.ClassGroupID, &a.SubjectID, &a.TeacherID,
			&a.Title, &a.Instructions, &a.DueDate, &a.Status, &a.PublishedAt, &a.ClosedAt, &a.CreatedAt)
}

func (r *pgRepo) AssignmentByID(ctx context.Context, schoolID, id string) (*Assignment, error) {
	return scanAssignment(r.pool.QueryRow(ctx,
		`SELECT `+assignmentCols+` FROM assignments WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListAssignments(ctx context.Context, schoolID, classGroupID string, status *AssignmentStatus, limit, offset int) ([]*Assignment, int, error) {
	var st any
	if status != nil {
		st = string(*status)
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM assignments
		 WHERE school_id = $1 AND ($2::uuid IS NULL OR class_group_id = $2) AND ($3::text IS NULL OR status = $3::text)`,
		schoolID, nullableUUID(classGroupID), st).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+assignmentCols+` FROM assignments
		 WHERE school_id = $1 AND ($2::uuid IS NULL OR class_group_id = $2) AND ($3::text IS NULL OR status = $3::text)
		 ORDER BY created_at DESC
		 LIMIT $4 OFFSET $5`,
		schoolID, nullableUUID(classGroupID), st, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (r *pgRepo) UpdateAssignmentStatus(ctx context.Context, schoolID, id string, from, to AssignmentStatus) error {
	var ts any
	if to == AssignmentClosed {
		ts = time.Now().UTC()
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE assignments SET status = $3, closed_at = $4
		 WHERE id = $1 AND school_id = $2 AND status = $5`,
		id, schoolID, string(to), ts, string(from))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrIllegalAssignmentTransition
	}
	return nil
}

const submissionCols = `id, school_id, assignment_id, learner_id, content, status, grade, feedback,
		submitted_at, graded_at, returned_at`

func scanSubmission(row pgx.Row) (*Submission, error) {
	var s Submission
	err := row.Scan(&s.ID, &s.SchoolID, &s.AssignmentID, &s.LearnerID, &s.Content, &s.Status,
		&s.Grade, &s.Feedback, &s.SubmittedAt, &s.GradedAt, &s.ReturnedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// UpsertSubmission inserts or refreshes a submission while it stays in the
// submitted state (one row per assignment+learner). pgx.ErrNoRows here means
// the row exists but is no longer 'submitted' — the service translates that
// to ErrAlreadyGraded (409) instead of an unmapped 500 (issue #48).
func (r *pgRepo) UpsertSubmission(ctx context.Context, sub *Submission) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO assignment_submissions (id, school_id, assignment_id, learner_id, content)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (assignment_id, learner_id) DO UPDATE SET
			content = EXCLUDED.content,
			submitted_at = now()
		 WHERE assignment_submissions.status = 'submitted'
		 RETURNING `+submissionCols,
		uuid.NewString(), sub.SchoolID, sub.AssignmentID, sub.LearnerID, sub.Content).
		Scan(&sub.ID, &sub.SchoolID, &sub.AssignmentID, &sub.LearnerID, &sub.Content, &sub.Status,
			&sub.Grade, &sub.Feedback, &sub.SubmittedAt, &sub.GradedAt, &sub.ReturnedAt)
}

// ClassGroupInSchool reports whether the class belongs to the acting school
// (cross-tenant reference guard, issue #48).
func (r *pgRepo) ClassGroupInSchool(ctx context.Context, schoolID, classGroupID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM class_groups WHERE id = $1 AND school_id = $2)`,
		classGroupID, schoolID).Scan(&ok)
	return ok, err
}

// SubjectInSchool reports whether the subject belongs to the acting school
// (cross-tenant reference guard, issue #48).
func (r *pgRepo) SubjectInSchool(ctx context.Context, schoolID, subjectID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM subjects WHERE id = $1 AND school_id = $2)`,
		subjectID, schoolID).Scan(&ok)
	return ok, err
}

func (r *pgRepo) Submission(ctx context.Context, schoolID, assignmentID, learnerID string) (*Submission, error) {
	return scanSubmission(r.pool.QueryRow(ctx,
		`SELECT `+submissionCols+` FROM assignment_submissions
		 WHERE school_id = $1 AND assignment_id = $2 AND learner_id = $3`,
		schoolID, assignmentID, learnerID))
}

func (r *pgRepo) SubmissionsForAssignment(ctx context.Context, schoolID, assignmentID string) ([]*Submission, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+submissionCols+` FROM assignment_submissions
		 WHERE school_id = $1 AND assignment_id = $2
		 ORDER BY submitted_at`, schoolID, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Submission{}
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *pgRepo) GradeSubmission(ctx context.Context, schoolID, assignmentID, learnerID, grade, feedback string) (*Submission, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE assignment_submissions
		 SET status = 'graded', grade = $4, feedback = $5, graded_at = now()
		 WHERE school_id = $1 AND assignment_id = $2 AND learner_id = $3 AND status = 'submitted'
		 RETURNING `+submissionCols,
		schoolID, assignmentID, learnerID, grade, feedback)
	return scanSubmission(row)
}

func (r *pgRepo) ReturnSubmission(ctx context.Context, schoolID, assignmentID, learnerID string) (*Submission, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE assignment_submissions
		 SET status = 'returned', returned_at = now()
		 WHERE school_id = $1 AND assignment_id = $2 AND learner_id = $3 AND status = 'graded'
		 RETURNING `+submissionCols,
		schoolID, assignmentID, learnerID)
	return scanSubmission(row)
}

func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
