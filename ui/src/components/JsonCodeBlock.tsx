import { colors } from "@/theme";

interface JsonCodeBlockProps {
  value: string;
  label?: string;
  maxHeight?: number;
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function highlightJson(json: string): string {
  let escaped = escapeHtml(json);

  // Highlight JSON keys (quoted strings followed by colon)
  escaped = escaped.replace(
    /(".*?"(?!\w):)/g,
    '<span style="color: var(--omneval-violet-light)">"$1</span>:'
  );

  // Highlight JSON string values
  escaped = escaped.replace(
    /:\s*("(?:[^"\\]|\\.)*")/g,
    ': <span style="color: var(--omneval-violet-pale)">$1</span>'
  );

  // Highlight JSON numbers
  escaped = escaped.replace(
    /:\s*(\d+\.?\d*)/g,
    ': <span style="color: #82AAFF">$1</span>'
  );

  // Highlight JSON booleans and null
  escaped = escaped.replace(
    /:\s*(true|false|null)/g,
    ': <span style="color: #C792EA">$1</span>'
  );

  return escaped;
}

export default function JsonCodeBlock({
  value,
  label,
  maxHeight = 280,
}: JsonCodeBlockProps) {
  if (!value) {
    return (
      <div className="text-xs text-omneval-text-muted opacity-60 px-3 py-2">
        — empty —
      </div>
    );
  }

  let rawOutput: string;

  try {
    const obj = JSON.parse(value);
    const pretty = JSON.stringify(obj, null, 2);
    rawOutput = highlightJson(pretty);
  } catch {
    // Not valid JSON — show as escaped plain text
    rawOutput = escapeHtml(value);
  }

  return (
    <div className="rounded-lg overflow-hidden border"
      style={{ borderColor: colors.backgrounds.border }}
    >
      {label && (
        <div
          className="px-3 py-1.5 text-xs font-medium border-b"
          style={{
            background: colors.backgrounds.surface,
            color: colors.typography.ashGrey,
            borderBottom: `1px solid ${colors.backgrounds.border}`,
          }}
        >
          {label}
        </div>
      )}
      <pre
        className="overflow-auto p-3 text-xs font-mono"
        style={{
          background: colors.backgrounds.depth,
          maxHeight: `${maxHeight}px`,
          color: colors.typography.ashGrey,
          lineHeight: 1.5,
        }}
        dangerouslySetInnerHTML={{ __html: rawOutput }}
      />
    </div>
  );
}
