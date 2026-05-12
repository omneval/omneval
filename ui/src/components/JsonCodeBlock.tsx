import { colors } from "@/theme";

interface JsonCodeBlockProps {
  value: string;
  label?: string;
  maxHeight?: number;
}

function highlightJson(json: string): string {
  // Basic JSON syntax highlighting with regex
  return json
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(
      /(".*?"(?!\w):)/g,
      '<span style="color: var(--lantern-accent-glow)">"$1</span>:'
    )
    .replace(
      /:\s*("(?:[^"\\]|\\.)*")/g,
      ': <span style="color: var(--lantern-accent-flicker)">$1</span>'
    )
    .replace(
      /:\s*(\d+\.?\d*)/g,
      ': <span style="color: #82AAFF">$1</span>'
    )
    .replace(
      /:\s*(true|false)/g,
      ': <span style="color: #C792EA">$1</span>'
    )
    .replace(
      /:\s*(null)/g,
      ': <span style="color: #C792EA">$1</span>'
    );
}

export default function JsonCodeBlock({
  value,
  label,
  maxHeight = 280,
}: JsonCodeBlockProps) {
  if (!value) {
    return (
      <div className="text-xs text-lantern-ash opacity-60 px-3 py-2">
        — empty —
      </div>
    );
  }

  let parsedJson: string;
  let rawOutput: string;

  try {
    const obj = JSON.parse(value);
    parsedJson = JSON.stringify(obj, null, 2);
    rawOutput = highlightJson(parsedJson);
  } catch {
    // If it's not valid JSON, show as plain text
    parsedJson = value;
    rawOutput = value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  return (
    <div className="rounded-lg overflow-hidden border"
      style={{ borderColor: colors.backgrounds.caveWall }}
    >
      {label && (
        <div
          className="px-3 py-1.5 text-xs font-medium border-b"
          style={{
            background: colors.backgrounds.slightIllumination,
            color: colors.typography.ashGrey,
            borderBottom: `1px solid ${colors.backgrounds.caveWall}`,
          }}
        >
          {label}
        </div>
      )}
      <pre
        className="overflow-auto p-3 text-xs font-mono"
        style={{
          background: colors.backgrounds.charcoalDepth,
          maxHeight: `${maxHeight}px`,
          color: colors.typography.ashGrey,
          lineHeight: 1.5,
        }}
        dangerouslySetInnerHTML={{ __html: rawOutput }}
      />
    </div>
  );
}
