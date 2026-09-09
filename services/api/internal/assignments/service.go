package assignments

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

// Service implements assignments business rules.
type Service struct {
	repo Repo
	pool *postgres.Pool
}

func NewService(repo Repo, pool *postgres.Pool) *Service {
	return &Service{repo: repo, pool: pool}
}

// CreateAssignmentInput carries the creation fields.
type CreateAssignmentInput struct {
	ClassGroupID string
	SubjectID    string
	Title        string
	Instructions string
	DueDate      string // YYYY-MM-DD or ""
}

// CreateAssignment creates a DRAFT assignment owned by the acting teacher.
func (s *Service) CreateAssignment(ctx context.Context, schoolID, teacherID string, in CreateAssignmentInput) (*Assignment, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		return nil, fmt.Errorf("%w: title required (<=200 chars)", ErrValidation)
	}
	if in.ClassGroupID == "" || in.SubjectID == "" {
		return nil, fmt.Errorf("%w: classGroupId and subjectId required", ErrValidation)
	}
	if _, err := uuid.Parse(teacherID); err != nil {
		return nil, fmt.Errorf("%w: acting user is not a valid teacher id", ErrValidation)
	}
	var due *string
	if in.DueDate != "" {
		d, err := time.Parse("2006-01-02", in.DueDate)
		if err != nil {
			return nil, fmt.Errorf("%w: dueDate must be YYYY-MM-DD", ErrValidation)
		}
		// Due date cannot be before today (local convention: UTC day).
		today := time.Now().UTC().Truncate(24 * time.Hour)
		if d.Before(today) {
			return nil, ErrDueDatePast
		}
		due = &in.DueDate
	}
	a := &Assignment{
		ID:           uuid.NewString(),
		SchoolID:     schoolID,
		ClassGroupID: in.ClassGroupID,
		SubjectID:    in.SubjectID,
		TeacherID:    teacherID,
		Title:        in.Title,
		Instructions: in.Instructions,
		DueDate:      due,
		Status:       AssignmentDraft,
	}
	if err := s.repo.CreateAssignment(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Assignments lists assignments (optional class/status filters), paginated.
func (s *Service) Assignments(ctx context.Context, schoolID, classGroupID string, status *AssignmentStatus, limit, offset int) ([]*Assignment, int, error) {
	return s.repo.ListAssignments(ctx, schoolID, classGroupID, status, limit, offset)
}

// Assignment resolves one assignment in the school scope.
func (s *Service) Assignment(ctx context.Context, schoolID, id string) (*Assignment, error) {
	return s.repo.AssignmentByID(ctx, schoolID, id)
}

// PublishAssignment moves draft -> published (owner only, due date must not
// be in the past at publish time) and emits assignments.AssignmentPublished.v1.
func (s *Service) PublishAssignment(ctx context.Context, schoolID, actorID, id string) (*Assignment, error) {
	a, err := s.ownedBy(ctx, schoolID, actorID, id)
	if err != nil {
		return nil, err
	}
	if !CanTransitionAssignment(a.Status, AssignmentPublished) {
		return nil, fmt.Errorf("%w: cannot publish from %s", ErrIllegalAssignmentTransition, a.Status)
	}
	if a.DueDate != nil {
		if d, err := time.Parse("2006-01-02", *a.DueDate); err == nil {
			today := time.Now().UTC().Truncate(24 * time.Hour)
			if d.Before(today) {
				return nil, ErrDueDatePast
			}
		}
	}
	if err := s.repo.UpdateAssignmentStatus(ctx, schoolID, id, a.Status, AssignmentPublished); err != nil {
		return nil, err
	}
	s.emit(ctx, schoolID, id, "assignments.AssignmentPublished", map[string]any{
		"class_group_id": a.ClassGroupID,
		"subject_id":     a.SubjectID,
		"teacher_id":     a.TeacherID,
		"due_date":       a.DueDate,
	})
	return s.repo.AssignmentByID(ctx, schoolID, id)
}

// CloseAssignment moves published -> closed (owner only). Closing stops
// further submissions.
func (s *Service) CloseAssignment(ctx context.Context, schoolID, actorID, id string) (*Assignment, error) {
	a, err := s.ownedBy(ctx, schoolID, actorID, id)
	if err != nil {
		return nil, err
	}
	if !CanTransitionAssignment(a.Status, AssignmentClosed) {
		return nil, fmt.Errorf("%w: cannot close from %s", ErrIllegalAssignmentTransition, a.Status)
	}
	if err := s.repo.UpdateAssignmentStatus(ctx, schoolID, id, a.Status, AssignmentClosed); err != nil {
		return nil, err
	}
	return s.repo.AssignmentByID(ctx, schoolID, id)
}

// SubmitAssignment records (or refreshes) a learner's submission. The
// assignment must be published and the learner rostered in its class.
// NOTE: until identity↔learner linking exists, the caller supplies the
// learnerId; learner-facing restriction comes with that linking.
func (s *Service) SubmitAssignment(ctx context.Context, schoolID, assignmentID, learnerID, content string) (*Submission, error) {
	if learnerID == "" {
		return nil, fmt.Errorf("%w: learnerId required", ErrValidation)
	}
	if _, err := uuid.Parse(learnerID); err != nil {
		return nil, fmt.Errorf("%w: invalid learnerId", ErrValidation)
	}
	a, err := s.repo.AssignmentByID(ctx, schoolID, assignmentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: assignment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if a.Status != AssignmentPublished {
		return nil, ErrNotOpen
	}
	ok, err := s.repo.RosteredLearner(ctx, a.ClassGroupID, learnerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotRostered
	}
	sub := &Submission{
		ID:           uuid.NewString(),
		SchoolID:     schoolID,
		AssignmentID: assignmentID,
		LearnerID:    learnerID,
		Content:      strings.TrimSpace(content),
		Status:       SubmissionSubmitted,
	}
	if err := s.repo.UpsertSubmission(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// Submissions lists a submission set (owner teacher or school staff reads).
func (s *Service) Submissions(ctx context.Context, schoolID, actorID, assignmentID string) ([]*Submission, error) {
	if _, err := s.repo.AssignmentByID(ctx, schoolID, assignmentID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: assignment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	return s.repo.SubmissionsForAssignment(ctx, schoolID, assignmentID)
}

// GradeAssignment grades a submitted response — OWNER ONLY (issue #11
// security requirement). submitted -> graded, emits
// assignments.SubmissionGraded.v1.
func (s *Service) GradeAssignment(ctx context.Context, schoolID, actorID, assignmentID, learnerID, grade, feedback string) (*Submission, error) {
	if _, err := s.ownedBy(ctx, schoolID, actorID, assignmentID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(grade) == "" {
		return nil, ErrGradeRequired
	}
	sub, err := s.repo.GradeSubmission(ctx, schoolID, assignmentID, learnerID, strings.TrimSpace(grade), strings.TrimSpace(feedback))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: submission not found", ErrNotFound)
		}
		return nil, err
	}
	s.emit(ctx, schoolID, assignmentID, "assignments.SubmissionGraded", map[string]any{
		"learner_id": learnerID,
		"grade":      sub.Grade,
	})
	return sub, nil
}

// ReturnAssignment moves graded -> returned (owner only): the graded work
// becomes visible to the guardian/learner surfaces.
func (s *Service) ReturnAssignment(ctx context.Context, schoolID, actorID, assignmentID, learnerID string) (*Submission, error) {
	if _, err := s.ownedBy(ctx, schoolID, actorID, assignmentID); err != nil {
		return nil, err
	}
	sub, err := s.repo.ReturnSubmission(ctx, schoolID, assignmentID, learnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: submission not found", ErrNotFound)
		}
		return nil, err
	}
	return sub, nil
}

// ownedBy resolves the assignment and enforces ownership by the acting user.
func (s *Service) ownedBy(ctx context.Context, schoolID, actorID, id string) (*Assignment, error) {
	a, err := s.repo.AssignmentByID(ctx, schoolID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: assignment not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if a.TeacherID != actorID {
		return nil, ErrNotOwner
	}
	return a, nil
}

func (s *Service) emit(ctx context.Context, schoolID, aggregateID, eventType string, payload map[string]any) {
	if s.pool == nil {
		return
	}
	_, _ = events.Record(ctx, s.pool, &schoolID, aggregateID, eventType, 1, payload)
}
