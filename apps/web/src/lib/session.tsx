"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useRouter } from "next/navigation";
import {
  ApiError,
  apiFetch,
  getAccessToken,
  getSchoolId,
  refreshSession,
  setAccessToken,
  setOnSessionExpired,
  setSchoolId,
} from "./api";
import type { Me, Membership } from "./permissions";
import { AUTH_HINT_COOKIE_NAME } from "./auth-cookies";

interface SessionState {
  /** null while loading; undefined when logged out. */
  me: Me | null | undefined;
  memberships: Membership[];
  activeSchoolId: string | null;
  loading: boolean;
  /** Bootstrap failure (e.g. API unreachable) — distinct from being logged out. */
  error: string | null;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  switchSchool: (schoolId: string) => void;
  reload: () => Promise<void>;
}

const SessionContext = createContext<SessionState>({
  me: null,
  memberships: [],
  activeSchoolId: null,
  loading: true,
  error: null,
  login: async () => {},
  logout: async () => {},
  switchSchool: () => {},
  reload: async () => {},
});

/**
 * Web-origin session-hint cookie for the middleware guard: presence-only,
 * carries no secret (the real credentials stay in the HttpOnly refresh cookie
 * and the memory-only access token). Set on login, cleared on logout and
 * session expiry so shared machines don't render the shell for the next user.
 */
function setAuthHint(present: boolean): void {
  if (typeof document === "undefined") return;
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  if (present) {
    document.cookie = `${AUTH_HINT_COOKIE_NAME}=1; Path=/; SameSite=Lax${secure}`;
  } else {
    document.cookie = `${AUTH_HINT_COOKIE_NAME}=; Path=/; Max-Age=0; SameSite=Lax${secure}`;
  }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const [memberships, setMemberships] = useState<Membership[]>([]);
  const [activeSchoolId, setActiveSchoolId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const clearSessionState = useCallback(() => {
    setAccessToken(null);
    setSchoolId(null);
    setActiveSchoolId(null);
    setMemberships([]);
    setMe(undefined);
    setAuthHint(false);
  }, []);

  /**
   * ADR-011: the access token is memory-only, so after every full page load
   * the session is rebuilt with a silent /auth/refresh (the HttpOnly refresh
   * cookie rides along). A network blip is NOT a logout: one silent retry,
   * then an explicit error state the user can retry from.
   *
   * Returns whether the session is usable afterwards so switchSchool can pick
   * between the context refetch and its hard-reload fallback (#57).
   */
  const boot = useCallback(async (): Promise<boolean> => {
    setLoading(true);
    setError(null);
    try {
      if (!getAccessToken()) {
        let outcome = await refreshSession();
        if (!outcome.ok && outcome.reason === "network") {
          outcome = await refreshSession(); // one silent retry
        }
        if (!outcome.ok) {
          if (outcome.reason === "network") {
            setError("Cannot reach the server. Check your connection and try again.");
          } else {
            clearSessionState();
          }
          return false;
        }
        setAccessToken(outcome.session.accessToken);
        setAuthHint(true);
      }
      const [meRes, membershipsRes] = await Promise.all([
        apiFetch<Me>("/api/v1/me"),
        apiFetch<Membership[]>("/api/v1/me/memberships"),
      ]);
      setMe(meRes);
      setMemberships(membershipsRes);
      const stored = getSchoolId();
      const active =
        stored && membershipsRes.some((m) => m.schoolId === stored)
          ? stored
          : membershipsRes[0]?.schoolId ?? null;
      setActiveSchoolId(active);
      setSchoolId(active);
      setAuthHint(true);
      return true;
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        // Definitive 401 already cleared credentials + notified expiry.
        clearSessionState();
      } else {
        setError(
          err instanceof Error && err.message
            ? `${err.message} — try again.`
            : "Something went wrong while loading your session — try again.",
        );
      }
      return false;
    } finally {
      setLoading(false);
    }
  }, [clearSessionState]);

  const reload = useCallback(async () => {
    await boot();
  }, [boot]);

  useEffect(() => {
    void reload();
  }, [reload]);

  /**
   * Invoked by the api client when the refresh cookie is definitively rejected
   * (family revocation / expiry): reset provider state and return to /login.
   */
  useEffect(() => {
    setOnSessionExpired(() => {
      clearSessionState();
      setLoading(false);
      router.replace("/login");
    });
    return () => setOnSessionExpired(null);
  }, [clearSessionState, router]);

  const login = useCallback(
    async (email: string, password: string) => {
      const session = await apiFetch<{ accessToken: string }>("/api/v1/auth/login", {
        method: "POST",
        body: { email, password },
        noRetry: true,
      });
      setAccessToken(session.accessToken);
      setAuthHint(true);
      setError(null);
      await reload();
    },
    [reload],
  );

  const logout = useCallback(async () => {
    try {
      await apiFetch("/api/v1/auth/logout", { method: "POST", noRetry: true });
    } finally {
      // Clear everything, including the persisted school preference — on a
      // shared machine the next visitor must not inherit tenant context.
      clearSessionState();
      setLoading(false);
    }
  }, [clearSessionState]);

  /**
   * Switch the active tenant WITHOUT a full page reload (#57): every page keys
   * its queries on activeSchoolId, so updating context state is the refetch —
   * school-scoped data reloads through React, preserving the in-memory token
   * and scroll position. If this tab has no in-memory session we boot one
   * silently. When that boot fails, NO reload fallback is needed: a network
   * failure already surfaces the provider's explicit retry panel, and a
   * definitive expiry clears state and routes to /login on its own.
   */
  const switchSchool = useCallback(
    (schoolId: string) => {
      setSchoolId(schoolId);
      setActiveSchoolId(schoolId);
      if (getAccessToken()) return;
      void boot();
    },
    [boot],
  );

  const value = useMemo(
    () => ({
      me,
      memberships,
      activeSchoolId,
      loading,
      error,
      login,
      logout,
      switchSchool,
      reload,
    }),
    [me, memberships, activeSchoolId, loading, error, login, logout, switchSchool, reload],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionState {
  return useContext(SessionContext);
}
