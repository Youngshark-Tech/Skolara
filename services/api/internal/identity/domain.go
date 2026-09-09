// Package identity is the bounded context owning user accounts, credentials,
// sessions, RBAC, and audit logging (ADR-007). It depends only on platform
// packages — never on other domains.
package identity

import (
	"context"
	"time"
)

// UserStatus enumerates account states.
type UserStatus string

const (
	StatusActive   UserStatus = "active"
	StatusDisabled UserStatus = "disabled"
	StatusLocked   UserStatus = "locked"
)

// User is a platform user account. School binding lives in tenancy memberships.
type User struct {
	ID                  string
	Email               string
	Name                string
	PasswordHash        string
	Status              UserStatus
	FailedLoginAttempts int
	LockedUntil         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Role is a named permission bundle.
type Role struct {
	ID   int
	Name string
}

// SessionClaims is what a valid access token asserts.
type SessionClaims struct {
	UserID    string   `json:"sub"`
	Email     string   `json:"email,omitempty"`
	SchoolID  string   `json:"school,omitempty"` // active school context ("" if none)
	Roles     []string `json:"roles"`
	PermVer   int      `json:"pv"` // permission matrix version for cache busting
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
}

// Repo is the persistence port of the identity context.
type Repo interface {
	CreateUser(ctx context.Context, u *User) error
	UserByID(ctx context.Context, id string) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	UpdateUserStatus(ctx context.Context, id string, status UserStatus, failedAttempts int, lockedUntil *time.Time) error
	ListUsers(ctx context.Context, limit, offset int) ([]*User, int, error)

	RolesForUser(ctx context.Context, userID string) ([]string, error)
	PermissionsForUser(ctx context.Context, userID string) (map[string]bool, error)
	AssignRole(ctx context.Context, userID string, roleName string) error

	CreateRefreshToken(ctx context.Context, rt *RefreshToken) error
	RefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error)
	RevokeRefreshFamily(ctx context.Context, familyID string) error
	RevokeRefreshToken(ctx context.Context, id string) error
	RevokeAllUserRefreshTokens(ctx context.Context, userID string) error
	MarkRefreshTokenUsed(ctx context.Context, id string) error

	RecordAudit(ctx context.Context, a AuditEntry) error
}

// RefreshToken is an opaque, hashed refresh credential with rotation metadata.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	FamilyID  string
	ExpiresAt time.Time
	CreatedIP string
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// AuditEntry is one auditable action record (§43).
type AuditEntry struct {
	ActorID       string
	Action        string // "auth.login", "user.created", ...
	ResourceType  string
	ResourceID    string
	SchoolID      string
	Before        map[string]any
	After         map[string]any
	RequestID     string
	CorrelationID string
	Detail        map[string]any
}
