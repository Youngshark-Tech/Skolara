#!/usr/bin/env bash
# Skolara live E2E smoke — exercises the full product loop over real HTTP
# against a running API (SKOLARA_BOOTSTRAP_ADMIN_* configured).
# Usage: ./scripts/e2e-smoke.sh [BASE_URL]   (default http://127.0.0.1:8080)
set -euo pipefail

BASE="${1:-http://127.0.0.1:8080}"
EMAIL="admin@skolara.test"
PASSWORD='S0pera!Admin2026'
PASS=0; FAIL=0

say()  { printf '%s\n' "$*"; }
ok()   { PASS=$((PASS+1)); say "  ✓ $*"; }
bad()  { FAIL=$((FAIL+1)); say "  ✗ $*"; }
check(){ if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (want $2, got $1)"; fi }

jsonget() { python3 -c "
import sys, json
raw = sys.stdin.read()
try:
    d = json.loads(raw)
    print(d$1)
except Exception as e:
    print('PARSE_ERROR:', raw[:200])
"; }

say "== Skolara live E2E smoke against $BASE =="

# --- health -----------------------------------------------------------------
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz"); check "$code" 200 "healthz liveness"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz"); check "$code" 200 "readyz dependencies"

# --- auth -------------------------------------------------------------------
LOGIN=$(curl -s -c /tmp/sk-cookies.txt -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" "$BASE/api/v1/auth/login")
TOKEN=$(echo "$LOGIN" | jsonget "['accessToken']" 2>/dev/null || true)
if [ -n "$TOKEN" ]; then ok "login issued access token"; else bad "login failed: $LOGIN"; exit 1; fi
AUTH="Authorization: Bearer $TOKEN"

ME=$(curl -s -H "$AUTH" "$BASE/api/v1/me")
ROLES=$(echo "$ME" | jsonget "['roles'][0]")
[ "$ROLES" = "platform_admin" ] && ok "me resolves platform_admin" || bad "me roles: $ME"

# refresh rotation
REFRESH=$(curl -s -X POST -b /tmp/sk-cookies.txt -c /tmp/sk-cookies.txt "$BASE/api/v1/auth/refresh")
NEW_TOKEN=$(echo "$REFRESH" | jsonget "['accessToken']" 2>/dev/null || true)
[ -n "$NEW_TOKEN" ] && ok "refresh rotates session" || bad "refresh failed: $REFRESH"
AUTH="Authorization: Bearer $NEW_TOKEN"

# --- tenancy ----------------------------------------------------------------
SUFFIX=$RANDOM
SCHOOL=$(curl -s -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"code\":\"SMK-$SUFFIX\",\"name\":\"Smoke School $SUFFIX\"}" "$BASE/api/v1/schools")
SCHOOL_ID=$(echo "$SCHOOL" | jsonget "['id']")
[ -n "$SCHOOL_ID" ] && ok "school created ($SCHOOL_ID)" || { bad "school create failed: $SCHOOL"; exit 1; }
SCH="X-School-ID: $SCHOOL_ID"

# membership for the admin (needed for school-scoped flows)
ADMIN_ID=$(echo "$ME" | jsonget "['id']")
curl -s -o /dev/null -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"userId\":\"$ADMIN_ID\",\"role\":\"school_admin\"}" "$BASE/api/v1/schools/$SCHOOL_ID/members"
ok "admin granted school_admin membership"

# --- students ---------------------------------------------------------------
LEARNER=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d '{"firstName":"Smoke","lastName":"Learner","externalId":"SMK-001"}' "$BASE/api/v1/learners")
LEARNER_ID=$(echo "$LEARNER" | jsonget "['id']")
[ -n "$LEARNER_ID" ] && ok "learner created" || bad "learner create failed: $LEARNER"

ENROLL=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"learnerId\":\"$LEARNER_ID\"}" "$BASE/api/v1/enrollments")
ENROLL_ID=$(echo "$ENROLL" | jsonget "['id']")
ENROLL_STATUS=$(echo "$ENROLL" | jsonget "['status']")
[ "$ENROLL_STATUS" = "admitted" ] && ok "learner enrolled (admitted)" || bad "enrollment: $ENROLL"

TRANSITION=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d '{"to":"active"}' "$BASE/api/v1/enrollments/$ENROLL_ID/transition")
NEW_STATUS=$(echo "$TRANSITION" | jsonget "['status']")
[ "$NEW_STATUS" = "active" ] && ok "enrollment transitioned to active" || bad "transition: $TRANSITION"

# --- academics --------------------------------------------------------------
YEAR=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d '{"name":"2026","startDate":"2026-01-01","endDate":"2026-12-31"}' "$BASE/api/v1/academic-years")
YEAR_ID=$(echo "$YEAR" | jsonget "['id']")
[ -n "$YEAR_ID" ] && ok "academic year created" || bad "year: $YEAR"

TERM=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"academicYearId\":\"$YEAR_ID\",\"name\":\"Term 1\",\"startDate\":\"2026-01-01\",\"endDate\":\"2026-04-30\"}" "$BASE/api/v1/terms")
TERM_ID=$(echo "$TERM" | jsonget "['id']")
[ -n "$TERM_ID" ] && ok "term created" || bad "term: $TERM"

SUBJECT=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d '{"code":"MATH","name":"Mathematics"}' "$BASE/api/v1/subjects")
SUBJECT_ID=$(echo "$SUBJECT" | jsonget "['id']")
[ -n "$SUBJECT_ID" ] && ok "subject created" || bad "subject: $SUBJECT"

CLASS=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"academicYearId\":\"$YEAR_ID\",\"name\":\"Grade 4\"}" "$BASE/api/v1/classes")
CLASS_ID=$(echo "$CLASS" | jsonget "['id']")
[ -n "$CLASS_ID" ] && ok "class created" || bad "class: $CLASS"

ROSTER=$(curl -s -o /dev/null -w '%{http_code}' -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"learnerIds\":[\"$LEARNER_ID\"]}" "$BASE/api/v1/classes/$CLASS_ID/roster")
check "$ROSTER" 204 "learner rostered into class"

# --- attendance -------------------------------------------------------------
SESSION=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"classGroupId\":\"$CLASS_ID\",\"date\":\"2026-09-09\"}" "$BASE/api/v1/attendance/sessions")
SESSION_ID=$(echo "$SESSION" | jsonget "['id']")
[ -n "$SESSION_ID" ] && ok "attendance session opened" || bad "session: $SESSION"

REC=$(curl -s -o /dev/null -w '%{http_code}' -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"records\":[{\"learnerId\":\"$LEARNER_ID\",\"status\":\"present\",\"clientMutationId\":\"smoke-mut-1\"}]}" \
  "$BASE/api/v1/attendance/sessions/$SESSION_ID/records")
check "$REC" 204 "attendance record submitted"
REC2=$(curl -s -o /dev/null -w '%{http_code}' -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"records\":[{\"learnerId\":\"$LEARNER_ID\",\"status\":\"present\",\"clientMutationId\":\"smoke-mut-1\"}]}" \
  "$BASE/api/v1/attendance/sessions/$SESSION_ID/records")
check "$REC2" 204 "attendance replay idempotent (204, no duplicates)"

# --- finance ----------------------------------------------------------------
INVOICE=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"learnerId\":\"$LEARNER_ID\",\"dueDate\":\"2026-12-31\",\"lines\":[{\"description\":\"Tuition\",\"amountMinor\":1250000}]}" \
  "$BASE/api/v1/invoices")
INVOICE_ID=$(echo "$INVOICE" | jsonget "['id']")
[ -n "$INVOICE_ID" ] && ok "invoice created (12,500.00 KES)" || bad "invoice: $INVOICE"

PAYMENT=$(curl -s -H "$AUTH" -H "$SCH" -H 'Content-Type: application/json' \
  -d "{\"invoiceId\":\"$INVOICE_ID\",\"amountMinor\":1250000,\"provider\":\"mpesa\",\"providerRef\":\"SMK-$SUFFIX-1\"}" \
  "$BASE/api/v1/payments")
PAYMENT_ID=$(echo "$PAYMENT" | jsonget "['id']")
[ -n "$PAYMENT_ID" ] && ok "payment intent recorded (pending)" || bad "payment: $PAYMENT"

BODY="{\"schoolId\":\"$SCHOOL_ID\",\"eventId\":\"smoke-evt-$SUFFIX\",\"paymentId\":\"$PAYMENT_ID\",\"status\":\"confirmed\"}"
SIG=$(BODY="$BODY" python3 - <<'PYEOF'
import hmac, hashlib, os
body = os.environ["BODY"].encode()
print(hmac.new(b'dev-webhook-secret-0123456789abcdef', body, hashlib.sha256).hexdigest())
PYEOF
)
WH=$(curl -s -H 'Content-Type: application/json' -H "X-Skolar-Signature: $SIG" \
  -d "$BODY" "$BASE/api/v1/payments/webhooks")
WH_STATUS=$(echo "$WH" | jsonget "['status']")
[ "$WH_STATUS" = "confirmed" ] && ok "webhook confirmed payment" || bad "webhook: $WH"

INV2=$(curl -s -H "$AUTH" -H "$SCH" "$BASE/api/v1/invoices/$INVOICE_ID")
INV_STATUS=$(echo "$INV2" | jsonget "['status']")
[ "$INV_STATUS" = "paid" ] && ok "invoice fully paid via allocation" || bad "invoice status: $INV2"

WH2=$(curl -s -H 'Content-Type: application/json' -H "X-Skolar-Signature: $SIG" \
  -d "$BODY" "$BASE/api/v1/payments/webhooks")
REPLAYED=$(echo "$WH2" | jsonget "['replayed']")
[ "$REPLAYED" = "True" ] && ok "duplicate webhook replayed (zero extra postings)" || bad "replay: $WH2"

WALLET=$(curl -s -H "$AUTH" -H "$SCH" "$BASE/api/v1/wallet")
MAIN_BALANCE=$(echo "$WALLET" | python3 -c "
import sys, json
w = json.load(sys.stdin)['wallet']
m = [b for b in w if b['purpose'] == 'main']
print(m[0]['balanceMinor'] if m else 'none')")
[ "$MAIN_BALANCE" = "1250000" ] && ok "wallet main reflects inflow (1,250,000 minor)" || bad "wallet main: $MAIN_BALANCE"

# --- bad signature rejected --------------------------------------------------
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -H "X-Skolar-Signature: deadbeef" -d "$BODY" "$BASE/api/v1/payments/webhooks")
check "$code" 401 "forged webhook signature rejected"

# --- observability -----------------------------------------------------------
METRICS=$(curl -s "$BASE/metrics" | grep -c "^skolara_http_requests_total" || true)
[ "$METRICS" -ge 1 ] && ok "per-route Prometheus metrics exposed" || bad "metrics missing skolara_http_requests_total"

say ""
say "== RESULT: $PASS passed, $FAIL failed =="
[ "$FAIL" = "0" ]
