package attendance

import (
	"context"
	"errors"
	"strings"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgRepo struct {
	pool *postgres.Pool
}

// NewRepo builds the attendance repository.
func NewRepo(pool *postgres.Pool) Repo { return &pgRepo{pool: pool} }

func (r *pgRepo) ClassExists(ctx context.Context, schoolID, classGroupID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM class_groups WHERE id = $1 AND school_id = $2)`,
		classGroupID, schoolID).Scan(&exists)
	return exists, err
}

func (r *pgRepo) CreateSession(ctx context.Context, s *AttendanceSession) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO attendance_sessions (id, school_id, class_group_id, session_date, created_by)
                 VALUES ($1,$2,$3,$4::date,$5)
                 RETURNING id, school_id, class_group_id, session_date::text, status, created_by, created_at`,
		s.ID, s.SchoolID, s.ClassGroupID, s.Date, s.CreatedBy).
		Scan(&s.ID, &s.SchoolID, &s.ClassGroupID, &s.Date, &s.Status, &s.CreatedBy, &s.CreatedAt)
}

const sessionCols = `id, school_id, class_group_id, session_date::text, status, created_by, created_at`

func scanSession(row pgx.Row) (*AttendanceSession, error) {
	var s AttendanceSession
	err := row.Scan(&s.ID, &s.SchoolID, &s.ClassGroupID, &s.Date, &s.Status, &s.CreatedBy, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *pgRepo) FindSession(ctx context.Context, schoolID, classGroupID, date string) (*AttendanceSession, error) {
	return scanSession(r.pool.QueryRow(ctx,
		`SELECT `+sessionCols+` FROM attendance_sessions
                 WHERE school_id = $1 AND class_group_id = $2 AND session_date = $3::date`,
		schoolID, classGroupID, date))
}

func (r *pgRepo) SessionByID(ctx context.Context, schoolID, id string) (*AttendanceSession, error) {
	return scanSession(r.pool.QueryRow(ctx,
		`SELECT `+sessionCols+` FROM attendance_sessions WHERE id = $1 AND school_id = $2`, id, schoolID))
}

func (r *pgRepo) ListSessions(ctx context.Context, schoolID, classGroupID, date string, limit, offset int) ([]*AttendanceSession, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM attendance_sessions
                 WHERE school_id = $1
                   AND ($2::uuid IS NULL OR class_group_id = $2)
                   AND ($3::date IS NULL OR session_date = $3::date)`,
		schoolID, nullableUUID(classGroupID), nullableText(date)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+sessionCols+` FROM attendance_sessions
                 WHERE school_id = $1
                   AND ($2::uuid IS NULL OR class_group_id = $2)
                   AND ($3::date IS NULL OR session_date = $3::date)
                 ORDER BY session_date DESC, created_at DESC
                 LIMIT $4 OFFSET $5`,
		schoolID, nullableUUID(classGroupID), nullableText(date), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*AttendanceSession{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// UpsertRecords applies the batch in one transaction:
//   - fresh (session, learner) rows insert;
//   - corrections update when the incoming client_mutation_id differs;
//   - replays (same mutation id reaching the same row) are no-ops;
//   - a mutation id still carried by a different row fails loudly.
//
// The row KEEPS the FIRST mutation id that created it (the SET clause never
// touches client_mutation_id) — the currently-recorded id is what replay
// detection keys on.
func (r *pgRepo) UpsertRecords(ctx context.Context, schoolID, sessionID, recordedBy string, records []RecordInput) error {
	return r.pool.WithinTx(ctx, func(tx postgres.Querier) error {
		for _, in := range records {
			tag, err := tx.Exec(ctx,
				`INSERT INTO attendance_records
                                        (id, school_id, session_id, learner_id, status, reason, recorded_by, client_mutation_id)
                                 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
                                 ON CONFLICT (session_id, learner_id) DO UPDATE SET
                                        status = EXCLUDED.status,
                                        reason = EXCLUDED.reason,
                                        recorded_by = EXCLUDED.recorded_by,
                                        recorded_at = now()
                                 WHERE attendance_records.client_mutation_id IS DISTINCT FROM EXCLUDED.client_mutation_id`,
				uuid.NewString(), schoolID, sessionID, in.LearnerID, string(in.Status), in.Reason, recordedBy, in.ClientMutationID)
			if err != nil {
				if isUniqueViolation(err) {
					// client_mutation_id already used by a different row.
					return ErrMutationUsed
				}
				return err
			}
			if tag.RowsAffected() == 0 {
				// No-op: replay of an already-applied mutation for this row.
				continue
			}
		}
		return nil
	})
}

func (r *pgRepo) RecordsForSession(ctx context.Context, schoolID, sessionID string) ([]*AttendanceRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, school_id, session_id, learner_id, status, reason, recorded_by, client_mutation_id, recorded_at
                 FROM attendance_records
                 WHERE school_id = $1 AND session_id = $2
                 ORDER BY recorded_at`, schoolID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AttendanceRecord{}
	for rows.Next() {
		rec := &AttendanceRecord{}
		if err := rows.Scan(&rec.ID, &rec.SchoolID, &rec.SessionID, &rec.LearnerID, &rec.Status,
			&rec.Reason, &rec.RecordedBy, &rec.ClientMutationID, &rec.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *pgRepo) CloseSession(ctx context.Context, schoolID, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE attendance_sessions SET status = 'closed' WHERE id = $1 AND school_id = $2 AND status = 'open'`,
		id, schoolID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either unknown in this school or already closed.
		return ErrNotFound
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

func nullableUUID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
