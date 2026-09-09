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
import { apiFetch, getAccessToken, getSchoolId, setAccessToken, setSchoolId } from "./api";
import type { Me, Membership } from "./permissions";

interface SessionState {
  /** null while loading; undefined when logged out. */
  me: Me | null | undefined;
  memberships: Membership[];
  activeSchoolId: string | null;
  loading: boolean;
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
  login: async () => {},
  logout: async () => {},
  switchSchool: () => {},
  reload: async () => {},
});

export function SessionProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const [memberships, setMemberships] = useState<Membership[]>([]);
  const [activeSchoolId, setActiveSchoolId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    if (!getAccessToken()) {
      setMe(undefined);
      setLoading(false);
      return;
    }
    try {
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
    } catch {
      setMe(undefined);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const login = useCallback(
    async (email: string, password: string) => {
      const session = await apiFetch<{ accessToken: string }>("/api/v1/auth/login", {
        method: "POST",
        body: { email, password },
        noRetry: true,
      });
      setAccessToken(session.accessToken);
      await reload();
    },
    [reload],
  );

  const logout = useCallback(async () => {
    try {
      await apiFetch("/api/v1/auth/logout", { method: "POST", noRetry: true });
    } finally {
      setAccessToken(null);
      setMe(undefined);
      setMemberships([]);
    }
  }, []);

  const switchSchool = useCallback((schoolId: string) => {
    setSchoolId(schoolId);
    setActiveSchoolId(schoolId);
    window.location.reload();
  }, []);

  const value = useMemo(
    () => ({ me, memberships, activeSchoolId, loading, login, logout, switchSchool, reload }),
    [me, memberships, activeSchoolId, loading, login, logout, switchSchool, reload],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionState {
  return useContext(SessionContext);
}
