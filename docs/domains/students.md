# Students Domain

**Owner:** `services/api/internal/students` · **Migration:** `20260909000004`

## Ownership

Global learner identity, guardians, guardian↔learner links (with access flags), and enrollments — the tenant binding with a lifecycle state machine.

## Model (spec §19)

- **Learners are deliberately NOT tenant-scoped**: a learner may enroll at multiple schools over time. The enrollment is the tenant binding.
- **Guardians** attach to learners via links; `can_view_financials` / `can_view_academics` are per-link flags consumed by guardian surfaces.

## Enrollment lifecycle

```
applicant → admitted → active → suspended → active …
 active   → transfer_pending → transferred_out | active
 active   → graduated → alumni
 any open → withdrawn (terminal)
```

- Terminal states: `transferred_out`, `withdrawn`, `alumni`.
- At most one OPEN enrollment per (school, learner) — partial unique index + service check.
- Transitions are optimistic-concurrency guarded (a racing writer surfaces as 409).

## Tenant visibility

Learners resolve through their enrollments: `LearnerInSchool` joins enrollments — a learner with no enrollment at the acting school does not exist for it (404, never 403).

## Events

`students.LearnerCreated.v1`, `students.GuardianCreated.v1`, `students.GuardianLinked.v1`, `students.LearnerEnrolled.v1`, `students.EnrollmentStateChanged.v1` (payload carries before/after).

## API surface

`/api/v1/learners*`, `/api/v1/guardians`, `/api/v1/enrollments*`.
