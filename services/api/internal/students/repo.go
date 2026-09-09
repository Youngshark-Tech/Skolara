package students

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the students repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

var ErrNotFound = errors.New("students: not found")

// statusList renders a compile-time state slice as a SQL literal list. Values
// come from the package's named constants only — never user input.
func statusList(states []EnrollmentStatus) string {
	quoted := make([]string, len(states))
	for i, s := range states {
		quoted[i] = "'" + string(s) + "'"
	}
	return strings.Join(quoted, ",")
}

// --- learners ---------------------------------------------------------------

const learnerCols = `id, first_name, last_name, middle_name, date_of_birth, gender, external_id, created_at`

func scanLearner(row pgx.Row) (*Learner, error) {
	var l Learner
	err := row.Scan(&l.ID, &l.FirstName, &l.LastName, &l.MiddleName, &l.DateOfBirth, &l.Gender, &l.ExternalID, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &l, err
}

func (r *pgRepo) CreateLearner(ctx context.Context, l *Learner) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO learners (id, first_name, last_name, middle_name, date_of_birth, gender, external_id)
                 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+learnerCols,
		l.ID, l.FirstName, l.LastName, l.MiddleName, l.DateOfBirth, l.Gender, l.ExternalID).
		Scan(&l.ID, &l.FirstName, &l.LastName, &l.MiddleName, &l.DateOfBirth, &l.Gender, &l.ExternalID, &l.CreatedAt)
}

func (r *pgRepo) LearnerByID(ctx context.Context, id string) (*Learner, error) {
	return scanLearner(r.pool.QueryRow(ctx, `SELECT `+learnerCols+` FROM learners WHERE id = $1`, id))
}

// LearnerInSchool resolves a learner only through an enrollment at the school
// — the tenant visibility boundary for learner records.
func (r *pgRepo) LearnerInSchool(ctx context.Context, schoolID, learnerID string) (*Learner, error) {
	return scanLearner(r.pool.QueryRow(ctx,
		`SELECT `+prefixedCols("l", learnerCols)+` FROM learners l
                 JOIN enrollments e ON e.learner_id = l.id
                 WHERE l.id = $1 AND e.school_id = $2
                 LIMIT 1`, learnerID, schoolID))
}

// ListLearners lists (and optionally name-searches) learners enrolled at a
// school. Learners are global; the school join is what scopes the listing.
func (r *pgRepo) ListLearners(ctx context.Context, schoolID, query string, limit, offset int) ([]*Learner, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(DISTINCT l.id) FROM learners l
                 JOIN enrollments e ON e.learner_id = l.id
                 WHERE e.school_id = $1
                   AND ($2::text = '' OR l.first_name ILIKE '%'||$2||'%' OR l.last_name ILIKE '%'||$2||'%')`,
		schoolID, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT `+prefixedCols("l", learnerCols)+` FROM learners l
                 JOIN enrollments e ON e.learner_id = l.id
                 WHERE e.school_id = $1
                   AND ($2::text = '' OR l.first_name ILIKE '%'||$2||'%' OR l.last_name ILIKE '%'||$2||'%')
                 ORDER BY l.last_name, l.first_name
                 LIMIT $3 OFFSET $4`, schoolID, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Learner{}
	for rows.Next() {
		l, err := scanLearner(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

// --- guardians and links ----------------------------------------------------

const guardianCols = `id, first_name, last_name, phone, email, created_at`

func scanGuardian(row pgx.Row) (*Guardian, error) {
	var g Guardian
	err := row.Scan(&g.ID, &g.FirstName, &g.LastName, &g.Phone, &g.Email, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

func (r *pgRepo) CreateGuardian(ctx context.Context, g *Guardian) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO guardians (id, first_name, last_name, phone, email)
                 VALUES ($1,$2,$3,$4,$5) RETURNING `+guardianCols,
		g.ID, g.FirstName, g.LastName, g.Phone, g.Email).
		Scan(&g.ID, &g.FirstName, &g.LastName, &g.Phone, &g.Email, &g.CreatedAt)
}

func (r *pgRepo) GuardianByID(ctx context.Context, id string) (*Guardian, error) {
	return scanGuardian(r.pool.QueryRow(ctx, `SELECT `+guardianCols+` FROM guardians WHERE id = $1`, id))
}

func (r *pgRepo) CreateGuardianLink(ctx context.Context, link *GuardianLink) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO guardian_learner_links (guardian_id, learner_id, relationship, is_primary, can_view_financials, can_view_academics)
                 VALUES ($1,$2,$3,$4,$5,$6)
                 RETURNING guardian_id, learner_id, relationship, is_primary, can_view_financials, can_view_academics`,
		link.GuardianID, link.LearnerID, link.Relationship, link.IsPrimary, link.CanViewFinancials, link.CanViewAcademics).
		Scan(&link.GuardianID, &link.LearnerID, &link.Relationship, &link.IsPrimary, &link.CanViewFinancials, &link.CanViewAcademics)
}

// GuardianLinksForLearner returns the learner's guardian links joined with
// guardian identity, primary contact first.
func (r *pgRepo) GuardianLinksForLearner(ctx context.Context, learnerID string) ([]*GuardianLinkView, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT l.guardian_id, l.learner_id, l.relationship, l.is_primary, l.can_view_financials, l.can_view_academics,
                        g.id, g.first_name, g.last_name, g.phone, g.email, g.created_at
                 FROM guardian_learner_links l
                 JOIN guardians g ON g.id = l.guardian_id
                 WHERE l.learner_id = $1
                 ORDER BY l.is_primary DESC, g.last_name, g.first_name`, learnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*GuardianLinkView{}
	for rows.Next() {
		v := &GuardianLinkView{}
		if err := rows.Scan(&v.Link.GuardianID, &v.Link.LearnerID, &v.Link.Relationship, &v.Link.IsPrimary,
			&v.Link.CanViewFinancials, &v.Link.CanViewAcademics,
			&v.Guardian.ID, &v.Guardian.FirstName, &v.Guardian.LastName, &v.Guardian.Phone, &v.Guardian.Email, &v.Guardian.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- enrollments ------------------------------------------------------------

const enrollmentCols = `id, school_id, learner_id, class_group_id, academic_year_id, status, started_at, ended_at, created_at`

func scanEnrollment(row pgx.Row) (*Enrollment, error) {
	var e Enrollment
	err := row.Scan(&e.ID, &e.SchoolID, &e.LearnerID, &e.ClassGroupID, &e.AcademicYearID, &e.Status, &e.StartedAt, &e.EndedAt, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

func (r *pgRepo) CreateEnrollment(ctx context.Context, e *Enrollment) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO enrollments (id, school_id, learner_id, class_group_id, academic_year_id, status, started_at)
                 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+enrollmentCols,
		e.ID, e.SchoolID, e.LearnerID, e.ClassGroupID, e.AcademicYearID, e.Status, e.StartedAt).
		Scan(&e.ID, &e.SchoolID, &e.LearnerID, &e.ClassGroupID, &e.AcademicYearID, &e.Status, &e.StartedAt, &e.EndedAt, &e.CreatedAt)
}

// EnrollmentByIDInSchool enforces tenant scoping in the WHERE clause: an
// enrollment of another school is indistinguishable from a missing one.
func (r *pgRepo) EnrollmentByIDInSchool(ctx context.Context, schoolID, id string) (*Enrollment, error) {
	return scanEnrollment(r.pool.QueryRow(ctx,
		`SELECT `+enrollmentCols+` FROM enrollments WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListEnrollments(ctx context.Context, schoolID string, status *EnrollmentStatus, limit, offset int) ([]*Enrollment, int, error) {
	var s any
	if status != nil {
		s = string(*status)
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM enrollments
                 WHERE school_id = $1 AND ($2::text IS NULL OR status = $2::text)`, schoolID, s).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+enrollmentCols+` FROM enrollments
                 WHERE school_id = $1 AND ($2::text IS NULL OR status = $2::text)
                 ORDER BY created_at DESC`, schoolID, s)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Enrollment{}
	for rows.Next() {
		e, err := scanEnrollment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// HasOpenEnrollment reports whether the learner already holds a non-terminal
// enrollment at the school (blocks duplicate concurrent enrollment).
func (r *pgRepo) HasOpenEnrollment(ctx context.Context, schoolID, learnerID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
                        SELECT 1 FROM enrollments
                        WHERE school_id = $1 AND learner_id = $2
                          AND status IN (`+statusList(openEnrollmentStates)+`))`,
		schoolID, learnerID).Scan(&exists)
	return exists, err
}

// UpdateEnrollmentStatus persists a transition guarded by the expected source
// status (optimistic concurrency: a racing writer makes the UPDATE affect 0
// rows, which surfaces as ErrIllegalTransition).
func (r *pgRepo) UpdateEnrollmentStatus(ctx context.Context, id string, from, to EnrollmentStatus, endedAt *time.Time) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE enrollments SET status = $2, ended_at = $3 WHERE id = $1 AND status = $4`,
		id, string(to), endedAt, string(from))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w: enrollment %s no longer in status %s", ErrIllegalTransition, id, from)
	}
	return nil
}

// prefixedCols qualifies an unqualified column list with a table alias.
func prefixedCols(alias, cols string) string {
	parts := strings.Split(cols, ", ")
	for i, c := range parts {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}
