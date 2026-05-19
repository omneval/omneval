import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import JsonCodeBlock from "./JsonCodeBlock";

describe("JsonCodeBlock", () => {
  it("renders empty state when value is missing", () => {
    render(<JsonCodeBlock value="" />);
    expect(screen.getByText(/— empty —/i)).toBeInTheDocument();
  });

  it("renders label when provided", () => {
    render(<JsonCodeBlock value='{"a": 1}' label="Test Label" />);
    expect(screen.getByText("Test Label")).toBeInTheDocument();
  });

  it("renders raw text for non-JSON values", () => {
    render(<JsonCodeBlock value="plain text" />);
    expect(screen.getByText("plain text")).toBeInTheDocument();
  });

  it("renders valid JSON in a code block", () => {
    const jsonStr = '{"name": "test", "count": 42}';
    render(<JsonCodeBlock value={jsonStr} />);
    const pre = screen.getByText(/"name"/);
    expect(pre).toBeInTheDocument();
  });
});

describe("highlightJson", () => {
  it("escapes HTML entities", () => {
    render(<JsonCodeBlock value="<script>alert('xss')</script>" />);
    const container = screen.getByText(/alert/);
    expect(container.innerHTML).toContain("&lt;");
  });
});
