package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

// pgRepo implements Repo on PostgreSQL.
type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the identity repository.
func NewRepo(pool *postgres.Pool) Repo {
	return &pgRepo{pool: pool}
}

var ErrNotFound = errors.New("identity: not found")

func scanUser(row pgx.Row) (*User, error) {
	var u User
	var status string
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &status,
		&u.FailedLoginAttempts, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Status = UserStatus(status)
	return &u, nil
}

const userCols = `id, email, name, password_hash, status, failed_login_attempts, locked_until, created_at, updated_at`

func (r *pgRepo) CreateUser(ctx context.Context, u *User) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1,$2,$3,$4)
		 RETURNING `+userCols,
		u.ID, strings.ToLower(u.Email), u.Name, u.PasswordHash)
	got, err := scanUser(row)
	if err != nil {
		if strings.Contains(err.Error(), "users_email_key") {
			return fmt.Errorf("identity: email already registered")
		}
		return err
	}
	*u = *got
	return nil
}

func (r *pgRepo) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (r *pgRepo) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(email) = $1`, strings.ToLower(email)))
}

func (r *pgRepo) UpdateUserStatus(ctx context.Context, id string, status UserStatus, failedAttempts int, lockedUntil *time.Time) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE users SET status=$2, failed_login_attempts=$3, locked_until=$4, updated_at=now() WHERE id=$1`,
		id, string(status), failedAttempts, lockedUntil)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgRepo) ListUsers(ctx context.Context, limit, offset int) ([]*User, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+userCols+` FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *pgRepo) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *pgRepo) PermissionsForUser(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT p.name
		 FROM user_roles ur
		 JOIN role_permissions rp ON rp.role_id = ur.role_id
		 JOIN permissions p ON p.id = rp.permission_id
		 WHERE ur.user_id = $1`, userID)
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

// RoleExists reports whether the named role exists in the RBAC seed.
func (r *pgRepo) RoleExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM roles WHERE name = $1)`, name).Scan(&exists)
	return exists, err
}

func (r *pgRepo) AssignRole(ctx context.Context, userID, roleName string) error {
	ct, err := r.pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, (SELECT id FROM roles WHERE name=$2))
		 ON CONFLICT DO NOTHING`, userID, roleName)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return nil // idempotent assignment
	}
	return nil
}

func (r *pgRepo) CreateRefreshToken(ctx context.Context, rt *RefreshToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, family_id, expires_at, created_ip)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		rt.ID, rt.UserID, rt.TokenHash, rt.FamilyID, rt.ExpiresAt, rt.CreatedIP)
	return err
}

var _ = json.Marshal

func scanRefresh(row pgx.Row) (*RefreshToken, error) {
	var rt RefreshToken
	err := row.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.ExpiresAt, &rt.UsedAt, &rt.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &rt, err
}

func (r *pgRepo) RefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	return scanRefresh(r.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, family_id, expires_at, used_at, revoked_at
		 FROM refresh_tokens WHERE token_hash = $1`, hash))
}

func (r *pgRepo) RevokeRefreshFamily(ctx context.Context, familyID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, familyID)
	return err
}

func (r *pgRepo) RevokeRefreshToken(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

func (r *pgRepo) MarkRefreshTokenUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL`, id)
	return err
}

func (r *pgRepo) RevokeAllUserRefreshTokens(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (r *pgRepo) RecordAudit(ctx context.Context, a AuditEntry) error {
	before, err := json.Marshal(a.Before)
	if err != nil {
		before = nil
	}
	after, err := json.Marshal(a.After)
	if err != nil {
		after = nil
	}
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		detail = nil
	}
	var actorID any
	if a.ActorID != "" {
		actorID = a.ActorID
	}
	var schoolID any
	if a.SchoolID != "" {
		schoolID = a.SchoolID
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_logs (actor_id, action, resource_type, resource_id, school_id, before_state, after_state, request_id, correlation_id, detail)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		actorID, a.Action, a.ResourceType, a.ResourceID, schoolID,
		nullableJSON(before), nullableJSON(after), a.RequestID, a.CorrelationID, nullableJSON(detail))
	return err
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
