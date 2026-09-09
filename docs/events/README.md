# Event Catalog

Events follow the envelope in ADR-003: `event_id, type, schema_version, school_id, aggregate_id, occurred_at, actor, correlation_id, causation_id, payload`.

| Event | Version | Producer | Notable consumers |
|-------|---------|----------|-------------------|
| identity.UserCreated | v1 | identity | audit, notifications |
| identity.UserLoggedIn | v1 | identity | audit |
| tenancy.SchoolCreated | v1 | tenancy | finance (chart-of-accounts seed), audit |
| students.LearnerCreated | v1 | students | audit, notifications |
| students.EnrollmentStateChanged | v1 | students | notifications, audit |
| academics.ClassCreated | v1 | academics | audit |
| academics.TeacherAssigned | v1 | academics | workforce, notifications |
| attendance.AbsenceRecorded | v1 | attendance | notifications (guardian) |
| attendance.SessionClosed | v1 | attendance | analytics |
| assignments.AssignmentPublished | v1 | assignments | notifications (students/guardians) |
| assignments.SubmissionGraded | v1 | assignments | notifications |
| finance.InvoiceCreated | v1 | finance | notifications (guardian) |
| finance.PaymentConfirmed | v1 | finance | receipts, notifications |
| finance.ReceiptIssued | v1 | finance | notifications, audit |
