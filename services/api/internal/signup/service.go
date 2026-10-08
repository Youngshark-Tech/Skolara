// Package signup implements self-serve school onboarding (issue #130):
// one public workflow that provisions an admin user, an education group, a
// school with a generated unique code, and the admin's school_admin
// membership in a SINGLE transaction, using the domain repos' exported
// *Tx methods so validation and invariants (role-scope triggers, unique
// keys) are enforced by the same database contracts as production traffic.
//
// It lives OUTSIDE the identity/tenancy packages (neither imports the other;
// the composition root wires this workflow), and the HTTP surface is a
// route on the identity handler via the identity.SignupService interface
// (consumer-side interface — no import cycle).
//
// Abuse posture: the endpoint is public but sits behind the global per-IP
// rate limiter; captcha/email-verification are tracked as explicit follow-ups.
package signup

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/identity"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/tenancy"
	"github.com/google/uuid"
)

// The workflow returns IDENTITY sentinel errors (wrapped with context) so the
// identity handler's switch maps them without an import cycle:
// identity.ErrEmailTaken → 409, identity.ErrValidation → 400.

// Input carries the self-serve onboarding request.
type Input struct {
	SchoolName string
	AdminName  string
	Email      string
	Password   string
}

// Result identifies what was provisioned.
type Result struct {
	UserID     string
	Email      string
	GroupID    string
	SchoolID   string
	SchoolCode string
}

// Service runs the signup workflow over the shared pool.
type Service struct {
	pool         *postgres.Pool
	identityRepo identity.Repo
	tenancyRepo  tenancy.Repo
}

// New composes the workflow. Both repos are plain pool-backed repos; the
// transaction is owned HERE (one tx across all writes).
func New(pool *postgres.Pool, idRepo identity.Repo, tenRepo tenancy.Repo) *Service {
	return &Service{pool: pool, identityRepo: idRepo, tenancyRepo: tenRepo}
}

// Compile-time proof the workflow satisfies the identity handler's
// consumer-side interface (issue #130) — no import cycle either way.
var _ identity.SignupService = (*Service)(nil)

// Signup provisions user + group + school + membership atomically, adapting
// the identity-handler DTOs onto the internal input shape.
func (s *Service) Signup(ctx context.Context, req identity.SignupRequest) (*identity.SignupOutcome, error) {
	res, err := s.run(ctx, Input{
		SchoolName: req.SchoolName,
		AdminName:  req.AdminName,
		Email:      req.Email,
		Password:   req.Password,
	})
	if err != nil {
		return nil, err
	}
	return &identity.SignupOutcome{
		UserID:     res.UserID,
		Email:      res.Email,
		Name:       req.AdminName,
		SchoolID:   res.SchoolID,
		SchoolCode: res.SchoolCode,
		SchoolName: req.SchoolName,
	}, nil
}

// run is the internal workflow over the package-local Input shape.
func (s *Service) run(ctx context.Context, in Input) (*Result, error) {
	in.SchoolName = strings.TrimSpace(in.SchoolName)
	in.AdminName = strings.TrimSpace(in.AdminName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	if len(in.SchoolName) < 2 || len(in.SchoolName) > 128 {
		return nil, fmt.Errorf("%w: school name must be 2-128 characters", identity.ErrValidation)
	}
	if in.AdminName == "" || len(in.AdminName) > 120 {
		return nil, fmt.Errorf("%w: your name is required", identity.ErrValidation)
	}
	if !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " \t\r\n") {
		return nil, fmt.Errorf("%w: a valid email is required", identity.ErrValidation)
	}
	// Hash BEFORE opening the transaction (argon2 is expensive; hash errors
	// are pure validation — the 8-char floor lives in identity.HashPassword).
	hash, err := identity.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("%w: password must be at least 8 characters", identity.ErrValidation)
	}

	// Fast, cheap pre-check for the common conflict; the DB unique key inside
	// the tx remains the authority (raced signups still get 409).
	if _, err := s.identityRepo.UserByEmail(ctx, in.Email); err == nil {
		return nil, identity.ErrEmailTaken
	}

	user := &identity.User{
		ID:           identity.NewID(),
		Email:        in.Email,
		Name:         in.AdminName,
		PasswordHash: hash,
		Status:       identity.StatusActive,
	}
	group := &tenancy.EducationGroup{ID: uuid.NewString(), Name: groupDisplayName(in.SchoolName)}
	school := &tenancy.School{
		ID:      uuid.NewString(),
		Code:    generateSchoolCode(),
		Name:    in.SchoolName,
		GroupID: &group.ID,
	}
	member := &tenancy.Membership{UserID: user.ID, SchoolID: school.ID, Role: "school_admin", Status: "active"}

	err = s.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		if err := s.identityRepo.CreateUserTx(ctx, tx, user); err != nil {
			return err
		}
		if err := s.tenancyRepo.CreateGroupTx(ctx, tx, group); err != nil {
			return err
		}
		if err := s.tenancyRepo.CreateSchoolTx(ctx, tx, school); err != nil {
			// Generated-code collision: regenerate and retry within the same
			// tx. Rare by construction (31^6 space) but handled.
			if strings.Contains(err.Error(), "schools_code_key") {
				school.Code = generateSchoolCode()
				if err := s.tenancyRepo.CreateSchoolTx(ctx, tx, school); err != nil {
					return err
				}
				return recordSchoolEvents(ctx, tx, group, school)
			}
			return err
		}
		if err := s.tenancyRepo.AddMemberTx(ctx, tx, member); err != nil {
			return err
		}
		// Domain events mirror what the tenancy service emits on its own
		// write paths, so outbox consumers see the same picture.
		if err := recordSchoolEvents(ctx, tx, group, school); err != nil {
			return err
		}
		_, err := events.Record(ctx, tx, &school.ID, school.ID, "tenancy.MemberAdded", 1,
			map[string]any{"school_id": school.ID, "user_id": user.ID, "role": "school_admin"})
		return err
	})
	if err != nil {
		if strings.Contains(err.Error(), "users_email_key") {
			return nil, fmt.Errorf("%w (raced signup)", identity.ErrEmailTaken)
		}
		return nil, err
	}

	return &Result{
		UserID:     user.ID,
		Email:      user.Email,
		GroupID:    group.ID,
		SchoolID:   school.ID,
		SchoolCode: school.Code,
	}, nil
}

func recordSchoolEvents(ctx context.Context, tx postgres.Querier, group *tenancy.EducationGroup, school *tenancy.School) error {
	if _, err := events.Record(ctx, tx, &school.ID, group.ID, "tenancy.EducationGroupCreated", 1,
		map[string]any{"name": group.Name}); err != nil {
		return err
	}
	_, err := events.Record(ctx, tx, &school.ID, school.ID, "tenancy.SchoolCreated", 1,
		map[string]any{"code": school.Code, "name": school.Name})
	return err
}

// groupDisplayName derives the owning education group's name from the school.
func groupDisplayName(schoolName string) string {
	return schoolName + " Group"
}

// generateSchoolCode returns an SL-XXXXXX code from crypto/rand
// (31^6 ≈ 887M space; collisions are retried by the caller).
func generateSchoolCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O/1/I/L lookalikes
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable; derive from a UUID instead.
		u := uuid.NewString()
		out := make([]byte, 0, 6)
		for _, c := range []byte(u) {
			if len(out) == 6 {
				break
			}
			if c >= '0' && c <= '9' {
				out = append(out, byte('2'+(c-'0')%8))
			} else if c >= 'a' && c <= 'z' {
				out = append(out, byte('A'+(c-'a')%25))
			}
		}
		for len(out) < 6 {
			out = append(out, 'X')
		}
		return "SL-" + string(out)
	}
	out := make([]byte, 6)
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return "SL-" + string(out)
}
