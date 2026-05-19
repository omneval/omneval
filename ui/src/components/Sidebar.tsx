import { useState } from "react";
import { colors } from "@/theme";

// ── Types ──────────────────────────────────────────────────────────

export type NavSection = "home" | "traces" | "dashboards" | "prompts" | "eval" | "settings";

export interface NavItem {
  id: string;
  label: string;
  section: NavSection;
  icon: React.ReactNode;
}

interface SidebarProps {
  collapsed: boolean;
  onToggle: () => void;
  active: string;
  onNavigate: (id: string) => void;
  onLogout?: () => void;
}

// ── Icons (SVG) ────────────────────────────────────────────────────

function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      className={`transition-transform duration-200 ${open ? "rotate-90" : ""}`}
    >
      <path
        d="M6 4l4 4-4 4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function HomeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path
        d="M3 8.5L9 3l6 5.5V15a1 1 0 01-1 1H4a1 1 0 01-1-1V8.5z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function TracesIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path
        d="M4 4h10M4 9h10M4 14h10"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <circle cx="2" cy="4" r="1" fill="currentColor" />
      <circle cx="2" cy="9" r="1" fill="currentColor" />
      <circle cx="2" cy="14" r="1" fill="currentColor" />
    </svg>
  );
}

function PromptIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <rect x="3" y="3" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M7 7h4M7 10h2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function EvalIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path
        d="M9 2l6 4v6l-6 4-6-4V6l6-4z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M9 6v6M6 8l3 2 3-2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function SettingsIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <circle cx="9" cy="9" r="2.5" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="M9 1v2m0 12v2M1 9h2m12 0h2M3.3 3.3l1.4 1.4m8.6 8.6l1.4 1.4M3.3 14.7l1.4-1.4m8.6-8.6l1.4-1.4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

function AdminIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path
        d="M9 3L16 6.5v5L9 15l-7-3.5v-5L9 3z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M9 9v3M7 12l2 2 2-2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function LogoutIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
      <path d="M8 14H4a1 1 0 01-1-1V5a1 1 0 011-1h4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M12 9h3M14 6l3 3-3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** omneval brand logo — abstract data-node / orbital eye */
function OmnevalLogo() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
      {/* Outer dashed orbital ring */}
      <circle cx="12" cy="12" r="10" stroke={colors.accents.violetLight} strokeWidth="1" strokeDasharray="3 2" opacity="0.6" />
      {/* Inner ring */}
      <circle cx="12" cy="12" r="6" stroke={colors.accents.violet} strokeWidth="1.5" />
      {/* Core */}
      <circle cx="12" cy="12" r="3" fill={colors.accents.violet} />
      <circle cx="12" cy="12" r="1.5" fill={colors.accents.violetPale} />
      {/* Axis ticks */}
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2" stroke={colors.accents.violetLight} strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

// ── Navigation Data ────────────────────────────────────────────────

const NAV_SECTIONS: {
  label: string;
  items: NavItem[];
}[] = [
  {
    label: "",
    items: [
      { id: "dashboard", label: "Dashboard", section: "home", icon: <HomeIcon /> },
      { id: "traces", label: "Traces", section: "traces", icon: <TracesIcon /> },
    ],
  },

  {
    label: "Prompts",
    items: [
      { id: "prompts", label: "Prompts", section: "prompts", icon: <PromptIcon /> },
    ],
  },
  {
    label: "Evaluation",
    items: [
      { id: "datasets", label: "Datasets", section: "eval", icon: <EvalIcon /> },
      { id: "eval-rules", label: "Eval Rules", section: "eval", icon: <EvalIcon /> },
    ],
  },
];

const BOTTOM_ITEMS: NavItem[] = [
  { id: "settings", label: "Settings", section: "settings", icon: <SettingsIcon /> },
  { id: "admin", label: "Admin", section: "settings", icon: <AdminIcon /> },
];

// ── Section Accordion ──────────────────────────────────────────────

function SectionAccordion({
  label,
  items,
  active,
  onNavigate,
  collapsed,
  defaultOpen,
}: {
  label: string;
  items: NavItem[];
  active: string;
  onNavigate: (id: string) => void;
  collapsed: boolean;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen ?? label === "");

  if (collapsed) {
    return (
      <div className="flex flex-col items-center gap-1 px-1">
        {items.map((item) => (
          <button
            key={item.id}
            onClick={() => onNavigate(item.id)}
            className={`p-2 rounded-md transition-colors duration-150 ${
              active === item.id
                ? "text-omneval-violet-pale bg-omneval-violet-active"
                : "text-omneval-text-muted hover:text-omneval-text-pure"
            }`}
            title={item.label}
            aria-label={item.label}
          >
            {item.icon}
          </button>
        ))}
      </div>
    );
  }

  return (
    <div className="mb-1">
      {label && (
        <>
          <button
            onClick={() => setOpen(!open)}
            className="flex items-center gap-1.5 w-full px-2 py-1.5 text-xs font-semibold tracking-wide text-omneval-text-muted hover:text-omneval-text-pure transition-colors"
          >
            <ChevronIcon open={open} />
            {label.toUpperCase()}
          </button>
          <div
            className="h-px mx-2"
            style={{ background: "rgba(42, 42, 58, 0.7)" }}
          />
        </>
      )}
      {open && (
        <div className="space-y-0.5">
          {items.map((item) => (
            <NavItemButton
              key={item.id}
              item={item}
              active={active}
              onNavigate={onNavigate}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function NavItemButton({
  item,
  active,
  onNavigate,
}: {
  item: NavItem;
  active: string;
  onNavigate: (id: string) => void;
}) {
  const isActive = active === item.id;

  return (
    <button
      onClick={() => onNavigate(item.id)}
      className={`group flex items-center gap-3 w-full px-2 py-2 text-sm rounded-md transition-all duration-150 relative ${
        isActive
          ? "text-omneval-violet-pale bg-omneval-violet-active"
          : "text-omneval-text-muted hover:text-omneval-text-pure hover:bg-omneval-violet-hover"
      }`}
    >
      {/* Violet left border for active state */}
      {isActive && (
        <span
          className="absolute left-0 top-0 bottom-0 rounded-r"
          style={{
            width: "2px",
            background: `linear-gradient(180deg, ${colors.accents.violetLight}, ${colors.accents.violet})`,
          }}
        />
      )}
      <span className={isActive ? "text-omneval-violet-pale" : "text-current group-hover:text-omneval-text-pure"}>
        {item.icon}
      </span>
      <span className="truncate">{item.label}</span>
    </button>
  );
}

// ── Sidebar Component ──────────────────────────────────────────────

export default function Sidebar({ collapsed, onToggle, active, onNavigate, onLogout }: SidebarProps) {
  return (
    <aside
      className={`flex flex-col bg-omneval-depth border-r border-omneval-border transition-all duration-200 ${
        collapsed ? "w-14" : "w-56"
      }`}
      style={{ minWidth: collapsed ? "3.5rem" : "14rem" }}
    >
      {/* Top section: logo + toggle */}
      <div className="flex items-center justify-between px-2 py-3 border-b border-omneval-border">
        <div className={`flex items-center gap-2 ${collapsed ? "justify-center w-full" : ""}`}>
          <span className="text-omneval-violet">
            <OmnevalLogo />
          </span>
          {!collapsed && (
            <span
              className="text-sm font-bold tracking-wide"
              style={{ color: colors.accents.violetPale, letterSpacing: "0.04em" }}
            >
              omneval
            </span>
          )}
        </div>
        {!collapsed && (
          <button
            onClick={onToggle}
            className="p-1 rounded hover:bg-omneval-surface text-omneval-text-muted hover:text-omneval-text-pure transition-colors"
            title="Collapse sidebar"
            aria-label="Collapse sidebar"
          >
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M6 2L2 6l4 4M10 6H2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )}
        {collapsed && (
          <button
            onClick={onToggle}
            className="p-1 rounded hover:bg-omneval-surface text-omneval-text-muted hover:text-omneval-text-pure transition-colors"
            title="Expand sidebar"
            aria-label="Expand sidebar"
          >
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M10 2l4 4-4 4M6 6H14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )}
      </div>

      {/* Navigation sections */}
      <nav className="flex-1 overflow-y-auto py-2 px-1">
        {NAV_SECTIONS.map((section) => (
          <SectionAccordion
            key={section.label || "home"}
            label={section.label}
            items={section.items}
            active={active}
            onNavigate={onNavigate}
            collapsed={collapsed}
            defaultOpen={section.label === ""}
          />
        ))}
      </nav>

      {/* Bottom section */}
      {!collapsed && (
        <div className="border-t border-omneval-border py-2 px-1">
          {BOTTOM_ITEMS.map((item) => (
            <NavItemButton
              key={item.id}
              item={item}
              active={active}
              onNavigate={onNavigate}
            />
          ))}
        </div>
      )}

      {/* Bottom section for collapsed state - Settings + Logout */}
      {collapsed && (
        <>
          <div className="border-t border-omneval-border w-full" />
          <div className="flex flex-col items-center gap-1 px-1 pb-2">
            <button
              onClick={() => onNavigate("settings")}
              className={`p-2 rounded-md transition-colors duration-150 ${
                active === "settings"
                  ? "text-omneval-violet-pale bg-omneval-violet-active"
                  : "text-omneval-text-muted hover:text-omneval-text-pure"
              }`}
              title="Settings"
              aria-label="Settings"
            >
              <SettingsIcon />
            </button>
            <button
              onClick={() => onNavigate("admin")}
              className={`p-2 rounded-md transition-colors duration-150 ${
                active === "admin"
                  ? "text-omneval-violet-pale bg-omneval-violet-active"
                  : "text-omneval-text-muted hover:text-omneval-text-pure"
              }`}
              title="Admin"
              aria-label="Admin"
            >
              <AdminIcon />
            </button>
            <button
              onClick={onLogout}
              className={`p-2 rounded-md transition-colors duration-150 ${
                onLogout
                  ? "text-omneval-text-muted hover:text-omneval-text-pure"
                  : "text-omneval-text-muted/40 cursor-not-allowed"
              }`}
              title="Logout"
              aria-label="Logout"
              disabled={!onLogout}
            >
              <LogoutIcon />
            </button>
          </div>
        </>
      )}

      {/* User section with logout */}
      {!collapsed && (
        <div className="border-t border-omneval-border py-2 px-3">
          <button
            onClick={onLogout}
            className="flex items-center gap-2 w-full text-sm text-omneval-text-muted hover:text-omneval-text-pure transition-colors px-2 py-1.5 rounded-md hover:bg-omneval-surface"
            title="Logout"
          >
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M7 13H3a1 1 0 01-1-1V4a1 1 0 011-1h4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              <path d="M10 8h5M13 5l3 3-3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>Logout</span>
          </button>
        </div>
      )}
    </aside>
  );
}
