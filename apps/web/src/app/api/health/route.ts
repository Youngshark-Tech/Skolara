/**
 * Liveness probe for container orchestrators (issue #56). Deliberately
 * dependency-free: no auth, no upstream API call, no DB — it answers "is the
 * web server up", nothing more.
 */
export function GET(): Response {
  return Response.json({ status: "ok" });
}
