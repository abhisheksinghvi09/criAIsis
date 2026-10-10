"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Mark } from "./Mark";
import { useSession } from "@/lib/session";
import { useTheme } from "@/lib/theme";

interface Route {
  href: string;
  label: string;
  icon: React.ReactNode;
}

function Icon({ d }: { d: string }) {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  );
}

const ROUTES: Route[] = [
  { href: "/", label: "Overview", icon: <Icon d="M3 12h4l3 8 4-16 3 8h4" /> },
  { href: "/setup", label: "Setup", icon: <Icon d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" /> },
  { href: "/runbooks", label: "Runbooks", icon: <Icon d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" /> },
  { href: "/incidents", label: "Incidents", icon: <Icon d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0zM12 9v4M12 17h.01" /> },
  { href: "/specialists", label: "Specialists", icon: <Icon d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" /> },
  { href: "/integrations", label: "Integrations", icon: <Icon d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" /> },
];

export function Shell({ title, subtitle, actions, children }: {
  title: string;
  subtitle?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const { mode, toggle } = useTheme();
  const { creds, signOut } = useSession();

  return (
    <div className="shell">
      <aside className="sidebar">
        {/* The brand returns to Overview; there is no dropdown. */}
        <Link href="/" className="brand">
          <Mark />
          <span>
            <span className="brand-name">criAIsis</span>
            <span className="brand-sub">War Room</span>
          </span>
        </Link>

        <nav className="nav" aria-label="Sections">
          {ROUTES.map((route) => {
            const active = route.href === "/" ? pathname === "/" : pathname.startsWith(route.href);
            return (
              <Link
                key={route.href}
                href={route.href}
                className="nav-link"
                aria-current={active ? "page" : undefined}
              >
                {route.icon}
                {route.label}
              </Link>
            );
          })}
        </nav>

        <div className="sidebar-bottom">
          <p className="sidebar-note">
            Read-only posture.
            <br />
            criAIsis never mutates production.
          </p>
          {/* The mode switch lives here, per the design contract. There are no
              appearance tabs anywhere in the interface. */}
          <button className="mode-switch" onClick={toggle} type="button">
            <span>{mode === "light" ? "Light" : "Dark"} mode</span>
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              {mode === "light" ? (
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
              ) : (
                <>
                  <circle cx="12" cy="12" r="4" />
                  <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
                </>
              )}
            </svg>
          </button>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <div>
            <h1>{title}</h1>
            {subtitle ? <p className="topbar-sub">{subtitle}</p> : null}
          </div>
          <div className="topbar-right">
            {actions}
            {creds ? (
              <>
                <span className="badge muted mono">{creds.workspace}</span>
                <button className="btn subtle small" onClick={signOut} type="button">
                  Sign out
                </button>
              </>
            ) : null}
          </div>
        </header>
        <main className="content">{children}</main>
      </div>
    </div>
  );
}
