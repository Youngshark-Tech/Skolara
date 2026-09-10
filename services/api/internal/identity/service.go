package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Permission constants used across route registration.
const (
	PermUserRead         = "user.read"
	PermUserManage       = "user.manage"
	PermAuditRead        = "audit.read"
	PermSchoolRead       = "school.read"
	PermSchoolManage     = "school.manage"
	PermAcadRead         = "academics.read"
	PermAcadManage       = "academics.manage"
	PermAttendanceRead   = "attendance.read"
	PermAttendanceRecord = "attendance.record"
	PermAssignmentRead   = "assignment.read"
	PermAssignmentManage = "assignment.manage"
	PermFinanceRead      = "finance.read"
	PermFinanceManage    = "finance.manage"
	PermStudentRead      = "student.read"
	PermStudentManage    = "student.manage"
)

// RolePlatformAdmin bypasses scope checks (platform-level operator).
const RolePlatformAdmin = "platform_admin"

// AuthService implements authentication and user administration.
type AuthService struct {
	repo Repo
	jwt  *JWTManager
	now  func() time.Time
}

func NewAuthService(repo Repo, jwt *JWTManager) *AuthService {
	return &AuthService{repo: repo, jwt: jwt, now: time.Now}
}

var (
	ErrBadCredentials  = errors.New("identity: invalid email or password")
	ErrAccountLocked   = errors.New("identity: account locked")
	ErrAccountDisabled = errors.New("identity: account disabled")
	ErrTokenReuse      = errors.New("identity: refresh token reuse detected")
	ErrEmailTaken      = errors.New("identity: email already registered")

	// ErrRefreshAlreadyUsed is returned by the repo when the atomic
	// mark-used UPDATE matched no rows (token already consumed by a
	// concurrent refresh). Callers treat it exactly like the sequential
	// reuse path: revoke the family, return ErrTokenReuse (issue #41).
	ErrRefreshAlreadyUsed = errors.New("identity: refresh token already used")
)

// Login authenticates, applying lockout policy, and returns an access token
// plus a new refresh token. The password is verified BEFORE any account-state
// error is revealed: unknown emails, wrong passwords, disabled and locked
// accounts all receive the same 401-class ErrBadCredentials until the
// password proves correct — only then is the actionable state (disabled /
// locked) disclosed to the legitimate credential holder (issue #43).
func (s *AuthService) Login(ctx context.Context, email, password, ip string) (access string, refresh string, err error) {
	u, err := s.repo.UserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// Burn comparable time to avoid user-enumeration timing signal.
		_ = VerifyPassword(
			"$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			password)
		return "", "", ErrBadCredentials
	}
	if err != nil {
		return "", "", err
	}

	if !VerifyPassword(u.PasswordHash, password) {
		// Only ACTIVE accounts accumulate failure counts (and can be driven
		// into lockout by failures). The increment is atomic server-side so
		// concurrent failed logins cannot under-count the counter (#43).
		if u.Status == StatusActive {
			if err := s.repo.RegisterFailedLogin(ctx, u.ID, MaxFailedLogins, s.now().Add(LockoutDuration)); err != nil {
				return "", "", err
			}
		}
		return "", "", ErrBadCredentials
	}

	// Password is correct — actionable account state is now safe to reveal.
	if u.Status == StatusDisabled {
		return "", "", ErrAccountDisabled
	}
	if u.Status == StatusLocked {
		// Auto-unlock when the lockout window has elapsed.
		if u.LockedUntil != nil && s.now().After(*u.LockedUntil) {
			if err := s.repo.UpdateUserStatus(ctx, u.ID, StatusActive, 0, nil); err != nil {
				return "", "", err
			}
			u.Status = StatusActive
			u.FailedLoginAttempts = 0
		} else {
			return "", "", ErrAccountLocked
		}
	}

	if err := s.repo.UpdateUserStatus(ctx, u.ID, StatusActive, 0, nil); err != nil {
		return "", "", err
	}
	roles, err := s.repo.RolesForUser(ctx, u.ID)
	if err != nil {
		return "", "", err
	}
	access, err = s.jwt.Issue(SessionClaims{UserID: u.ID, Email: u.Email, Roles: roles})
	if err != nil {
		return "", "", err
	}
	refresh, err = s.issueRefresh(ctx, u.ID, ip, "")
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (s *AuthService) locked(u *User) bool {
	return u.Status == StatusLocked
}

// Refresh rotates a refresh token. Reuse of an already-used token revokes the
// whole family (ADR-007).
func (s *AuthService) Refresh(ctx context.Context, rawToken, ip string) (string, string, error) {
	rt, err := s.repo.RefreshTokenByHash(ctx, HashToken(rawToken))
	if errors.Is(err, ErrNotFound) {
		return "", "", ErrBadCredentials
	}
	if err != nil {
		return "", "", err
	}
	if rt.RevokedAt != nil || s.now().After(rt.ExpiresAt) {
		return "", "", ErrBadCredentials
	}
	if rt.UsedAt != nil {
		// Reuse detected — kill the chain.
		_ = s.repo.RevokeRefreshFamily(ctx, rt.FamilyID)
		return "", "", ErrTokenReuse
	}

	u, err := s.repo.UserByID(ctx, rt.UserID)
	if err != nil {
		return "", "", err
	}
	if u.Status != StatusActive {
		return "", "", ErrAccountDisabled
	}

	// Mark used (not revoked) so a replay is DETECTABLE as reuse —
	// which then revokes the whole family (ADR-007). The UPDATE is
	// guarded by `used_at IS NULL`; racing it returns
	// ErrRefreshAlreadyUsed so concurrent reuse is caught too (#41).
	if err := s.repo.MarkRefreshTokenUsed(ctx, rt.ID); err != nil {
		if errors.Is(err, ErrRefreshAlreadyUsed) {
			_ = s.repo.RevokeRefreshFamily(ctx, rt.FamilyID)
			return "", "", ErrTokenReuse
		}
		return "", "", err
	}
	roles, err := s.repo.RolesForUser(ctx, u.ID)
	if err != nil {
		return "", "", err
	}
	access, err := s.jwt.Issue(SessionClaims{UserID: u.ID, Email: u.Email, Roles: roles})
	if err != nil {
		return "", "", err
	}
	refresh, err := s.issueRefresh(ctx, u.ID, ip, rt.FamilyID)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (s *AuthService) issueRefresh(ctx context.Context, userID, ip, familyID string) (string, error) {
	raw, err := NewOpaqueToken()
	if err != nil {
		return "", err
	}
	if familyID == "" {
		familyID = NewID()
	}
	rt := &RefreshToken{
		ID:        NewID(),
		UserID:    userID,
		TokenHash: HashToken(raw),
		FamilyID:  familyID,
		ExpiresAt: s.now().Add(RefreshTokenExpiry),
		CreatedIP: ip,
	}
	if err := s.repo.CreateRefreshToken(ctx, rt); err != nil {
		return "", err
	}
	return raw, nil
}

// Logout revokes a single refresh token.
func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	rt, err := s.repo.RefreshTokenByHash(ctx, HashToken(rawToken))
	if errors.Is(err, ErrNotFound) {
		return nil // idempotent logout
	}
	if err != nil {
		return err
	}
	return s.repo.RevokeRefreshToken(ctx, rt.ID)
}

// CreateUser provisions a user (admin flow). First platform admin bootstraps
// via the same path.
func (s *AuthService) CreateUser(ctx context.Context, email, name, password, roleName string) (*User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: invalid email", ErrValidation)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: name required", ErrValidation)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	u := &User{ID: NewID(), Email: email, Name: name, PasswordHash: hash, Status: StatusActive}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	if roleName != "" {
		if err := s.repo.AssignRole(ctx, u.ID, roleName); err != nil {
			return nil, err
		}
	}
	return u, nil
}

var ErrValidation = errors.New("identity: validation failed")

// PermissionsFor returns the permission set for a user by resolving roles.
func (s *AuthService) PermissionsFor(ctx context.Context, userID string) (map[string]bool, error) {
	return s.repo.PermissionsForUser(ctx, userID)
}

// AssignRole grants a named role to a user. Unknown roles are rejected with
// ErrValidation (400) instead of a NULL not-null violation (500).
func (s *AuthService) AssignRole(ctx context.Context, actorID, userID, roleName string) error {
	ok, err := s.repo.RoleExists(ctx, roleName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown role %q", ErrValidation, roleName)
	}
	if err := s.repo.AssignRole(ctx, userID, roleName); err != nil {
		if isFKViolation(err) {
			return fmt.Errorf("%w: user %s does not exist", ErrValidation, userID)
		}
		return err
	}
	return nil
}

func isFKViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}

// GetUser fetches a user by ID.
func (s *AuthService) GetUser(ctx context.Context, id string) (*User, error) {
	return s.repo.UserByID(ctx, id)
}

// SetUserStatus transitions a user between active and disabled (admin
// lifecycle flow). Disabling revokes ALL of the user's refresh tokens so no
// further token exchange succeeds — staff offboarding invariant.
func (s *AuthService) SetUserStatus(ctx context.Context, actorID, userID, status string) (*User, error) {
	switch UserStatus(status) {
	case StatusActive, StatusDisabled:
	default:
		return nil, fmt.Errorf("%w: status must be active or disabled", ErrValidation)
	}
	u, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Status == UserStatus(status) {
		return u, nil // idempotent
	}
	if err := s.repo.UpdateUserStatus(ctx, u.ID, UserStatus(status), u.FailedLoginAttempts, u.LockedUntil); err != nil {
		return nil, err
	}
	if status == string(StatusDisabled) {
		if err := s.repo.RevokeAllUserRefreshTokens(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	_ = s.Audit(ctx, AuditEntry{
		ActorID:      actorID,
		Action:       "user.status_changed",
		ResourceType: "user",
		ResourceID:   u.ID,
		Before:       map[string]any{"status": string(u.Status)},
		After:        map[string]any{"status": status},
	})
	return s.repo.UserByID(ctx, u.ID)
}

// ListUsers returns a page of users.
func (s *AuthService) ListUsers(ctx context.Context, limit, offset int) ([]*User, int, error) {
	return s.repo.ListUsers(ctx, limit, offset)
}

// Audit writes an audit entry with best-effort failure logging.
func (s *AuthService) Audit(ctx context.Context, entry AuditEntry) error {
	return s.repo.RecordAudit(ctx, entry)
}
