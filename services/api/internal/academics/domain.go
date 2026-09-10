// Package academics is the bounded context owning the academic structure:
// academic years, terms, subjects, class groups, class rosters, and teaching
// assignments. Every aggregate here is school-scoped — the class group is the
// hub that attendance and assignments hang off in later phases (issue #10/#11).
// Curriculum is designed extensible: strands/outcomes land in later phases.
package academics

import (
	"context"
	"errors"
	"time"
)

// AcademicYear is a school's named academic year with inclusive start/end
// dates. Terms of a year must nest inside the year's range.
type AcademicYear struct {
	ID        string    `json:"id"`
	SchoolID  string    `json:"schoolId"`
	Name      string    `json:"name"`
	StartDate string    `json:"startDate"` // YYYY-MM-DD
	EndDate   string    `json:"endDate"`   // YYYY-MM-DD
	Status    string    `json:"status"`    // planning | active | closed
	CreatedAt time.Time `json:"createdAt"`
}

// AllYearStatuses is the closed set of year lifecycle states — mirrored by
// the CHECK constraint on academic_years.status.
var AllYearStatuses = []string{"planning", "active", "closed"}

// Term is a named period inside an academic year. Terms of one year must not
// overlap (inclusive ranges) — enforced at the service layer against sibling
// terms, mirroring the checked invariant with clear domain errors.
type Term struct {
	ID             string    `json:"id"`
	SchoolID       string    `json:"schoolId"`
	AcademicYearID string    `json:"academicYearId"`
	Name           string    `json:"name"`
	StartDate      string    `json:"startDate"` // YYYY-MM-DD
	EndDate        string    `json:"endDate"`   // YYYY-MM-DD
	CreatedAt      time.Time `json:"createdAt"`
}

// Subject is a course of study offered by a school. Code is unique per school.
type Subject struct {
	ID       string `json:"id"`
	SchoolID string `json:"schoolId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

// ClassGroup is a taught class — a named group of learners for an academic
// year. The (school, year, name) triple is unique.
type ClassGroup struct {
	ID             string `json:"id"`
	SchoolID       string `json:"schoolId"`
	AcademicYearID string `json:"academicYearId"`
	Name           string `json:"name"`
}

// RosterEntry seats a learner in a class group (idempotent pair).
type RosterEntry struct {
	ClassGroupID string `json:"classGroupId"`
	LearnerID    string `json:"learnerId"`
}

// RosterEntryView is a roster entry enriched with learner identity for API
// responses (single JOIN — no N+1 fan-out).
type RosterEntryView struct {
	LearnerID  string  `json:"learnerId"`
	FirstName  string  `json:"firstName"`
	LastName   string  `json:"lastName"`
	ExternalID *string `json:"externalId,omitempty"`
}

// TeachingAssignment binds a teacher (identity user) to a class+subject pair.
// The (class, subject) pair is unique: one teacher owns a subject in a class.
type TeachingAssignment struct {
	ID           string `json:"id"`
	SchoolID     string `json:"schoolId"`
	ClassGroupID string `json:"classGroupId"`
	SubjectID    string `json:"subjectId"`
	TeacherID    string `json:"teacherId"`
}

// TeachingAssignmentView is an assignment enriched with the teacher's
// identity for API responses.
type TeachingAssignmentView struct {
	Assignment TeachingAssignment `json:"assignment"`
	Teacher    TeacherView        `json:"teacher"`
	Subject    SubjectRef         `json:"subject"`
}

// TeacherView carries display fields of an identity user. academics never
// mutates identity data — read-only join on the id column only.
type TeacherView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SubjectRef carries display fields of a subject on an assignment view.
type SubjectRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// Domain errors (mapped to HTTP status codes in the transport layer).
var (
	// ErrValidation maps to 400: malformed input.
	ErrValidation = errors.New("academics: validation failed")
	// ErrNotFound maps to 404: aggregate missing in this school's scope.
	ErrNotFound = errors.New("academics: not found")
	// ErrDateRange maps to 400: end before start.
	ErrDateRange = errors.New("academics: end date before start date")
	// ErrTermOverlap maps to 409: term range overlaps a sibling term.
	ErrTermOverlap = errors.New("academics: term overlaps an existing term")
	// ErrOutsideYear maps to 400: term range not nested in its academic year.
	ErrOutsideYear = errors.New("academics: term outside academic year range")
	// ErrCodeTaken maps to 409: subject code already used by the school.
	ErrCodeTaken = errors.New("academics: subject code already in use")
	// ErrNameTaken maps to 409: class name already used for the year.
	ErrNameTaken = errors.New("academics: class name already in use for year")
	// ErrAssignmentExists maps to 409: class+subject already assigned.
	ErrAssignmentExists = errors.New("academics: subject already assigned for class")
	// ErrBatchSize maps to 400: roster batch outside 1..500.
	ErrBatchSize = errors.New("academics: roster batch must contain 1..500 learner ids")
)

// Repo is the persistence port of the academics context. Every method takes
// the schoolID of the acting tenant and scopes its SQL by it — tenant
// isolation never relies on the transport layer.
type Repo interface {
	// Academic years.
	CreateAcademicYear(ctx context.Context, schoolID string, y *AcademicYear) error
	AcademicYearByID(ctx context.Context, schoolID, id string) (*AcademicYear, error)
	ListAcademicYears(ctx context.Context, schoolID string) ([]*AcademicYear, error)

	// Terms.
	CreateTerm(ctx context.Context, schoolID string, t *Term) error
	TermsForYear(ctx context.Context, schoolID, yearID string) ([]*Term, error)

	// Subjects.
	CreateSubject(ctx context.Context, schoolID string, s *Subject) error
	SubjectByID(ctx context.Context, schoolID, id string) (*Subject, error)
	ListSubjects(ctx context.Context, schoolID string) ([]*Subject, error)

	// Class groups.
	CreateClassGroup(ctx context.Context, schoolID string, c *ClassGroup) error
	ClassGroupByID(ctx context.Context, schoolID, id string) (*ClassGroup, error)
	ListClassGroups(ctx context.Context, schoolID, yearID string) ([]*ClassGroup, error)

	// Roster.
	// AddRosterEntries returns the learner ids that were NOT seated
	// (unknown, already handled as duplicates are skipped; unenrolled-at-this-
	// school learners are reported so the service can reject the batch).
	AddRosterEntries(ctx context.Context, schoolID, classGroupID string, learnerIDs []string) ([]string, error)
	RosterForClass(ctx context.Context, schoolID, classGroupID string) ([]*RosterEntryView, error)

	// Teaching assignments.
	CreateTeachingAssignment(ctx context.Context, schoolID string, a *TeachingAssignment) error
	AssignmentsForClass(ctx context.Context, schoolID, classGroupID string) ([]*TeachingAssignmentView, error)
}
