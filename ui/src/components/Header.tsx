import { useState } from "react";

// ── Types ──────────────────────────────────────────────────────────

interface Project {
  project_id: string;
  name: string;
  org_id: string;
}

interface HeaderProps {
  activeProject: string;
  projects: Project[];
  onProjectChange: (id: string) => void;
  onNewProject: () => void;
  timeRange: string;
  onTimeRangeChange: (range: string) => void;
}

interface TimeRangeOption {
  label: string;
  value: string;
}

// ── Data ───────────────────────────────────────────────────────────

const TIME_RANGES: TimeRangeOption[] = [
  { label: "Past 1 hour", value: "1h" },
  { label: "Past 6 hours", value: "6h" },
  { label: "Past 24 hours", value: "1d" },
  { label: "Past 7 days", value: "7d" },
  { label: "Past 30 days", value: "30d" },
  { label: "Custom...", value: "custom" },
];

// ── Components ─────────────────────────────────────────────────────

function Dropdown({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { label: string; value: string }[];
  onChange: (v: string) => void;
}) {
  const [open, setOpen] = useState(false);

  const selected = options.find((o) => o.value === value);

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 px-2.5 py-1.5 text-sm rounded-md bg-omneval-surface border border-omneval-border text-omneval-text-muted hover:border-omneval-violet hover:text-omneval-text-pure transition-all duration-150"
      >
        <span className="text-omneval-text-muted font-mono text-[10px] uppercase tracking-wider">
          {label}
        </span>
        <span className="text-omneval-text-pure">{selected?.label ?? value}</span>
        <svg
          width="12"
          height="12"
          viewBox="0 0 12 12"
          fill="none"
          className={`transition-transform duration-150 ${open ? "rotate-180" : ""}`}
        >
          <path d="M3 4.5l3 3 3-3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 z-40"
            onClick={() => setOpen(false)}
          />

          {/* Dropdown menu */}
          <div
            className="absolute z-50 mt-1 w-48 bg-omneval-depth border border-omneval-border rounded-md shadow-lg overflow-hidden"
            style={{ boxShadow: "0 8px 24px rgba(0,0,0,0.7)" }}
          >
            {options.map((option) => (
              <button
                key={option.value}
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
                className={`w-full px-3 py-2 text-sm text-left transition-colors ${
                  option.value === value
                    ? "text-omneval-violet-pale bg-omneval-violet-active"
                    : "text-omneval-text-muted hover:text-omneval-text-pure hover:bg-omneval-violet-hover"
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// ── Header Component ───────────────────────────────────────────────

export default function Header({
  activeProject,
  projects,
  onProjectChange,
  onNewProject,
  timeRange,
  onTimeRangeChange,
}: HeaderProps) {
  return (
    <header
      className="flex items-center justify-between px-4 py-2 bg-omneval-depth border-b border-omneval-border"
      style={{ height: "3rem" }}
    >
      {/* Left: project selector */}
      <div className="flex items-center gap-2">
        <button
          onClick={onNewProject}
          className="p-1.5 rounded-md text-omneval-text-muted hover:text-omneval-text-pure hover:bg-omneval-surface transition-colors"
          title="New project"
        >
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
            <path d="M7 1v12M1 7h12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
        <Dropdown
          label="Project"
          value={activeProject}
          options={
            projects.length > 0
              ? projects.map((p) => ({
                  label: p.name,
                  value: p.project_id,
                }))
              : [{ label: "No projects yet", value: "" }]
          }
          onChange={onProjectChange}
        />

        <Dropdown
          label="Time Range"
          value={timeRange}
          options={TIME_RANGES}
          onChange={onTimeRangeChange}
        />
      </div>
    </header>
  );
}
