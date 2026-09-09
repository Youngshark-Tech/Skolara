# Assignments Domain

**Owner:** `services/api/internal/assignments` · **Migration:** `20260909000007`

## Ownership

Assignment workflow and submissions (master spec §34): teacher creates → publishes → learners submit → teacher grades → returns.

## Lifecycles

- **Assignment**: `draft → published → closed` (linear; closed is terminal).
- **Submission**: `submitted → graded → returned` (linear).

## Rules

- **Ownership**: the creating teacher is the owner; ONLY the owner publishes, closes, grades, and returns (`ErrNotOwner` → 403).
- **Due dates**: rejected in the past at creation AND at publish.
- **Submissions**: only while the assignment is `published` and the learner is rostered in the assignment's class (roster FK check — no cross-domain import). Resubmission refreshes content while `submitted`; frozen after grading.
- **Grading**: grade required; only `submitted` rows can be graded; only `graded` rows can be returned.

## Events

`assignments.AssignmentPublished.v1` (class, subject, teacher, due date), `assignments.SubmissionGraded.v1` (learner, grade) — notification groundwork.

## Security notes

Learner submissions currently arrive with an explicit `learnerId` (validated against the roster) because identity↔learner linking does not exist yet. When learner accounts land, this endpoint must restrict `learnerId` to the acting user.

## API surface

`/api/v1/assignments*` (+ `/publish`, `/close`, `/submissions*`, `/grade`, `/return`).
