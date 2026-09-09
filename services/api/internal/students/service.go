package students

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/events"
	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/google/uuid"
)

// Service implements students business rules.
type Service struct {
	repo Repo
	pool *postgres.Pool
}

func NewService(repo Repo, pool *postgres.Pool) *Service {
	return &Service{repo: repo, pool: pool}
}

var (
	ErrValidation      = errors.New("students: validation failed")
	ErrAlreadyEnrolled = errors.New("students: learner already has an open enrollment at this school")
	ErrLinkExists      = errors.New("students: guardian already linked to learner")
	ErrExternalIDTaken = errors.New("students: learner external id already in use")
)

// LearnerInput carries the fields for provisioning a learner identity.
type LearnerInput struct {
	FirstName   string
	LastName    string
	MiddleName  string
	DateOfBirth *time.Time
	Gender      string
	ExternalID  string
}

// GuardianInput carries the fields for provisioning a guardian.
type GuardianInput struct {
	FirstName string
	LastName  string
	Phone     string
	Email     string
}

// LinkInput carries the fields for binding a guardian to a learner.
type LinkInput struct {
	GuardianID        string
	Relationship      string
	IsPrimary         bool
	CanViewFinancials bool
	CanViewAcademics  bool
}

// EnrollmentInput carries the fields for creating an enrollment.
type EnrollmentInput struct {
	LearnerID      string
	ClassGroupID   string
	AcademicYearID string
	Status         string // "" | applicant | admitted (defaults to admitted)
}

// CreateLearner provisions a global (non-tenant) learner identity and emits
// students.LearnerCreated v1 scoped to the acting school.
func (s *Service) CreateLearner(ctx context.Context, schoolID string, in LearnerInput) (*Learner, error) {
	first := strings.TrimSpace(in.FirstName)
	last := strings.TrimSpace(in.LastName)
	middle := strings.TrimSpace(in.MiddleName)
	if first == "" || last == "" {
		return nil, fmt.Errorf("%w: learner first and last name required", ErrValidation)
	}
	if len(first) > 100 || len(last) > 100 || len(middle) > 100 {
		return nil, fmt.Errorf("%w: learner names must be <= 100 chars", ErrValidation)
	}
	if in.DateOfBirth != nil && in.DateOfBirth.After(time.Now()) {
		return nil, fmt.Errorf("%w: date of birth must be in the past", ErrValidation)
	}
	l := &Learner{
		ID:          uuid.NewString(),
		FirstName:   first,
		LastName:    last,
		MiddleName:  optionalText(middle),
		DateOfBirth: in.DateOfBirth,
		Gender:      strings.TrimSpace(in.Gender),
		ExternalID:  optionalText(strings.TrimSpace(in.ExternalID)),
	}
	if err := s.repo.CreateLearner(ctx, l); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrExternalIDTaken
		}
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, l.ID, "students.LearnerCreated", 1,
			map[string]any{"first_name": l.FirstName, "last_name": l.LastName, "external_id": l.ExternalID})
	}
	return l, nil
}

// LearnerByID resolves a learner for a school context — 404-semantics unless
// the learner holds an enrollment at that school.
func (s *Service) LearnerByID(ctx context.Context, schoolID, learnerID string) (*Learner, error) {
	return s.repo.LearnerInSchool(ctx, schoolID, learnerID)
}

// ListLearners lists learners enrolled at the school, optionally filtered by
// a name substring.
func (s *Service) ListLearners(ctx context.Context, schoolID, query string) ([]*Learner, error) {
	return s.repo.ListLearners(ctx, schoolID, strings.TrimSpace(query))
}

// CreateGuardian provisions a guardian and emits students.GuardianCreated v1.
func (s *Service) CreateGuardian(ctx context.Context, schoolID string, in GuardianInput) (*Guardian, error) {
	first := strings.TrimSpace(in.FirstName)
	last := strings.TrimSpace(in.LastName)
	phone := strings.TrimSpace(in.Phone)
	email := strings.TrimSpace(in.Email)
	if first == "" || last == "" {
		return nil, fmt.Errorf("%w: guardian first and last name required", ErrValidation)
	}
	if len(first) > 100 || len(last) > 100 {
		return nil, fmt.Errorf("%w: guardian names must be <= 100 chars", ErrValidation)
	}
	if !validPhone(phone) {
		return nil, fmt.Errorf("%w: invalid guardian phone", ErrValidation)
	}
	if !validEmail(email) {
		return nil, fmt.Errorf("%w: invalid guardian email", ErrValidation)
	}
	g := &Guardian{ID: uuid.NewString(), FirstName: first, LastName: last, Phone: phone, Email: email}
	if err := s.repo.CreateGuardian(ctx, g); err != nil {
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, g.ID, "students.GuardianCreated", 1,
			map[string]any{"first_name": g.FirstName, "last_name": g.LastName})
	}
	return g, nil
}

// LinkGuardian binds an existing guardian to a learner with relationship and
// access flags, and emits students.GuardianLinked v1. The learner must be
// enrolled at the acting school (tenant guard) and both records must exist.
func (s *Service) LinkGuardian(ctx context.Context, schoolID, learnerID string, in LinkInput) (*GuardianLink, error) {
	if in.GuardianID == "" {
		return nil, fmt.Errorf("%w: guardianId required", ErrValidation)
	}
	rel := Relationship(in.Relationship)
	if !ValidRelationship(rel) {
		return nil, fmt.Errorf("%w: relationship must be one of mother|father|guardian|other", ErrValidation)
	}
	if _, err := s.repo.LearnerInSchool(ctx, schoolID, learnerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: learner not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if _, err := s.repo.GuardianByID(ctx, in.GuardianID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: guardian %s not found", ErrNotFound, in.GuardianID)
		}
		return nil, err
	}
	link := &GuardianLink{
		GuardianID:        in.GuardianID,
		LearnerID:         learnerID,
		Relationship:      string(rel),
		IsPrimary:         in.IsPrimary,
		CanViewFinancials: in.CanViewFinancials,
		CanViewAcademics:  in.CanViewAcademics,
	}
	if err := s.repo.CreateGuardianLink(ctx, link); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrLinkExists
		}
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, learnerID, "students.GuardianLinked", 1,
			map[string]any{"guardian_id": link.GuardianID, "learner_id": link.LearnerID, "relationship": link.Relationship})
	}
	return link, nil
}

// GuardiansForLearner lists the guardian links of a learner visible to the
// acting school.
func (s *Service) GuardiansForLearner(ctx context.Context, schoolID, learnerID string) ([]*GuardianLinkView, error) {
	if _, err := s.repo.LearnerInSchool(ctx, schoolID, learnerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: learner not found in this school", ErrNotFound)
		}
		return nil, err
	}
	return s.repo.GuardianLinksForLearner(ctx, learnerID)
}

// EnrollLearner binds a learner into a school with an initial lifecycle
// status (admitted by default, applicant allowed) and emits
// students.LearnerEnrolled v1 scoped to the school.
func (s *Service) EnrollLearner(ctx context.Context, schoolID string, in EnrollmentInput) (*Enrollment, error) {
	if in.LearnerID == "" {
		return nil, fmt.Errorf("%w: learnerId required", ErrValidation)
	}
	initial := EnrollmentStatus(strings.TrimSpace(in.Status))
	if initial == "" {
		initial = StatusAdmitted
	}
	if initial != StatusAdmitted && initial != StatusApplicant {
		return nil, fmt.Errorf("%w: initial status must be applicant or admitted", ErrValidation)
	}
	// The learner identity record must exist (global identity domain).
	if _, err := s.repo.LearnerByID(ctx, in.LearnerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: learner %s not found", ErrNotFound, in.LearnerID)
		}
		return nil, err
	}
	if open, err := s.repo.HasOpenEnrollment(ctx, schoolID, in.LearnerID); err != nil {
		return nil, err
	} else if open {
		return nil, ErrAlreadyEnrolled
	}
	e := &Enrollment{
		ID:        uuid.NewString(),
		SchoolID:  schoolID,
		LearnerID: in.LearnerID,
		Status:    initial,
		StartedAt: ptrTime(time.Now().UTC()),
	}
	if in.ClassGroupID != "" {
		e.ClassGroupID = &in.ClassGroupID
	}
	if in.AcademicYearID != "" {
		e.AcademicYearID = &in.AcademicYearID
	}
	if err := s.repo.CreateEnrollment(ctx, e); err != nil {
		if isUniqueViolation(err) {
			// Backstop for the partial unique index on open enrollments.
			return nil, ErrAlreadyEnrolled
		}
		return nil, err
	}
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, e.ID, "students.LearnerEnrolled", 1,
			map[string]any{"learner_id": e.LearnerID, "school_id": schoolID, "status": string(e.Status)})
	}
	return e, nil
}

// ListEnrollments lists a school's enrollments, optionally filtered by status.
func (s *Service) ListEnrollments(ctx context.Context, schoolID string, status *EnrollmentStatus) ([]*Enrollment, error) {
	return s.repo.ListEnrollments(ctx, schoolID, status)
}

// TransitionEnrollment applies the lifecycle state machine then persists the
// move, emitting students.EnrollmentStateChanged v1 with before/after in the
// payload. Illegal moves return ErrIllegalTransition (HTTP 409).
func (s *Service) TransitionEnrollment(ctx context.Context, schoolID, enrollmentID string, to EnrollmentStatus) (*Enrollment, error) {
	if !ValidEnrollmentStatus(to) {
		return nil, fmt.Errorf("%w: unknown enrollment status %q", ErrValidation, to)
	}
	e, err := s.repo.EnrollmentByIDInSchool(ctx, schoolID, enrollmentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: enrollment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	from := e.Status
	if !CanTransition(from, to) {
		return nil, fmt.Errorf("%w: cannot move enrollment from %s to %s", ErrIllegalTransition, from, to)
	}
	var endedAt *time.Time
	if containsState(endedAtStates, to) {
		endedAt = ptrTime(time.Now().UTC())
	}
	if err := s.repo.UpdateEnrollmentStatus(ctx, e.ID, from, to, endedAt); err != nil {
		return nil, err
	}
	e.Status = to
	e.EndedAt = endedAt
	if s.pool != nil {
		_, _ = events.Record(ctx, s.pool, &schoolID, e.ID, "students.EnrollmentStateChanged", 1,
			map[string]any{"learner_id": e.LearnerID, "before": string(from), "after": string(to)})
	}
	return e, nil
}

// --- helpers ----------------------------------------------------------------

// optionalText maps "" to NULL for optional text columns.
func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrTime(t time.Time) *time.Time { return &t }

// validPhone accepts "" or a loose international shape: 7-20 chars drawn from
// digits with optional +, spaces, dashes and parentheses.
func validPhone(p string) bool {
	if p == "" {
		return true
	}
	if len(p) < 7 || len(p) > 20 {
		return false
	}
	digits := 0
	for _, c := range p {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
		default:
			return false
		}
	}
	return digits >= 7
}

// validEmail accepts "" or a basic local@domain shape (no whitespace, single @).
func validEmail(e string) bool {
	if e == "" {
		return true
	}
	if len(e) > 254 || strings.ContainsAny(e, " \t") {
		return false
	}
	at := strings.Index(e, "@")
	return at > 0 && at < len(e)-1 && !strings.Contains(e[at+1:], "@")
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
