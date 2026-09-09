/**
 * Typed fetch client for the Skolara API.
 *
 * Auth model: the API sets a HttpOnly refresh cookie; the short-lived access
 * token lives in memory/localStorage. On a 401 the client attempts ONE
 * refresh-and-retry before surfacing the error. Tenant context is sent via
 * X-School-ID (server derives it from the verified session, never payloads).
 */

export const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const TOKEN_KEY = "skolara_access_token";
const SCHOOL_KEY = "skolara_school_id";

export function getAccessToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function setAccessToken(token: string | null): void {
  if (typeof window === "undefined") return;
  if (token) window.localStorage.setItem(TOKEN_KEY, token);
  else window.localStorage.removeItem(TOKEN_KEY);
}

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
  /** Skip the automatic refresh-and-retry (used by the refresh call itself). */
  noRetry?: boolean;
}

export interface SessionResponse {
  accessToken: string;
  tokenType: string;
  expiresIn: number;
}

/** Exchange the refresh cookie for a fresh access token. */
export async function refreshSession(): Promise<SessionResponse | null> {
  const res = await fetch(`${API_URL}/api/v1/auth/refresh`, {
    method: "POST",
    credentials: "include",
  });
  if (!res.ok) return null;
  return (await res.json()) as SessionResponse;
}

/**
 * Perform an API request with bearer auth, tenant header, and one
 * refresh-and-retry on 401.
 */
export async function apiFetch<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { method = "GET", body, noRetry } = options;

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
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  };

  let res = await doFetch();

  if (res.status === 401 && !noRetry) {
    const session = await refreshSession();
    if (session) {
      setAccessToken(session.accessToken);
      res = await doFetch();
    }
  }

  if (res.status === 204) return undefined as T;

  const payload = await res.json().catch(() => null);

  if (!res.ok) {
    if (res.status === 401) setAccessToken(null);
    const envelope = payload as { error?: { code?: string; message?: string } } | null;
    throw new ApiError(
      res.status,
      envelope?.error?.code ?? "unknown",
      envelope?.error?.message ?? `request failed with status ${res.status}`,
    );
  }
  return payload as T;
}
