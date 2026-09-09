// Package tenancy is the bounded context owning the tenant hierarchy:
// education groups → schools → campuses — and school memberships with
// school-scoped roles (ADR-006). School is the tenant root.
package tenancy

import (
	"context"
	"time"
)

// School is the tenant root for all school-scoped data.
type School struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	GroupID   *string   `json:"groupId,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Campus is a physical site of a school.
type Campus struct {
	ID       string `json:"id"`
	SchoolID string `json:"schoolId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// EducationGroup is a network of schools.
type EducationGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Membership binds a user to a school with a school-scoped role.
type Membership struct {
	UserID   string `json:"userId"`
	SchoolID string `json:"schoolId"`
	Role     string `json:"role"`
	Status   string `json:"status"` // active | suspended
}

// Repo is the persistence port.
type Repo interface {
	CreateGroup(ctx context.Context, g *EducationGroup) error
	ListGroups(ctx context.Context) ([]*EducationGroup, error)
	RoleExists(ctx context.Context, name string) (bool, error)

	CreateSchool(ctx context.Context, s *School) error
	SchoolByID(ctx context.Context, id string) (*School, error)
	SchoolByCode(ctx context.Context, code string) (*School, error)
	ListSchools(ctx context.Context, groupID *string, limit, offset int) ([]*School, int, error)

	CreateCampus(ctx context.Context, c *Campus) error
	ListCampuses(ctx context.Context, schoolID string, limit, offset int) ([]*Campus, int, error)

	AddMember(ctx context.Context, m *Membership) error
	MembershipsForUser(ctx context.Context, userID string) ([]*Membership, error)
	// HasActiveMembership reports whether the user holds ANY active
	// membership at the school, regardless of role. Deterministic over all
	// membership rows — access decisions must not depend on row order.
	HasActiveMembership(ctx context.Context, userID, schoolID string) (bool, error)
	ListMembers(ctx context.Context, schoolID string, limit, offset int) ([]*Membership, int, error)
	IsPlatformAdmin(ctx context.Context, userID string) (bool, error)
	PermissionsFromMemberships(ctx context.Context, userID string) (map[string]bool, error)
}
