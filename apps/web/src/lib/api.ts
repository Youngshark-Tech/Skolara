/**
 * Typed fetch client for the Skolara API.
 *
 * Auth model (ADR-011): the API sets a HttpOnly refresh cookie
 * (`skolara_refresh`, see services/api/internal/identity/http.go); the
 * short-lived access token lives in MEMORY ONLY — it is never persisted, so an
 * XSS payload cannot steal a long-lived session. On boot the app exchanges the
 * refresh cookie for a fresh access token (silent refresh). On a 401 the
 * client performs exactly one single-flight refresh-and-retry: concurrent 401s
 * share the same in-flight refresh instead of racing (which used to present an
 * already-rotated refresh token and trip family revocation). Tenant context is
 * sent via X-School-ID (server derives it from the verified session, never
 * payloads).
 */

/**
 * Resolve the API base URL (issue #127):
 * - An explicitly set NEXT_PUBLIC_API_URL always wins. It may be a full URL
 *   (split deployment, e.g. "https://api.example.com") or an empty string
 *   (same-origin calls behind a reverse proxy / Vercel Services rewrite).
 * - Otherwise production builds default to SAME-ORIGIN (""): the Vercel
 *   Services rewrite routes /api/* to the Go service on the shared domain, so
 *   relative URLs are correct and the refresh cookie stays first-party.
 * - Dev keeps the local Go server default of http://localhost:8080.
 * A localhost default previously left production browsers calling the
 * user's own machine — why login silently failed on the first Vercel deploy.
 */
export function resolveApiUrl(
  explicit: string | undefined,
  isProduction: boolean,
): string {
  if (explicit !== undefined) return explicit;
  return isProduction ? "" : "http://localhost:8080";
}

export const API_URL = resolveApiUrl(
  process.env.NEXT_PUBLIC_API_URL,
  process.env.NODE_ENV === "production",
);

const SCHOOL_KEY = "skolara_school_id";

/**
 * Access token, module-scoped and never persisted. `null` before the first
 * successful login/silent refresh and after logout/session expiry.
 */
let accessToken: string | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

/**
 * The selected school is a non-secret UI preference (which tenant workspace to
 * open); it is the ONLY value kept in localStorage.
 */
export function getSchoolId(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(SCHOOL_KEY);
}

export function setSchoolId(id: string | null): void {
  if (typeof window === "undefined") return;
  if (id) window.localStorage.setItem(SCHOOL_KEY, id);
  else window.localStorage.removeItem(SCHOOL_KEY);
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Skip the automatic refresh-and-retry (used by the login call itself). */
  noRetry?: boolean;
  /**
   * Abort a request that a newer one supersedes (search-as-you-type #57):
   * the fetch rejects with an AbortError which callers must ignore.
   */
  signal?: AbortSignal;
  /**
   * Observe the final Response of a successful request (after any 401
   * refresh-and-retry) — used to read response headers like X-Total-Count
   * that the JSON envelope does not carry (#58 pagination).
   */
  onResponse?: (res: Response) => void;
}

export interface SessionResponse {
  accessToken: string;
  tokenType: string;
  expiresIn: number;
}

/**
 * Normalized refresh outcome (never throws — network failures used to escape
 * as raw TypeErrors): `unauthenticated` means the refresh cookie was rejected
 * (session definitively over); `network` means the call could not complete and
 * is transient.
 */
export type RefreshOutcome =
  | { ok: true; session: SessionResponse }
  | { ok: false; reason: "unauthenticated" | "network" };

/**
 * Single-flight refresh: one shared in-flight promise for all concurrent 401s.
 * The refresh endpoint ROTATES the refresh cookie on every call and revokes
 * the token family on reuse — concurrent refreshes would present an
 * already-used token and hard-logout the user.
 */
let refreshInFlight: Promise<RefreshOutcome> | null = null;

async function doRefresh(): Promise<RefreshOutcome> {
  try {
    const res = await fetch(`${API_URL}/api/v1/auth/refresh`, {
      method: "POST",
      credentials: "include",
    });
    if (!res.ok) return { ok: false, reason: "unauthenticated" };
    const session = (await res.json()) as SessionResponse;
    return { ok: true, session };
  } catch {
    return { ok: false, reason: "network" };
  }
}

export function refreshSession(): Promise<RefreshOutcome> {
  if (!refreshInFlight) {
    refreshInFlight = doRefresh().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

/**
 * Session-expired hook consumed by SessionProvider: invoked exactly when the
 * session is DEFINITIVELY over (refresh cookie rejected), so the provider can
 * reset its state and route to /login. Never fires for transient network
 * failures or for plain 401s like a rejected login attempt.
 */
let onSessionExpired: (() => void) | null = null;

export function setOnSessionExpired(handler: (() => void) | null): void {
  onSessionExpired = handler;
}

function notifySessionExpired(): void {
  onSessionExpired?.();
}

/**
 * Perform an API request with bearer auth, tenant header, and one
 * single-flight refresh-and-retry on 401.
 */
export async function apiFetch<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { method = "GET", body, noRetry, signal, onResponse } = options;

  const doFetch = () => {
    const headers: Record<string, string> = {};
    const token = getAccessToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    const school = getSchoolId();
    if (school) headers["X-School-ID"] = school;
    if (body !== undefined) headers["Content-Type"] = "application/json";
    return fetch(`${API_URL}${path}`, {
      method,
      headers,
      credentials: "include",
      signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  };

  let res = await doFetch();

  // One refresh-and-retry; concurrent 401s coalesce onto the same refresh.
  // `sessionDefinitivelyOver` distinguishes "refresh cookie rejected" (logout)
  // from transient failures (surface the error, keep the session).
  let sessionDefinitivelyOver = false;
  if (res.status === 401 && !noRetry) {
    const outcome = await refreshSession();
    if (outcome.ok) {
      setAccessToken(outcome.session.accessToken);
      res = await doFetch();
      if (res.status === 401) sessionDefinitivelyOver = true;
    } else if (outcome.reason === "unauthenticated") {
      sessionDefinitivelyOver = true;
    }
    // reason === "network": transient — the 401 below is surfaced as-is.
  }

  onResponse?.(res);

  if (res.status === 204) return undefined as T;

  const payload = await res.json().catch(() => null);

  if (res.ok && payload === null) {
    // 2xx with a non-JSON body (issue #136): a proxy or error page answered
    // instead of the API (e.g. a same-origin deployment without the API
    // rewrite — the login POST used to surface as a raw TypeError from the
    // caller's property access). Never resolve a typed payload of null.
    throw new ApiError(
      502,
      "bad_response",
      "The server returned an unexpected response — if this persists, contact your administrator.",
    );
  }

  if (!res.ok) {
    if (res.status === 401) setAccessToken(null);
    if (sessionDefinitivelyOver) {
      // Session over: drop the tenant preference too (shared-machine leak) and
      // let the provider reset + redirect.
      setSchoolId(null);
      notifySessionExpired();
    }
    const envelope = payload as { error?: { code?: string; message?: string } } | null;
    throw new ApiError(
      res.status,
      envelope?.error?.code ?? "unknown",
      envelope?.error?.message ?? `request failed with status ${res.status}`,
    );
  }
  return payload as T;
}
