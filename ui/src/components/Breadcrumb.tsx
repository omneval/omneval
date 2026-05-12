import { colors } from "@/theme";

interface BreadcrumbProps {
  items: { label: string; onClick?: () => void }[];
}

export default function Breadcrumb({ items }: BreadcrumbProps) {
  if (items.length === 0) return null;

  return (
    <nav className="flex items-center gap-1 text-xs" aria-label="Breadcrumb">
      {items.map((item, index) => {
        const isLast = index === items.length - 1;
        const handleClick = isLast ? undefined : item.onClick;

        if (index > 0) {
          return (
            <div key={index} className="flex items-center gap-1">
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
              <button
                onClick={handleClick}
                className={`transition-colors ${
                  isLast
                    ? "text-lantern-pure font-medium"
                    : "text-lantern-ash hover:text-lantern-pure"
                }`}
                disabled={isLast}
                aria-current={isLast ? "page" : undefined}
              >
                {item.label}
              </button>
            </div>
          );
        }

        return (
          <button
            key={item.label}
            onClick={handleClick}
            className={`transition-colors ${
              isLast
                ? "text-lantern-pure font-medium"
                : "text-lantern-ash hover:text-lantern-pure"
            }`}
            disabled={isLast}
            aria-current={isLast ? "page" : undefined}
          >
            {item.label}
          </button>
        );
      })}
    </nav>
  );
}
