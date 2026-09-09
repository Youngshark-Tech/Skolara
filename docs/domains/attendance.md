# Attendance Domain

**Owner:** `services/api/internal/attendance` · **Migration:** `20260909000006`

## Ownership

Per-class daily attendance sessions and per-learner records with offline-tolerant idempotency (master spec §36, §46 sync groundwork).

## Model

- **AttendanceSession**: one roll call per (school, class, date) — unique in SQL, resolved convergently by the service (find-or-create), so offline retries always land on the same session.
- **AttendanceRecord**: one outcome per learner per session — `present | absent | late | excused` (+ free-text reason ≤ 200 chars).
- Lifecycle: `open → closed`; closed sessions reject further records (409).

## Offline idempotency (§46)

Every record carries a client-generated `clientMutationId`:

1. **Fresh** (session, learner) row → insert.
2. **Replay** (same mutation id reaching the row that carries it) → no-op.
3. **Correction** (same row, new mutation id) → update (status/reason/recorded_by).
4. **Reuse** of a mutation id still carried by a DIFFERENT row → rejected (`ErrMutationUsed`).

Rows keep the FIRST mutation id that created them — that id is the replay-detection key. A global unique index on `client_mutation_id` makes collisions detectable regardless of row.

## Events

`attendance.AbsenceRecorded.v1` (one per absent outcome — at-least-once; the future notification consumer must be idempotent), `attendance.SessionClosed.v1`.

## API surface

`/api/v1/attendance/sessions*` — open (200 convergent), list, records (bulk POST 204 / GET), close. Permissions: `attendance.record` (writes), `attendance.read` (reads).
