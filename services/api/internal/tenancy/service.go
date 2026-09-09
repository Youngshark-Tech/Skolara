package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

// Service implements tenancy business rules.
type Service struct {
	repo Repo
	pool *postgres.Pool
}

func NewService(repo Repo, pool *postgres.Pool) *Service {
	return &Service{repo: repo, pool: pool}
}

var (
	ErrValidation = errors.New("tenancy: validation failed")
	ErrCodeTaken  = errors.New("tenancy: code already in use")
	ErrNotAMember = errors.New("tenancy: no active membership for school")
)

// CreateGroup validates and creates an education group, emitting
// tenancy.GroupCreated v1 (platform-scope event: no school_id).
func (s *Service) CreateGroup(ctx context.Context, name, actorID string) (*EducationGroup, error) {
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: group name required (<=128 chars)", ErrValidation)
	}
	g := &EducationGroup{ID: newID(), Name: name}
	if err := s.repo.CreateGroup(ctx, g); err != nil {
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, nil, g.ID, "tenancy.GroupCreated", 1,
			map[string]any{"name": g.Name, "actor_id": actorID})
	}
	return g, nil
}

// Groups lists education groups (admin surface).
func (s *Service) Groups(ctx context.Context) ([]*EducationGroup, error) {
	return s.repo.ListGroups(ctx)
}

// RoleExists reports whether a named role is seeded in the RBAC matrix.
// Used to reject unknown role references with a 400 instead of a NULL
// not-null violation surfacing as a 500.
func (s *Service) RoleExists(ctx context.Context, name string) (bool, error) {
	return s.repo.RoleExists(ctx, name)
}

// CreateSchool validates and creates a school (tenant root), seeding the
// default institution ledger accounts (via finance hook when it lands) and
// emitting tenancy.SchoolCreated.
func (s *Service) CreateSchool(ctx context.Context, code, name, groupID, actorID string) (*School, error) {
	if code == "" || len(code) > 32 {
		return nil, fmt.Errorf("%w: school code required (<=32 chars)", ErrValidation)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: school name required", ErrValidation)
	}
	var gid *string
	if groupID != "" {
		gid = &groupID
	}
	school := &School{ID: newID(), Code: code, Name: name, GroupID: gid}
	if err := s.repo.CreateSchool(ctx, school); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCodeTaken
		}
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &school.ID, school.ID, "tenancy.SchoolCreated", 1,
			map[string]any{"code": school.Code, "name": school.Name})
	}
	return school, nil
}

func (s *Service) ListSchools(ctx context.Context, groupID string) ([]*School, error) {
	var gid *string
	if groupID != "" {
		gid = &groupID
	}
	return s.repo.ListSchools(ctx, gid)
}

func (s *Service) School(ctx context.Context, id string) (*School, error) {
	return s.repo.SchoolByID(ctx, id)
}

func (s *Service) CreateCampus(ctx context.Context, schoolID, code, name, location string) (*Campus, error) {
	if code == "" || name == "" {
		return nil, fmt.Errorf("%w: campus code and name required", ErrValidation)
	}
	c := &Campus{ID: newID(), SchoolID: schoolID, Code: code, Name: name, Location: location}
	if err := s.repo.CreateCampus(ctx, c); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCodeTaken
		}
		return nil, err
	}
	return c, nil
}

func (s *Service) Campuses(ctx context.Context, schoolID string) ([]*Campus, error) {
	return s.repo.ListCampuses(ctx, schoolID)
}

// AddMember grants a user a school-scoped role after validating the role
// exists (400 otherwise) and mapping an unknown target user's FK violation
// to a validation error instead of a 500.
func (s *Service) AddMember(ctx context.Context, schoolID, userID, role string) error {
	if role == "" {
		return fmt.Errorf("%w: role required", ErrValidation)
	}
	if userID == "" {
		return fmt.Errorf("%w: userId required", ErrValidation)
	}
	ok, err := s.repo.RoleExists(ctx, role)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown role %q", ErrValidation, role)
	}
	if err := s.repo.AddMember(ctx, &Membership{UserID: userID, SchoolID: schoolID, Role: role, Status: "active"}); err != nil {
		if isFKViolation(err) {
			return fmt.Errorf("%w: user %s does not exist", ErrValidation, userID)
		}
		return err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, userID, "tenancy.MemberAdded", 1,
			map[string]any{"school_id": schoolID, "user_id": userID, "role": role})
	}
	return nil
}

// PermissionsFor returns permissions granted via active school memberships —
// implements identity.PermissionResolver; combined with identity's
// platform-role set at the composition root.
func (s *Service) PermissionsFor(ctx context.Context, userID string) (map[string]bool, error) {
	return s.repo.PermissionsFromMemberships(ctx, userID)
}

// HasActiveMembership exposes the deterministic any-active-row access check
// used by school-scoped handlers.
func (s *Service) HasActiveMembership(ctx context.Context, userID, schoolID string) (bool, error) {
	return s.repo.HasActiveMembership(ctx, userID, schoolID)
}

// IsPlatformAdmin reports whether the user holds the platform_admin role.
func (s *Service) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	return s.repo.IsPlatformAdmin(ctx, userID)
}

// ResolveSchoolContext determines which school a request operates on:
//  1. explicit header X-School-ID — requires an ACTIVE membership, or the
//     platform_admin role (administrative access)
//  2. otherwise the user's single active membership (unambiguous default)
//
// It returns ErrNotAMember when no valid context exists.
func (s *Service) ResolveSchoolContext(ctx context.Context, userID, requestedSchoolID string) (*School, error) {
	if requestedSchoolID != "" {
		if ok, _ := s.repo.IsPlatformAdmin(ctx, userID); ok {
			return s.repo.SchoolByID(ctx, requestedSchoolID)
		}
	}
	memberships, err := s.repo.MembershipsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	active := make([]*Membership, 0, len(memberships))
	for _, m := range memberships {
		if m.Status == "active" {
			active = append(active, m)
		}
	}
	if requestedSchoolID != "" {
		for _, m := range active {
			if m.SchoolID == requestedSchoolID {
				return s.repo.SchoolByID(ctx, requestedSchoolID)
			}
		}
		return nil, ErrNotAMember
	}
	if len(active) == 1 {
		return s.repo.SchoolByID(ctx, active[0].SchoolID)
	}
	return nil, ErrNotAMember
}

// MembershipsFor exposes a user's memberships.
func (s *Service) MembershipsFor(ctx context.Context, userID string) ([]*Membership, error) {
	return s.repo.MembershipsForUser(ctx, userID)
}

// Members lists a school's members (already tenant-scoped by caller).
func (s *Service) Members(ctx context.Context, schoolID string) ([]*Membership, error) {
	return s.repo.ListMembers(ctx, schoolID)
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

func isFKViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}
