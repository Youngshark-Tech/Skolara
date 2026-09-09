# Tenancy Domain

**Owner:** `services/api/internal/tenancy` · **Migration:** `20260909000003`

## Ownership

Education groups → schools (tenant root) → campuses; school memberships binding users to schools with school-scoped roles; the tenant resolution middleware (`RequireSchool`).

## Data

- `education_groups`, `schools` (code unique, case-insensitive), `campuses` (unique per school+code).
- `school_memberships` — PK (user, school, role); status `active|suspended`; roles reference the identity RBAC seed.
- `school_join_codes` — invite codes for onboarding (schema ready; API lands later).

## Rules

- Every tenant-scoped table across the platform carries `school_id` referencing this school.
- Tenant resolution: `X-School-ID` requires an ACTIVE membership or the platform-admin role; without a header, a single active membership is the unambiguous default.
- School access checks are deterministic: an any-active-row EXISTS over memberships (a suspended role never shadows an active one).
- Cross-tenant reads return 404 (never 403) to prevent enumeration.

## Events

`tenancy.SchoolCreated.v1`, `tenancy.MemberAdded.v1`, `tenancy.GroupCreated.v1`.

## API surface

`/api/v1/education-groups`, `/api/v1/schools*`, `/api/v1/me/memberships`.
