# Academics Domain

**Owner:** `services/api/internal/academics` · **Migration:** `20260909000005`

## Ownership

Academic years, terms, subjects, class groups, class rosters, teaching assignments — the structural hub that attendance and assignments hang off.

## Rules

- Years: `start ≤ end` (CHECK + service), status `planning|active|closed`, unique name per school.
- Terms: inclusive ranges that MUST nest inside their year and MUST NOT overlap sibling terms — service-layer policy with explicit domain errors (`ErrTermOverlap`, `ErrOutsideYear`).
- Subjects: code unique per school (case-insensitive).
- Classes: name unique per (school, year, case-insensitive).
- Rosters: bulk add of 1..500 learner UUIDs inside one transaction; idempotent (`ON CONFLICT DO NOTHING`); learner existence guarded by FK to `students.learners`.
- Teaching assignments: unique per (class, subject); teacher existence guarded by FK to `identity.users`.

## Events

`academics.AcademicYearCreated.v1`, `academics.TermCreated.v1`, `academics.ClassCreated.v1`, `academics.TeacherAssigned.v1`.

## API surface

`/api/v1/academic-years*`, `/api/v1/terms`, `/api/v1/subjects`, `/api/v1/classes*` (roster, teachers).
