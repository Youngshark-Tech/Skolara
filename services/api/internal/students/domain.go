// Package students is the bounded context owning learner identity — kept
// deliberately SEPARATE from school enrollment (master spec §19): a learner
// is a global person record (NOT tenant-scoped) that can enroll at multiple
// schools over time; an enrollment is the tenant binding carrying a lifecycle
// state machine. Guardians attach to learners via links with per-link access
// flags consumed by guardian-facing surfaces.
package students

import (
	"context"
	"errors"
	"time"
)

// Learner is the immutable identity record of a person. It carries no school
// scoping: enrollment is what binds a learner to a tenant.
type Learner struct {
	ID          string     `json:"id"`
	FirstName   string     `json:"firstName"`
	LastName    string     `json:"lastName"`
	MiddleName  *string    `json:"middleName,omitempty"`
	DateOfBirth *time.Time `json:"dateOfBirth,omitempty"`
	Gender      string     `json:"gender,omitempty"`
	ExternalID  *string    `json:"externalId,omitempty"` // school-issued admission number, unique when present
	CreatedAt   time.Time  `json:"createdAt"`
}

// Guardian is an adult related to one or more learners (parent/caregiver).
// The relationship to a specific learner lives on the GuardianLink.
type Guardian struct {
	ID        string    `json:"id"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Relationship of a guardian to a learner (closed set, enforced in SQL too).
type Relationship string

const (
	RelationshipMother   Relationship = "mother"
	RelationshipFather   Relationship = "father"
	RelationshipGuardian Relationship = "guardian"
	RelationshipOther    Relationship = "other"
)

// AllRelationships is the closed set of valid guardian-learner relationships.
var AllRelationships = []Relationship{
	RelationshipMother, RelationshipFather, RelationshipGuardian, RelationshipOther,
}

// ValidRelationship reports whether r is a known relationship.
func ValidRelationship(r Relationship) bool {
	for _, known := range AllRelationships {
		if known == r {
			return true
		}
	}
	return false
}

// GuardianLink binds a guardian to a learner with the relationship plus the
// access-control flags guardian-facing surfaces must honor.
type GuardianLink struct {
	GuardianID        string `json:"guardianId"`
	LearnerID         string `json:"learnerId"`
	Relationship      string `json:"relationship"`
	IsPrimary         bool   `json:"isPrimary"`
	CanViewFinancials bool   `json:"canViewFinancials"`
	CanViewAcademics  bool   `json:"canViewAcademics"`
}

// GuardianLinkView is a link enriched with the guardian's identity for API
// responses (single JOIN — no N+1 fan-out).
type GuardianLinkView struct {
	Link     GuardianLink `json:"link"`
	Guardian Guardian     `json:"guardian"`
}

// EnrollmentStatus enumerates the enrollment lifecycle states (spec §19).
type EnrollmentStatus string

const (
	StatusApplicant       EnrollmentStatus = "applicant"
	StatusAdmitted        EnrollmentStatus = "admitted"
	StatusActive          EnrollmentStatus = "active"
	StatusSuspended       EnrollmentStatus = "suspended"
	StatusTransferPending EnrollmentStatus = "transfer_pending"
	StatusTransferredOut  EnrollmentStatus = "transferred_out"
	StatusAlumni          EnrollmentStatus = "alumni"
	StatusWithdrawn       EnrollmentStatus = "withdrawn"
	StatusGraduated       EnrollmentStatus = "graduated"
)

// AllEnrollmentStates is the closed set of valid enrollment statuses —
// mirrored by the CHECK constraint on enrollments.status.
var AllEnrollmentStates = []EnrollmentStatus{
	StatusApplicant, StatusAdmitted, StatusActive, StatusSuspended,
	StatusTransferPending, StatusTransferredOut, StatusAlumni,
	StatusWithdrawn, StatusGraduated,
}

// openEnrollmentStates are the non-terminal states: a learner holding an
// enrollment in one of these at a school cannot open a second one there.
var openEnrollmentStates = []EnrollmentStatus{
	StatusApplicant, StatusAdmitted, StatusActive, StatusSuspended, StatusTransferPending,
}

// endedAtStates set enrollment.ended_at when entered.
var endedAtStates = []EnrollmentStatus{
	StatusTransferredOut, StatusWithdrawn, StatusGraduated,
}

// ValidEnrollmentStatus reports whether s is a known enrollment state.
func ValidEnrollmentStatus(s EnrollmentStatus) bool {
	for _, known := range AllEnrollmentStates {
		if known == s {
			return true
		}
	}
	return false
}

func containsState(states []EnrollmentStatus, s EnrollmentStatus) bool {
	for _, known := range states {
		if known == s {
			return true
		}
	}
	return false
}

// legalTransitions is the authoritative enrollment state machine. Any move
// absent from this map is rejected with ErrIllegalTransition:
//
//	applicant        -> admitted | withdrawn
//	admitted         -> active | withdrawn
//	active           -> suspended | transfer_pending | graduated | withdrawn | alumni
//	suspended        -> active | withdrawn
//	transfer_pending -> transferred_out | active
//	transferred_out  -> (terminal)
//	graduated        -> alumni
//	withdrawn        -> (terminal)
//	alumni           -> (terminal)
var legalTransitions = map[EnrollmentStatus][]EnrollmentStatus{
	StatusApplicant:       {StatusAdmitted, StatusWithdrawn},
	StatusAdmitted:        {StatusActive, StatusWithdrawn},
	StatusActive:          {StatusSuspended, StatusTransferPending, StatusGraduated, StatusWithdrawn, StatusAlumni},
	StatusSuspended:       {StatusActive, StatusWithdrawn},
	StatusTransferPending: {StatusTransferredOut, StatusActive},
	StatusTransferredOut:  {},
	StatusGraduated:       {StatusAlumni},
	StatusWithdrawn:       {},
	StatusAlumni:          {},
}

// ErrIllegalTransition is returned when an enrollment state change is not
// permitted by the lifecycle state machine (maps to HTTP 409).
var ErrIllegalTransition = errors.New("students: illegal enrollment transition")

// CanTransition reports whether moving an enrollment from -> to is permitted.
func CanTransition(from, to EnrollmentStatus) bool {
	for _, next := range legalTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Enrollment is the tenant binding of a learner to a school. All enrollment
// lookups MUST be school-scoped — school_id is part of every query.
type Enrollment struct {
	ID             string           `json:"id"`
	SchoolID       string           `json:"schoolId"`
	LearnerID      string           `json:"learnerId"`
	ClassGroupID   *string          `json:"classGroupId,omitempty"`   // plain UUID for now: FK lands with the academics migration
	AcademicYearID *string          `json:"academicYearId,omitempty"` // FK lands with the academics migration
	Status         EnrollmentStatus `json:"status"`
	StartedAt      *time.Time       `json:"startedAt,omitempty"`
	EndedAt        *time.Time       `json:"endedAt,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
}

// Repo is the persistence port of the students context.
type Repo interface {
	// Learners — global identity records (no school scoping on the row).
	CreateLearner(ctx context.Context, l *Learner) error
	LearnerByID(ctx context.Context, id string) (*Learner, error)
	// LearnerInSchool resolves a learner ONLY when it holds an enrollment at
	// the given school — the tenant visibility guard for cross-school reads.
	LearnerInSchool(ctx context.Context, schoolID, learnerID string) (*Learner, error)
	// ListLearners lists learners via their enrollments (tenant-scoped);
	// query optionally filters by name substring.
	ListLearners(ctx context.Context, schoolID, query string) ([]*Learner, error)

	// Guardians and links.
	CreateGuardian(ctx context.Context, g *Guardian) error
	GuardianByID(ctx context.Context, id string) (*Guardian, error)
	CreateGuardianLink(ctx context.Context, link *GuardianLink) error
	GuardianLinksForLearner(ctx context.Context, learnerID string) ([]*GuardianLinkView, error)

	// Enrollments — always tenant-scoped by schoolID.
	CreateEnrollment(ctx context.Context, e *Enrollment) error
	EnrollmentByIDInSchool(ctx context.Context, schoolID, id string) (*Enrollment, error)
	ListEnrollments(ctx context.Context, schoolID string, status *EnrollmentStatus) ([]*Enrollment, error)
	HasOpenEnrollment(ctx context.Context, schoolID, learnerID string) (bool, error)
	UpdateEnrollmentStatus(ctx context.Context, id string, from, to EnrollmentStatus, endedAt *time.Time) error
}
