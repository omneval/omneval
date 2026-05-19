import { colors } from "@/theme";

interface BreadcrumbProps {
  items: { label: string; onClick?: () => void }[];
}

function SeparatorArrow() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 16 16"
      fill="none"
      className="flex-shrink-0 opacity-50"
      style={{ color: colors.typography.ashGrey }}
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

function BreadcrumbButton({
  label,
  onClick,
  isLast,
}: {
  label: string;
  onClick?: () => void;
  isLast: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={`transition-colors ${
        isLast
          ? "text-omneval-text-pure font-medium"
          : "text-omneval-text-muted hover:text-omneval-text-pure"
      }`}
      disabled={isLast}
      aria-current={isLast ? "page" : undefined}
    >
      {label}
    </button>
  );
}

export default function Breadcrumb({ items }: BreadcrumbProps) {
  if (items.length === 0) return null;

  return (
    <nav className="flex items-center gap-1 text-xs" aria-label="Breadcrumb">
      {items.map((item, index) => {
        const isLast = index === items.length - 1;
        const handleClick = isLast ? undefined : item.onClick;

        if (index === 0) {
          return (
            <BreadcrumbButton
              key={item.label}
              label={item.label}
              onClick={handleClick}
              isLast={isLast}
            />
          );
        }

        return (
          <div key={index} className="flex items-center gap-1">
            <SeparatorArrow />
            <BreadcrumbButton
              label={item.label}
              onClick={handleClick}
              isLast={isLast}
            />
          </div>
        );
      })}
    </nav>
  );
}
