package tenancy

import (
	"context"
	"errors"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the tenancy repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

var ErrNotFound = errors.New("tenancy: not found")

func (r *pgRepo) CreateGroup(ctx context.Context, g *EducationGroup) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO education_groups (id, name) VALUES ($1,$2) RETURNING id, name`,
		g.ID, g.Name).Scan(&g.ID, &g.Name)
}

func (r *pgRepo) ListGroups(ctx context.Context) ([]*EducationGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM education_groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EducationGroup
	for rows.Next() {
		g := &EducationGroup{}
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

const schoolCols = `id, code, name, group_id, status, created_at, updated_at`

func scanSchool(row pgx.Row) (*School, error) {
	var s School
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.GroupID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *pgRepo) CreateSchool(ctx context.Context, s *School) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO schools (id, code, name, group_id) VALUES ($1,$2,$3,$4) RETURNING `+schoolCols,
		s.ID, s.Code, s.Name, s.GroupID).Scan(&s.ID, &s.Code, &s.Name, &s.GroupID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
}

func (r *pgRepo) SchoolByID(ctx context.Context, id string) (*School, error) {
	return scanSchool(r.pool.QueryRow(ctx, `SELECT `+schoolCols+` FROM schools WHERE id = $1`, id))
}

func (r *pgRepo) SchoolByCode(ctx context.Context, code string) (*School, error) {
	return scanSchool(r.pool.QueryRow(ctx,
		`SELECT `+schoolCols+` FROM schools WHERE lower(code) = lower($1)`, code))
}

func (r *pgRepo) ListSchools(ctx context.Context, groupID *string) ([]*School, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+schoolCols+` FROM schools WHERE ($1::uuid IS NULL OR group_id = $1) ORDER BY name`,
		groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*School
	for rows.Next() {
		s, err := scanSchool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *pgRepo) CreateCampus(ctx context.Context, c *Campus) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO campuses (id, school_id, code, name, location) VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, school_id, code, name, location`,
		c.ID, c.SchoolID, c.Code, c.Name, c.Location).Scan(&c.ID, &c.SchoolID, &c.Code, &c.Name, &c.Location)
}

func (r *pgRepo) ListCampuses(ctx context.Context, schoolID string) ([]*Campus, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, school_id, code, name, location FROM campuses WHERE school_id = $1 ORDER BY name`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Campus
	for rows.Next() {
		c := &Campus{}
		if err := rows.Scan(&c.ID, &c.SchoolID, &c.Code, &c.Name, &c.Location); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const memberCols = `user_id, school_id, r.name, m.status`

func (r *pgRepo) AddMember(ctx context.Context, m *Membership) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO school_memberships (user_id, school_id, role)
		 VALUES ($1,$2,(SELECT id FROM roles WHERE name=$3))
		 ON CONFLICT (user_id, school_id, role) DO NOTHING`,
		m.UserID, m.SchoolID, m.Role)
	return err
}

func scanMembership(row pgx.Row, into *Membership) error {
	return row.Scan(&into.UserID, &into.SchoolID, &into.Role, &into.Status)
}

func (r *pgRepo) MembershipsForUser(ctx context.Context, userID string) ([]*Membership, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.user_id, m.school_id, r.name, m.status
		 FROM school_memberships m JOIN roles r ON r.id = m.role
		 WHERE m.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Membership
	for rows.Next() {
		m := &Membership{}
		if err := scanMembership(rows, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *pgRepo) MembershipStatus(ctx context.Context, userID, schoolID string) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT m.status FROM school_memberships m
		 WHERE m.user_id = $1 AND m.school_id = $2 LIMIT 1`, userID, schoolID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}

// PermissionsFromMemberships unions permission names granted through ACTIVE
// school membership roles (school-scoped RBAC).
func (r *pgRepo) PermissionsFromMemberships(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT p.name
		 FROM school_memberships m
		 JOIN role_permissions rp ON rp.role_id = m.role
		 JOIN permissions p ON p.id = rp.permission_id
		 WHERE m.user_id = $1 AND m.status = 'active'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

func (r *pgRepo) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = $1 AND r.name = 'platform_admin')`, userID).Scan(&exists)
	return exists, err
}

func (r *pgRepo) ListMembers(ctx context.Context, schoolID string) ([]*Membership, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.user_id, m.school_id, r.name, m.status
		 FROM school_memberships m JOIN roles r ON r.id = m.role
		 WHERE m.school_id = $1 ORDER BY m.created_at`, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Membership
	for rows.Next() {
		m := &Membership{}
		if err := scanMembership(rows, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
