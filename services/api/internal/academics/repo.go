package academics

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the academics repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

// --- academic years ---------------------------------------------------------

const yearCols = `id, school_id, name, start_date::text, end_date::text, status, created_at`

func scanYear(row pgx.Row) (*AcademicYear, error) {
	var y AcademicYear
	err := row.Scan(&y.ID, &y.SchoolID, &y.Name, &y.StartDate, &y.EndDate, &y.Status, &y.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &y, err
}

func (r *pgRepo) CreateAcademicYear(ctx context.Context, schoolID string, y *AcademicYear) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO academic_years (id, school_id, name, start_date, end_date)
		 VALUES ($1,$2,$3,$4::date,$5::date) RETURNING `+yearCols,
		y.ID, schoolID, y.Name, y.StartDate, y.EndDate).
		Scan(&y.ID, &y.SchoolID, &y.Name, &y.StartDate, &y.EndDate, &y.Status, &y.CreatedAt)
}

func (r *pgRepo) AcademicYearByID(ctx context.Context, schoolID, id string) (*AcademicYear, error) {
	return scanYear(r.pool.QueryRow(ctx,
		`SELECT `+yearCols+` FROM academic_years WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListAcademicYears(ctx context.Context, schoolID string) ([]*AcademicYear, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+yearCols+` FROM academic_years WHERE school_id = $1 ORDER BY start_date DESC`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AcademicYear{}
	for rows.Next() {
		y, err := scanYear(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

// --- terms ------------------------------------------------------------------

const termCols = `id, school_id, academic_year_id, name, start_date::text, end_date::text, created_at`

func scanTerm(row pgx.Row) (*Term, error) {
	var t Term
	err := row.Scan(&t.ID, &t.SchoolID, &t.AcademicYearID, &t.Name, &t.StartDate, &t.EndDate, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (r *pgRepo) CreateTerm(ctx context.Context, schoolID string, t *Term) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO terms (id, school_id, academic_year_id, name, start_date, end_date)
		 VALUES ($1,$2,$3,$4,$5::date,$6::date) RETURNING `+termCols,
		t.ID, schoolID, t.AcademicYearID, t.Name, t.StartDate, t.EndDate).
		Scan(&t.ID, &t.SchoolID, &t.AcademicYearID, &t.Name, &t.StartDate, &t.EndDate, &t.CreatedAt)
}

// TermsForYear returns all terms of a year; overlap policy is a service-layer
// invariant (kept in one place with clear domain errors instead of exclusion
// constraints requiring the btree_gist extension).
func (r *pgRepo) TermsForYear(ctx context.Context, schoolID, yearID string) ([]*Term, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+termCols+` FROM terms WHERE school_id = $1 AND academic_year_id = $2 ORDER BY start_date`,
		schoolID, yearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Term{}
	for rows.Next() {
		t, err := scanTerm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- subjects ---------------------------------------------------------------

const subjectCols = `id, school_id, code, name`

func scanSubject(row pgx.Row) (*Subject, error) {
	var s Subject
	err := row.Scan(&s.ID, &s.SchoolID, &s.Code, &s.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *pgRepo) CreateSubject(ctx context.Context, schoolID string, s *Subject) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO subjects (id, school_id, code, name) VALUES ($1,$2,$3,$4) RETURNING `+subjectCols,
		s.ID, schoolID, s.Code, s.Name).
		Scan(&s.ID, &s.SchoolID, &s.Code, &s.Name)
}

func (r *pgRepo) SubjectByID(ctx context.Context, schoolID, id string) (*Subject, error) {
	return scanSubject(r.pool.QueryRow(ctx,
		`SELECT `+subjectCols+` FROM subjects WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListSubjects(ctx context.Context, schoolID string) ([]*Subject, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+subjectCols+` FROM subjects WHERE school_id = $1 ORDER BY name`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Subject{}
	for rows.Next() {
		s, err := scanSubject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- class groups -----------------------------------------------------------

const classCols = `id, school_id, academic_year_id, name`

func scanClass(row pgx.Row) (*ClassGroup, error) {
	var c ClassGroup
	err := row.Scan(&c.ID, &c.SchoolID, &c.AcademicYearID, &c.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *pgRepo) CreateClassGroup(ctx context.Context, schoolID string, c *ClassGroup) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO class_groups (id, school_id, academic_year_id, name) VALUES ($1,$2,$3,$4) RETURNING `+classCols,
		c.ID, schoolID, c.AcademicYearID, c.Name).
		Scan(&c.ID, &c.SchoolID, &c.AcademicYearID, &c.Name)
}

func (r *pgRepo) ClassGroupByID(ctx context.Context, schoolID, id string) (*ClassGroup, error) {
	return scanClass(r.pool.QueryRow(ctx,
		`SELECT `+classCols+` FROM class_groups WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListClassGroups(ctx context.Context, schoolID, yearID string) ([]*ClassGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+classCols+` FROM class_groups
		 WHERE school_id = $1 AND ($2::uuid IS NULL OR academic_year_id = $2)
		 ORDER BY name`, schoolID, nullableUUID(yearID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ClassGroup{}
	for rows.Next() {
		c, err := scanClass(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- roster -----------------------------------------------------------------

// AddRosterEntries bulk-inserts seats idempotently (ON CONFLICT DO NOTHING)
// inside a single transaction — partial failures leave no half-written roster.
//
// Seats are ENROLLMENT-SCOPED (issue #42): a learner is only seatable when an
// open enrollment exists at the acting school (same non-terminal state list
// as the students domain's openEnrollmentStates / HasOpenEnrollment guard —
// keep both in sync if enrollment states ever change). Learner identities are
// global, but visibility is tenant-guarded everywhere; seat any global
// learner here would leak PII across tenants via GET /classes/{id}/roster.
// Learners already seated are skipped (idempotent replay). Unenrolled (or
// unknown) learner ids are reported to the caller as rejections.
func (r *pgRepo) AddRosterEntries(ctx context.Context, schoolID, classGroupID string, learnerIDs []string) ([]string, error) {
	var rejected []string
	err := r.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		rejected = rejected[:0]
		for _, lid := range learnerIDs {
			// Idempotent replay: already seated -> skip silently.
			var seated bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM roster_entries WHERE class_group_id = $1 AND learner_id = $2)`,
				classGroupID, lid).Scan(&seated); err != nil {
				return fmt.Errorf("check roster entry: %w", err)
			}
			if seated {
				continue
			}
			tag, err := tx.Exec(ctx,
				`INSERT INTO roster_entries (class_group_id, learner_id)
				 SELECT $1, $2
				 WHERE EXISTS (SELECT 1 FROM class_groups WHERE id = $1 AND school_id = $3)
				   AND EXISTS (SELECT 1 FROM enrollments e
				               WHERE e.learner_id = $2 AND e.school_id = $3
				                 AND e.status IN ('applicant','admitted','active','suspended','transfer_pending'))
				 ON CONFLICT (class_group_id, learner_id) DO NOTHING`,
				classGroupID, lid, schoolID)
			if err != nil {
				return fmt.Errorf("insert roster entry: %w", err)
			}
			if tag.RowsAffected() == 0 {
				rejected = append(rejected, lid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rejected, nil
}

func (r *pgRepo) RosterForClass(ctx context.Context, schoolID, classGroupID string) ([]*RosterEntryView, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT l.id, l.first_name, l.last_name, l.external_id
		 FROM roster_entries re
		 JOIN class_groups c ON c.id = re.class_group_id
		 JOIN learners l ON l.id = re.learner_id
		 WHERE re.class_group_id = $1 AND c.school_id = $2
		 ORDER BY l.last_name, l.first_name`, classGroupID, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*RosterEntryView{}
	for rows.Next() {
		v := &RosterEntryView{}
		if err := rows.Scan(&v.LearnerID, &v.FirstName, &v.LastName, &v.ExternalID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- teaching assignments ---------------------------------------------------

func (r *pgRepo) CreateTeachingAssignment(ctx context.Context, schoolID string, a *TeachingAssignment) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO teaching_assignments (id, school_id, class_group_id, subject_id, teacher_id)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		a.ID, schoolID, a.ClassGroupID, a.SubjectID, a.TeacherID).Scan(&a.ID)
}

func (r *pgRepo) AssignmentsForClass(ctx context.Context, schoolID, classGroupID string) ([]*TeachingAssignmentView, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ta.id, ta.school_id, ta.class_group_id, ta.subject_id, ta.teacher_id,
		        u.id, u.name, u.email,
		        s.id, s.code, s.name
		 FROM teaching_assignments ta
		 JOIN users u ON u.id = ta.teacher_id
		 JOIN subjects s ON s.id = ta.subject_id
		 WHERE ta.class_group_id = $1 AND ta.school_id = $2
		 ORDER BY s.name`, classGroupID, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TeachingAssignmentView{}
	for rows.Next() {
		v := &TeachingAssignmentView{}
		if err := rows.Scan(&v.Assignment.ID, &v.Assignment.SchoolID, &v.Assignment.ClassGroupID,
			&v.Assignment.SubjectID, &v.Assignment.TeacherID,
			&v.Teacher.ID, &v.Teacher.Name, &v.Teacher.Email,
			&v.Subject.ID, &v.Subject.Code, &v.Subject.Name); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- helpers ----------------------------------------------------------------

func nullableUUID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}
