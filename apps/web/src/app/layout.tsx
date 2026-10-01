import type { Metadata } from "next";
import "./globals.css";
import { SessionProvider } from "@/lib/session";

export const metadata: Metadata = {
  title: {
    default: "Skolara",
    // Per-page metadata titles (#57): route layouts export `title: "Students"`
    // etc., rendered here as "Students · Skolara".
    template: "%s · Skolara",
  },
  description: "The Intelligent Operating System for Schools",
  icons: { icon: [{ url: "/favicon.svg", type: "image/svg+xml" }] },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-slate-50 text-ink antialiased">
        <SessionProvider>{children}</SessionProvider>
      </body>
    </html>
  );
}
