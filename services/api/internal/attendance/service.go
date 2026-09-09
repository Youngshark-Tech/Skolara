package attendance

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

// Service implements attendance business rules.
type Service struct {
	repo Repo
	pool *postgres.Pool
}

func NewService(repo Repo, pool *postgres.Pool) *Service {
	return &Service{repo: repo, pool: pool}
}

// OpenSession resolves the session for a class+date, creating it when absent.
// Retries (offline sync) converge on the same session instead of erroring —
// the idempotency scope of records is the session, so replay-safe resolution
// here is what makes the whole flow retry-tolerant.
func (s *Service) OpenSession(ctx context.Context, schoolID, classGroupID, date, actorID string) (*AttendanceSession, error) {
	if classGroupID == "" {
		return nil, fmt.Errorf("%w: classGroupId required", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("%w: date must be YYYY-MM-DD", ErrValidation)
	}
	ok, err := s.repo.ClassExists(ctx, schoolID, classGroupID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: class not found in this school", ErrNotFound)
	}
	if existing, err := s.repo.FindSession(ctx, schoolID, classGroupID, date); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	sess := &AttendanceSession{
		ID:           uuid.NewString(),
		SchoolID:     schoolID,
		ClassGroupID: classGroupID,
		Date:         date,
		Status:       SessionOpen,
		CreatedBy:    actorID,
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		// A concurrent creator won the race: converge on the existing row.
		if existing, findErr := s.repo.FindSession(ctx, schoolID, classGroupID, date); findErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return sess, nil
}

// Sessions lists sessions with optional class/date filters, paginated.
func (s *Service) Sessions(ctx context.Context, schoolID, classGroupID, date string, limit, offset int) ([]*AttendanceSession, int, error) {
	return s.repo.ListSessions(ctx, schoolID, classGroupID, date, limit, offset)
}

// Session resolves one session within the school scope.
func (s *Service) Session(ctx context.Context, schoolID, id string) (*AttendanceSession, error) {
	return s.repo.SessionByID(ctx, schoolID, id)
}

// SubmitRecords bulk-upserts records for a session (1..500). The session must
// be open and belong to the school. Every absent outcome emits
// attendance.AbsenceRecorded.v1 (at-least-once; consumers idempotent).
func (s *Service) SubmitRecords(ctx context.Context, schoolID, sessionID, actorID string, records []RecordInput) error {
	if len(records) == 0 || len(records) > maxRecordBatch {
		return fmt.Errorf("%w: records batch must contain 1..500 entries", ErrValidation)
	}
	seenMutations := map[string]bool{}
	for i := range records {
		in := &records[i]
		in.LearnerID = strings.TrimSpace(in.LearnerID)
		in.ClientMutationID = strings.TrimSpace(in.ClientMutationID)
		in.Reason = strings.TrimSpace(in.Reason)
		if _, err := uuid.Parse(in.LearnerID); err != nil {
			return fmt.Errorf("%w: invalid learnerId %q", ErrValidation, in.LearnerID)
		}
		if in.ClientMutationID == "" || len(in.ClientMutationID) > 128 {
			return fmt.Errorf("%w: clientMutationId required (<=128 chars)", ErrValidation)
		}
		if seenMutations[in.ClientMutationID] {
			return fmt.Errorf("%w: duplicate clientMutationId %q in batch", ErrValidation, in.ClientMutationID)
		}
		seenMutations[in.ClientMutationID] = true
		if !ValidRecordStatus(in.Status) {
			return fmt.Errorf("%w: status must be present|absent|late|excused", ErrValidation)
		}
		if len(in.Reason) > 200 {
			return fmt.Errorf("%w: reason must be <=200 chars", ErrValidation)
		}
	}
	sess, err := s.repo.SessionByID(ctx, schoolID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: session not found in this school", ErrNotFound)
		}
		return err
	}
	if sess.Status != SessionOpen {
		return ErrSessionClosed
	}
	if err := s.repo.UpsertRecords(ctx, schoolID, sessionID, actorID, records); err != nil {
		return err
	}
	// Absence notifications: one event per absent outcome (at-least-once).
	for _, in := range records {
		if in.Status != StatusAbsent {
			continue
		}
		s.emit(ctx, schoolID, sessionID, "attendance.AbsenceRecorded", map[string]any{
			"learner_id":     in.LearnerID,
			"class_group_id": sess.ClassGroupID,
			"date":           sess.Date,
			"status":         string(in.Status),
			"reason":         in.Reason,
		})
	}
	return nil
}

// Records lists a session's records (school-scoped).
func (s *Service) Records(ctx context.Context, schoolID, sessionID string) ([]*AttendanceRecord, error) {
	if _, err := s.repo.SessionByID(ctx, schoolID, sessionID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: session not found in this school", ErrNotFound)
		}
		return nil, err
	}
	return s.repo.RecordsForSession(ctx, schoolID, sessionID)
}

// CloseSession closes a session (finalizes the roll call) and emits
// attendance.SessionClosed.v1.
func (s *Service) CloseSession(ctx context.Context, schoolID, sessionID string) (*AttendanceSession, error) {
	sess, err := s.repo.SessionByID(ctx, schoolID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: session not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if err := s.repo.CloseSession(ctx, schoolID, sessionID); err != nil {
		return nil, err
	}
	s.emit(ctx, schoolID, sessionID, "attendance.SessionClosed", map[string]any{
		"class_group_id": sess.ClassGroupID,
		"date":           sess.Date,
	})
	return s.repo.SessionByID(ctx, schoolID, sessionID)
}

func (s *Service) emit(ctx context.Context, schoolID, aggregateID, eventType string, payload map[string]any) {
	if s.pool == nil {
		return
	}
	_, _ = events.Record(ctx, s.pool, &schoolID, aggregateID, eventType, 1, payload)
}
