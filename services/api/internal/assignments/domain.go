// Package assignments is the bounded context owning the assignment workflow:
// teachers create and publish assignments, learners (recorded via their
// learner id until identity↔learner linking lands) submit, teachers grade
// and return. Assignment lifecycle: draft → published → closed (linear).
// Submission lifecycle: submitted → graded → returned (linear). Cross-domain
// references (classes, subjects, learners) are DB FKs — no Go imports.
package assignments

import (
	"context"
	"errors"
	"time"
)

// AssignmentStatus is the assignment lifecycle (mirrored by SQL CHECK).
type AssignmentStatus string

const (
	AssignmentDraft     AssignmentStatus = "draft"
	AssignmentPublished AssignmentStatus = "published"
	AssignmentClosed    AssignmentStatus = "closed"
)

// SubmissionStatus is the submission lifecycle (mirrored by SQL CHECK).
type SubmissionStatus string

const (
	SubmissionSubmitted SubmissionStatus = "submitted"
	SubmissionGraded    SubmissionStatus = "graded"
	SubmissionReturned  SubmissionStatus = "returned"
)

// Assignment is a piece of work set for a class in one subject, owned by the
// teacher who created it — only that teacher publishes, closes, and grades.
type Assignment struct {
	ID           string           `json:"id"`
	SchoolID     string           `json:"schoolId"`
	ClassGroupID string           `json:"classGroupId"`
	SubjectID    string           `json:"subjectId"`
	TeacherID    string           `json:"teacherId"`
	Title        string           `json:"title"`
	Instructions string           `json:"instructions,omitempty"`
	DueDate      *string          `json:"dueDate,omitempty"` // YYYY-MM-DD
	Status       AssignmentStatus `json:"status"`
	PublishedAt  *time.Time       `json:"publishedAt,omitempty"`
	ClosedAt     *time.Time       `json:"closedAt,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
}

// Submission is one learner's response to an assignment.
type Submission struct {
	ID           string           `json:"id"`
	SchoolID     string           `json:"schoolId"`
	AssignmentID string           `json:"assignmentId"`
	LearnerID    string           `json:"learnerId"`
	Content      string           `json:"content,omitempty"`
	Status       SubmissionStatus `json:"status"`
	Grade        *string          `json:"grade,omitempty"`
	Feedback     string           `json:"feedback,omitempty"`
	SubmittedAt  time.Time        `json:"submittedAt"`
	GradedAt     *time.Time       `json:"gradedAt,omitempty"`
	ReturnedAt   *time.Time       `json:"returnedAt,omitempty"`
}

// legalAssignmentTransitions is the authoritative assignment state machine:
//
//	draft     -> published | (cancel is future work)
//	published -> closed
//	closed    -> (terminal)
var legalAssignmentTransitions = map[AssignmentStatus][]AssignmentStatus{
	AssignmentDraft:     {AssignmentPublished},
	AssignmentPublished: {AssignmentClosed},
	AssignmentClosed:    {},
}

// CanTransitionAssignment reports whether from -> to is permitted.
func CanTransitionAssignment(from, to AssignmentStatus) bool {
	for _, next := range legalAssignmentTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// ErrIllegalAssignmentTransition maps to HTTP 409.
var ErrIllegalAssignmentTransition = errors.New("assignments: illegal assignment transition")

// Domain errors.
var (
	ErrValidation    = errors.New("assignments: validation failed")
	ErrNotFound      = errors.New("assignments: not found")
	ErrNotOwner      = errors.New("assignments: only the owning teacher may perform this action")
	ErrDueDatePast   = errors.New("assignments: due date is in the past")
	ErrNotOpen       = errors.New("assignments: assignment is not open for submissions")
	ErrAlreadyGraded = errors.New("assignments: submission already graded")
	ErrNotRostered   = errors.New("assignments: learner is not rostered in the assignment's class")
	ErrGradeRequired = errors.New("assignments: grade required")
)

// Repo is the persistence port. Every method takes the acting schoolID.
type Repo interface {
	// RosteredLearner verifies the learner sits in the class roster (SQL join
	// into academics.roster_entries; no Go import).
	RosteredLearner(ctx context.Context, classGroupID, learnerID string) (bool, error)

	CreateAssignment(ctx context.Context, a *Assignment) error
	AssignmentByID(ctx context.Context, schoolID, id string) (*Assignment, error)
	ListAssignments(ctx context.Context, schoolID, classGroupID string, status *AssignmentStatus, limit, offset int) ([]*Assignment, int, error)
	UpdateAssignmentStatus(ctx context.Context, schoolID, id string, from, to AssignmentStatus) error

	UpsertSubmission(ctx context.Context, sub *Submission) error
	ClassGroupInSchool(ctx context.Context, schoolID, classGroupID string) (bool, error)
	SubjectInSchool(ctx context.Context, schoolID, subjectID string) (bool, error)
	Submission(ctx context.Context, schoolID, assignmentID, learnerID string) (*Submission, error)
	SubmissionsForAssignment(ctx context.Context, schoolID, assignmentID string) ([]*Submission, error)
	GradeSubmission(ctx context.Context, schoolID, assignmentID, learnerID, grade, feedback string) (*Submission, error)
	ReturnSubmission(ctx context.Context, schoolID, assignmentID, learnerID string) (*Submission, error)
}
