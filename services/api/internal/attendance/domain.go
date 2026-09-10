// Package attendance is the bounded context owning attendance recording:
// per-class daily sessions and per-learner records with offline-tolerant
// idempotency. Clients generate client_mutation_id values so a replayed
// offline batch never duplicates or double-applies (master spec §46 sync
// groundwork). Sessions hang off academics class groups; records reference
// students learners and identity users via DB FKs — no cross-domain imports.
package attendance

import (
	"context"
	"errors"
	"time"
)

// SessionStatus is the lifecycle of an attendance session.
type SessionStatus string

const (
	SessionOpen   SessionStatus = "open"
	SessionClosed SessionStatus = "closed"
)

// RecordStatus is the closed set of attendance outcomes (mirrored by the SQL
// CHECK constraint).
type RecordStatus string

const (
	StatusPresent RecordStatus = "present"
	StatusAbsent  RecordStatus = "absent"
	StatusLate    RecordStatus = "late"
	StatusExcused RecordStatus = "excused"
)

// AllRecordStatuses is the closed set used for validation.
var AllRecordStatuses = []RecordStatus{StatusPresent, StatusAbsent, StatusLate, StatusExcused}

// ValidRecordStatus reports whether s is a known attendance outcome.
func ValidRecordStatus(s RecordStatus) bool {
	for _, known := range AllRecordStatuses {
		if known == s {
			return true
		}
	}
	return false
}

// AttendanceSession marks a class roll-call for a date. One session per
// (school, class, date) — enforced in SQL and resolved idempotently in the
// service so offline retries converge on the same session.
type AttendanceSession struct {
	ID           string        `json:"id"`
	SchoolID     string        `json:"schoolId"`
	ClassGroupID string        `json:"classGroupId"`
	Date         string        `json:"date"` // YYYY-MM-DD
	Status       SessionStatus `json:"status"`
	CreatedBy    string        `json:"createdBy"`
	CreatedAt    time.Time     `json:"createdAt"`
}

// AttendanceRecord is one learner's outcome for one session.
type AttendanceRecord struct {
	ID               string       `json:"id"`
	SchoolID         string       `json:"schoolId"`
	SessionID        string       `json:"sessionId"`
	LearnerID        string       `json:"learnerId"`
	Status           RecordStatus `json:"status"`
	Reason           string       `json:"reason,omitempty"`
	RecordedBy       string       `json:"recordedBy"`
	ClientMutationID string       `json:"clientMutationId"`
	RecordedAt       time.Time    `json:"recordedAt"`
}

// RecordInput carries one record of a bulk submission.
type RecordInput struct {
	LearnerID        string       `json:"learnerId"`
	Status           RecordStatus `json:"status"`
	Reason           string       `json:"reason"`
	ClientMutationID string       `json:"clientMutationId"`
}

// Domain errors (mapped to HTTP status codes in the transport layer).
var (
	ErrValidation    = errors.New("attendance: validation failed")
	ErrNotFound      = errors.New("attendance: not found")
	ErrSessionClosed = errors.New("attendance: session is closed")
	ErrMutationUsed  = errors.New("attendance: client mutation id already used for another record")
)

// maxRecordBatch bounds one bulk submission.
const maxRecordBatch = 500

// Repo is the persistence port. Every method takes the acting schoolID and
// scopes its SQL by it — tenant isolation never relies on the caller.
type Repo interface {
	// ClassExists verifies the class belongs to the school (SQL-level join
	// into academics.class_groups; no Go import).
	ClassExists(ctx context.Context, schoolID, classGroupID string) (bool, error)
	// CreateSession inserts the session; ErrConflict semantics on duplicate
	// (school, class, date) are resolved by FindSession in the service.
	CreateSession(ctx context.Context, s *AttendanceSession) error
	FindSession(ctx context.Context, schoolID, classGroupID, date string) (*AttendanceSession, error)
	SessionByID(ctx context.Context, schoolID, id string) (*AttendanceSession, error)
	ListSessions(ctx context.Context, schoolID, classGroupID, date string, limit, offset int) ([]*AttendanceSession, int, error)

	// UpsertRecords applies a batch idempotently inside one transaction:
	// fresh inserts; corrections (same learner, new mutation id) update;
	// replays (same mutation id) are no-ops.
	UpsertRecords(ctx context.Context, schoolID, sessionID, recordedBy string, records []RecordInput) error
	RecordsForSession(ctx context.Context, schoolID, sessionID string) ([]*AttendanceRecord, error)

	// CloseSession applies the closed status; returns false when the session was
	// not open (already closed) — idempotent close semantics (#49).
	CloseSession(ctx context.Context, schoolID, id string) (bool, error)
	// RecordedLearnerIDs lists learner ids that already hold a record in the
	// session (replay path skips enrollment re-validation for them).
	RecordedLearnerIDs(ctx context.Context, schoolID, sessionID string) (map[string]bool, error)
	// EnrolledLearners filters the given learner ids to those with an open
	// enrollment at the school (issue #49: no cross-tenant attendance).
	EnrolledLearners(ctx context.Context, schoolID string, learnerIDs []string) (map[string]bool, error)
}
