package academics

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

// Service implements academics business rules.
type Service struct {
	repo Repo
	pool *postgres.Pool
}

func NewService(repo Repo, pool *postgres.Pool) *Service {
	return &Service{repo: repo, pool: pool}
}

// maxRosterBatch bounds the bulk roster insert per request.
const maxRosterBatch = 500

// CreateAcademicYear validates and creates an academic year, emitting
// academics.AcademicYearCreated v1 scoped to the school.
func (s *Service) CreateAcademicYear(ctx context.Context, schoolID, name, start, end string) (*AcademicYear, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, fmt.Errorf("%w: year name required (<=64 chars)", ErrValidation)
	}
	if err := validateRange(start, end); err != nil {
		return nil, err
	}
	y := &AcademicYear{
		ID:        uuid.NewString(),
		SchoolID:  schoolID,
		Name:      name,
		StartDate: start,
		EndDate:   end,
		Status:    "planning",
	}
	if err := s.repo.CreateAcademicYear(ctx, schoolID, y); err != nil {
		return nil, err
	}
	s.emit(ctx, schoolID, y.ID, "academics.AcademicYearCreated",
		map[string]any{"name": y.Name, "start_date": y.StartDate, "end_date": y.EndDate})
	return y, nil
}

// AcademicYears lists the school's academic years.
func (s *Service) AcademicYears(ctx context.Context, schoolID string) ([]*AcademicYear, error) {
	return s.repo.ListAcademicYears(ctx, schoolID)
}

// AcademicYearByID resolves a year within the school scope.
func (s *Service) AcademicYearByID(ctx context.Context, schoolID, id string) (*AcademicYear, error) {
	return s.repo.AcademicYearByID(ctx, schoolID, id)
}

// CreateTerm validates and creates a term inside a year: the year must exist,
// the range must be well-formed, must nest in the year, and must not overlap
// any sibling term (inclusive bounds). Emits academics.TermCreated v1.
func (s *Service) CreateTerm(ctx context.Context, schoolID, yearID, name, start, end string) (*Term, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, fmt.Errorf("%w: term name required (<=64 chars)", ErrValidation)
	}
	if err := validateRange(start, end); err != nil {
		return nil, err
	}
	year, err := s.repo.AcademicYearByID(ctx, schoolID, yearID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: academic year not found in this school", ErrNotFound)
		}
		return nil, err
	}
	// Terms must nest inside their academic year (inclusive).
	if start < year.StartDate || end > year.EndDate {
		return nil, fmt.Errorf("%w: term must lie within %s..%s", ErrOutsideYear, year.StartDate, year.EndDate)
	}
	siblings, err := s.repo.TermsForYear(ctx, schoolID, yearID)
	if err != nil {
		return nil, err
	}
	for _, t := range siblings {
		// Inclusive overlap: NOT (new.end < t.start OR new.start > t.end).
		if !(end < t.StartDate || start > t.EndDate) {
			return nil, fmt.Errorf("%w: overlaps %s (%s..%s)", ErrTermOverlap, t.Name, t.StartDate, t.EndDate)
		}
	}
	term := &Term{
		ID:             uuid.NewString(),
		SchoolID:       schoolID,
		AcademicYearID: yearID,
		Name:           name,
		StartDate:      start,
		EndDate:        end,
	}
	if err := s.repo.CreateTerm(ctx, schoolID, term); err != nil {
		return nil, err
	}
	s.emit(ctx, schoolID, term.ID, "academics.TermCreated",
		map[string]any{"name": term.Name, "academic_year_id": yearID, "start_date": start, "end_date": end})
	return term, nil
}

// TermsForYear lists a year's terms.
func (s *Service) TermsForYear(ctx context.Context, schoolID, yearID string) ([]*Term, error) {
	return s.repo.TermsForYear(ctx, schoolID, yearID)
}

// CreateSubject validates and creates a subject (code unique per school).
func (s *Service) CreateSubject(ctx context.Context, schoolID, code, name string) (*Subject, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" || len(code) > 32 {
		return nil, fmt.Errorf("%w: subject code required (<=32 chars)", ErrValidation)
	}
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: subject name required (<=128 chars)", ErrValidation)
	}
	sub := &Subject{ID: uuid.NewString(), SchoolID: schoolID, Code: code, Name: name}
	if err := s.repo.CreateSubject(ctx, schoolID, sub); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCodeTaken
		}
		return nil, err
	}
	return sub, nil
}

// Subjects lists the school's subjects.
func (s *Service) Subjects(ctx context.Context, schoolID string) ([]*Subject, error) {
	return s.repo.ListSubjects(ctx, schoolID)
}

// SubjectByID resolves a subject within the school scope.
func (s *Service) SubjectByID(ctx context.Context, schoolID, id string) (*Subject, error) {
	return s.repo.SubjectByID(ctx, schoolID, id)
}

// CreateClassGroup validates and creates a class for a year, emitting
// academics.ClassCreated v1.
func (s *Service) CreateClassGroup(ctx context.Context, schoolID, yearID, name string) (*ClassGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: class name required (<=100 chars)", ErrValidation)
	}
	if _, err := s.repo.AcademicYearByID(ctx, schoolID, yearID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: academic year not found in this school", ErrNotFound)
		}
		return nil, err
	}
	c := &ClassGroup{ID: uuid.NewString(), SchoolID: schoolID, AcademicYearID: yearID, Name: name}
	if err := s.repo.CreateClassGroup(ctx, schoolID, c); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	s.emit(ctx, schoolID, c.ID, "academics.ClassCreated",
		map[string]any{"name": c.Name, "academic_year_id": yearID})
	return c, nil
}

// ClassGroups lists a school's classes, optionally filtered by year.
func (s *Service) ClassGroups(ctx context.Context, schoolID, yearID string) ([]*ClassGroup, error) {
	return s.repo.ListClassGroups(ctx, schoolID, yearID)
}

// ClassGroupByID resolves a class within the school scope.
func (s *Service) ClassGroupByID(ctx context.Context, schoolID, id string) (*ClassGroup, error) {
	return s.repo.ClassGroupByID(ctx, schoolID, id)
}

// AddRoster seats learners in a class (1..500 ids, all valid UUIDs). The
// insert is idempotent. Seats are enrollment-scoped (issue #42): only
// learners with an open enrollment at the acting school can be seated —
// learner identities are global, so seat-any-learner would leak PII across
// tenants via the roster listing. Unenrolled/unknown ids are rejected as
// ErrValidation without revealing which school (if any) they belong to.
func (s *Service) AddRoster(ctx context.Context, schoolID, classGroupID string, learnerIDs []string) error {
	if len(learnerIDs) == 0 || len(learnerIDs) > maxRosterBatch {
		return ErrBatchSize
	}
	for _, id := range learnerIDs {
		if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
			return fmt.Errorf("%w: invalid learner id %q", ErrValidation, id)
		}
	}
	if _, err := s.repo.ClassGroupByID(ctx, schoolID, classGroupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: class not found in this school", ErrNotFound)
		}
		return err
	}
	trimmed := make([]string, 0, len(learnerIDs))
	for _, id := range learnerIDs {
		trimmed = append(trimmed, strings.TrimSpace(id))
	}
	rejected, err := s.repo.AddRosterEntries(ctx, schoolID, classGroupID, trimmed)
	if err != nil {
		if isFKViolation(err) {
			return fmt.Errorf("%w: one or more learner ids do not exist", ErrValidation)
		}
		return err
	}
	if len(rejected) > 0 {
		return fmt.Errorf("%w: %d learner(s) not enrolled at this school (first: %s)",
			ErrValidation, len(rejected), rejected[0])
	}
	return nil
}

// Roster lists a class's seats (learner identities joined for display).
func (s *Service) Roster(ctx context.Context, schoolID, classGroupID string) ([]*RosterEntryView, error) {
	if _, err := s.repo.ClassGroupByID(ctx, schoolID, classGroupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: class not found in this school", ErrNotFound)
		}
		return nil, err
	}
	return s.repo.RosterForClass(ctx, schoolID, classGroupID)
}

// AssignTeacher binds a teacher to a class+subject pair, emitting
// academics.TeacherAssigned v1. Teacher existence is guarded by the DB FK
// (identity.users.id) — FK violations map to ErrValidation (400).
func (s *Service) AssignTeacher(ctx context.Context, schoolID, classGroupID, subjectID, teacherID string) (*TeachingAssignment, error) {
	if subjectID == "" || teacherID == "" {
		return nil, fmt.Errorf("%w: subjectId and teacherId required", ErrValidation)
	}
	if _, err := uuid.Parse(teacherID); err != nil {
		return nil, fmt.Errorf("%w: invalid teacherId", ErrValidation)
	}
	if _, err := s.repo.ClassGroupByID(ctx, schoolID, classGroupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: class not found in this school", ErrNotFound)
		}
		return nil, err
	}
	if _, err := s.repo.SubjectByID(ctx, schoolID, subjectID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: subject not found in this school", ErrNotFound)
		}
		return nil, err
	}
	a := &TeachingAssignment{
		ID:           uuid.NewString(),
		SchoolID:     schoolID,
		ClassGroupID: classGroupID,
		SubjectID:    subjectID,
		TeacherID:    teacherID,
	}
	if err := s.repo.CreateTeachingAssignment(ctx, schoolID, a); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAssignmentExists
		}
		if isFKViolation(err) {
			return nil, fmt.Errorf("%w: teacher %s does not exist", ErrValidation, teacherID)
		}
		return nil, err
	}
	s.emit(ctx, schoolID, a.ID, "academics.TeacherAssigned",
		map[string]any{"class_group_id": classGroupID, "subject_id": subjectID, "teacher_id": teacherID})
	return a, nil
}

// AssignmentsForClass lists a class's teaching assignments (teacher+subject
// identity joined for display).
func (s *Service) AssignmentsForClass(ctx context.Context, schoolID, classGroupID string) ([]*TeachingAssignmentView, error) {
	if _, err := s.repo.ClassGroupByID(ctx, schoolID, classGroupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: class not found in this school", ErrNotFound)
		}
		return nil, err
	}
	return s.repo.AssignmentsForClass(ctx, schoolID, classGroupID)
}

// emit records an outbox event best-effort — like tenancy/students, a failed
// event write must not fail the committed business operation.
func (s *Service) emit(ctx context.Context, schoolID, aggregateID, eventType string, payload map[string]any) {
	if s.pool == nil {
		return
	}
	_, _ = events.Record(ctx, s.pool, &schoolID, aggregateID, eventType, 1, payload)
}

// validateRange checks both dates parse as strict YYYY-MM-DD and end >= start.
func validateRange(start, end string) error {
	s, err1 := time.Parse("2006-01-02", start)
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("%w: dates must be YYYY-MM-DD", ErrValidation)
	}
	if e.Before(s) {
		return fmt.Errorf("%w: %s .. %s", ErrDateRange, start, end)
	}
	return nil
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
