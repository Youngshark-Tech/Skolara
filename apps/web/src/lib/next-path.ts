/**
 * Sanitization for post-login redirect targets (#57).
 *
 * `?next=` lets a deep link return the user to where they were heading after
 * signing in — but an unsanitized redirect is an open-redirect vulnerability.
 * Only app-relative paths are honored:
 * - must start with a single "/" (no scheme, no domain),
 * - must NOT start with "//" or "/\" (protocol-relative / browser-parsed
 *   cross-origin forms),
 * - must contain no control characters or an embedded "://",
 * - bounded length.
 * Anything else (and an absent value) yields null → callers fall back to "/".
 */
export function safeNextPath(raw: string | null | undefined): string | null {
  if (!raw) return null;
  if (raw.length > 512) return null;
  if (!raw.startsWith("/")) return null;
  if (raw.startsWith("//") || raw.startsWith("/\\")) return null;
  if (/[\u0000-\u001f\u007f]/.test(raw)) return null;
  if (raw.includes("://")) return null;
  return raw;
}
