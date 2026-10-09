/**
 * The mock transport router (issue #142): maps (method, path, body) to
 * API-shaped responses entirely in memory.
 *
 * CONTRACT PARITY is the design goal — when the database is connected in
 * production and the flag flips off, callers must not observe a behavioral
 * change. That means:
 * - the same JSON envelopes as the Go API (httpx envelope for errors),
 * - the same status codes (400/404/409 mapping mirrors students/http.go),
 * - the same headers where the UI reads them (X-Total-Count on enrollments),
 * - the same abort semantics (superseded requests reject with AbortError),
 * - one deliberate, documented difference: sessions never expire and any
 *   credentials log in — this is a demo, not a security boundary.
 */
import { ApiError } from "@/lib/api-error";
import {
  ADMIN_PERMISSIONS,
  DEMO_SCHOOL_ID,
} from "./data";
import {
  MockApiError,
  academicYears,
  classGroups,
  createEnrollment,
  createLearner,
  currentDemoUser,
  getVisibleLearner,
  listEnrollments,
  listInvoices,
  listVisibleLearners,
  mockLogin,
  mockSignup,
  nextMockToken,
  transitionEnrollment,
  wallet,
} from "./store";

/** The subset of `Response` the client actually consumes. */
export interface MockResponseLike {
  ok: boolean;
  status: number;
  headers: { get(name: string): string | null };
  json: () => Promise<unknown>;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

function abortError(): Error {
  if (typeof DOMException !== "undefined") {
    return new DOMException("The operation was aborted.", "AbortError");
  }
  const e = new Error("The operation was aborted.");
  e.name = "AbortError";
  return e;
}

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): MockResponseLike {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) => {
        const key = Object.keys(headers).find((k) => k.toLowerCase() === name.toLowerCase());
        return key ? headers[key] : null;
      },
    },
    json: async () => body,
  };
}

function errorEnvelope(status: number, code: string, message: string): MockResponseLike {
  return jsonResponse(status, { error: { code, message } });
}

function intParam(value: string | null, fallback: number): number {
  const n = Number(value);
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : fallback;
}

interface MockRequest {
  method: string;
  path: string;
  body?: unknown;
  onResponse?: (res: MockResponseLike) => void;
}

/** Synchronous routing table: path -> response. */
function route(req: MockRequest): MockResponseLike {
  const method = req.method.toUpperCase();
  const [path, queryString] = req.path.split("?");
  const query = new URLSearchParams(queryString ?? "");
  const segments = (path ?? "").replace(/\/+$/, "").split("/").filter(Boolean);
  // Expected prefix: ["api","v1", ...rest]
  const rest = segments.slice(2);

  if (rest[0] === "auth") {
    if (method === "POST" && rest[1] === "signup" && rest.length === 2) {
      try {
        mockSignup(req.body);
        return jsonResponse(201, { status: "created" });
      } catch (err) {
        return mockError(err);
      }
    }
    if (method === "POST" && rest[1] === "login" && rest.length === 2) {
      try {
        const b = (req.body ?? {}) as Record<string, unknown>;
        mockLogin(b.email, b.password);
        return jsonResponse(200, {
          accessToken: nextMockToken(),
          tokenType: "Bearer",
          expiresIn: 900,
        });
      } catch (err) {
        return mockError(err);
      }
    }
    if (method === "POST" && rest[1] === "refresh" && rest.length === 2) {
      // The demo session never expires.
      return jsonResponse(200, {
        accessToken: nextMockToken(),
        tokenType: "Bearer",
        expiresIn: 900,
      });
    }
    if (method === "POST" && rest[1] === "logout" && rest.length === 2) {
      return jsonResponse(204, null);
    }
    return errorEnvelope(404, "not_found", "mock: unknown auth route");
  }

  if (rest[0] === "me" && method === "GET" && rest.length === 1) {
    const user = currentDemoUser();
    return jsonResponse(200, {
      id: user.id,
      email: user.email,
      name: user.name,
      status: user.status,
      roles: [user.role],
      permissions: ADMIN_PERMISSIONS,
    });
  }

  if (rest[0] === "me" && rest[1] === "memberships" && method === "GET" && rest.length === 2) {
    const user = currentDemoUser();
    return jsonResponse(200, [
      {
        userId: user.id,
        schoolId: DEMO_SCHOOL_ID,
        role: user.role,
        status: "active",
      },
    ]);
  }

  if (rest[0] === "learners") {
    if (method === "GET" && rest.length === 1) {
      const page = listVisibleLearners({
        q: query.get("q") ?? undefined,
        limit: intParam(query.get("limit"), 20),
        offset: intParam(query.get("offset"), 0),
      });
      return jsonResponse(200, { ...page, limit: page.learners.length, offset: intParam(query.get("offset"), 0) });
    }
    if (method === "POST" && rest.length === 1) {
      try {
        return jsonResponse(201, createLearner(req.body));
      } catch (err) {
        return mockError(err);
      }
    }
    if (method === "GET" && rest.length === 2) {
      try {
        return jsonResponse(200, getVisibleLearner(rest[1]!));
      } catch (err) {
        return mockError(err);
      }
    }
  }

  if (rest[0] === "enrollments") {
    if (method === "GET" && rest.length === 1) {
      const offset = intParam(query.get("offset"), 0);
      const limit = intParam(query.get("limit"), 20);
      const page = listEnrollments({
        status: query.get("status") ?? undefined,
        limit,
        offset,
      });
      return jsonResponse(
        200,
        { ...page, limit, offset },
        { "X-Total-Count": String(page.total) },
      );
    }
    if (method === "POST" && rest.length === 1) {
      try {
        return jsonResponse(201, createEnrollment(req.body));
      } catch (err) {
        return mockError(err);
      }
    }
    if (method === "POST" && rest.length === 3 && rest[2] === "transition") {
      try {
        const b = (req.body ?? {}) as Record<string, unknown>;
        return jsonResponse(200, transitionEnrollment(rest[1]!, b.to));
      } catch (err) {
        return mockError(err);
      }
    }
  }

  if (rest[0] === "academic-years" && method === "GET" && rest.length === 1) {
    return jsonResponse(200, academicYears());
  }

  if (rest[0] === "classes" && method === "GET" && rest.length === 1) {
    return jsonResponse(200, classGroups());
  }

  if (rest[0] === "wallet" && method === "GET" && rest.length === 1) {
    return jsonResponse(200, { wallet: wallet() });
  }

  if (rest[0] === "invoices" && method === "GET" && rest.length === 1) {
    const limit = intParam(query.get("limit"), 20);
    const page = listInvoices({
      status: query.get("status") ?? undefined,
      limit,
      offset: intParam(query.get("offset"), 0),
    });
    return jsonResponse(200, page);
  }

  return errorEnvelope(404, "not_found", `mock: no route for ${method} ${path}`);
}

function mockError(err: unknown): MockResponseLike {
  if (err instanceof MockApiError) {
    return errorEnvelope(err.status, err.code, err.message);
  }
  return errorEnvelope(500, "internal_error", "mock: unexpected store failure");
}

/**
 * Entry point used by lib/api.ts when demo data mode is on. Mirrors the
 * client's observable behavior: artificial latency, abort semantics, header
 * observation, 204 handling, and ApiError throws for non-2xx.
 */
export async function mockApiRequest<T>(options: {
  method?: string;
  path: string;
  body?: unknown;
  signal?: AbortSignal;
  onResponse?: (res: MockResponseLike) => void;
}): Promise<T> {
  const { method = "GET", path, body, signal, onResponse } = options;

  if (signal?.aborted) throw abortError();

  // Small, slightly varied latency so loading states are exercised honestly.
  await sleep(120 + Math.floor(Math.random() * 160));

  if (signal?.aborted) throw abortError();

  const res = route({ method, path, body });
  onResponse?.(res);

  if (res.status === 204) return undefined as T;

  const payload = await res.json().catch(() => null);

  if (res.ok && payload === null) {
    throw new ApiError(502, "bad_response", "The server returned an unexpected response.");
  }
  if (!res.ok) {
    const envelope = payload as { error?: { code?: string; message?: string } } | null;
    throw new ApiError(
      res.status,
      envelope?.error?.code ?? "unknown",
      envelope?.error?.message ?? `request failed with status ${res.status}`,
    );
  }
  return payload as T;
}
