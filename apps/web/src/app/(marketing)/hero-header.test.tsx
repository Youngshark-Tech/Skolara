import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { HeroAuthActions } from "./hero-header";

const sessionMock = vi.hoisted(() => ({
  me: undefined as
    | { id: string; email: string; name: string }
    | null
    | undefined,
  loading: false,
}));

vi.mock("@/lib/session", () => ({
  // The hero consumes the same provider lifecycle as the app shell; the test
  // pins the three render states (loading / guest / authenticated).
  useSession: () => sessionMock,
}));

describe("HeroAuthActions (#129)", () => {
  beforeEach(() => {
    cleanup();
    sessionMock.me = undefined;
    sessionMock.loading = false;
  });

  it("renders a width-stable placeholder while the session boots", () => {
    sessionMock.loading = true;
    render(<HeroAuthActions />);
    const placeholder = document.querySelector("div[aria-hidden='true']");
    expect(placeholder).not.toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("shows Log in / Sign up for guests (me === null)", () => {
    sessionMock.me = null;
    render(<HeroAuthActions />);
    const links = screen.getAllByRole("link");
    const hrefs = links.map((l) => l.getAttribute("href"));
    expect(hrefs).toContain("/login");
    expect(hrefs).toContain("/signup");
    expect(screen.queryByText("Open workspace")).toBeNull();
  });

  it("shows Open workspace for an authenticated session (me set)", () => {
    sessionMock.me = { id: "u1", email: "admin@skolara.dev", name: "Demo" };
    render(<HeroAuthActions />);
    const link = screen.getByRole("link", { name: "Open workspace" });
    expect(link.getAttribute("href")).toBe("/dashboard");
    expect(screen.queryByText("Sign up")).toBeNull();
  });
});
