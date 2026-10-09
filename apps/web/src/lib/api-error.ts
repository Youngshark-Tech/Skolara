/**
 * The typed API error shared by the network client (lib/api.ts) and the
 * in-memory mock transport (lib/mock/*, issue #142).
 *
 * It lives in its own module so the mock can throw the SAME class the UI
 * already catches (`err instanceof ApiError`) without a circular import —
 * api.ts re-exports it, so existing `import { ApiError } from "@/lib/api"`
 * keeps working unchanged.
 */
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
