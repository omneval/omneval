import { describe, it, expect } from "vitest";
import { diffText, diffModelConfig } from "./diff";

describe("diffText", () => {
  it("returns unchanged lines for identical text", () => {
    const text = "Hello world\nThis is a test";
    const result = diffText(text, text);
    const diffLines = result.filter((l) => l.type !== "header");
    expect(diffLines.every((l) => l.type === "unchanged")).toBe(true);
  });

  it("marks added lines as green", () => {
    const result = diffText("old", "new");
    const added = result.filter((l) => l.type === "added");
    expect(added.length).toBeGreaterThan(0);
  });

  it("marks removed lines as red", () => {
    const result = diffText("old", "new");
    const removed = result.filter((l) => l.type === "removed");
    expect(removed.length).toBeGreaterThan(0);
  });

  it("handles completely different text", () => {
    const result = diffText("line1\nline2\nline3", "a\nb\nc\nd");
    const types = result.map((l) => l.type);
    expect(types).toContain("removed");
    expect(types).toContain("added");
  });

  it("handles empty old text (empty line removed, new content added)", () => {
    const result = diffText("", "new content");
    const diffLines = result.filter((l) => l.type !== "header");
    expect(diffLines.length).toBe(2);
    expect(diffLines.find((l) => l.type === "removed")?.text).toBe("");
    expect(diffLines.find((l) => l.type === "added")?.text).toBe("new content");
  });

  it("handles empty new text (old content removed, empty line added)", () => {
    const result = diffText("old content", "");
    const diffLines = result.filter((l) => l.type !== "header");
    expect(diffLines.length).toBe(2);
    expect(diffLines.find((l) => l.type === "removed")?.text).toBe("old content");
    expect(diffLines.find((l) => l.type === "added")?.text).toBe("");
  });

  it("preserves line content in diff lines", () => {
    const result = diffText("hello\nworld", "hello\nuniverse");
    const added = result.find((l) => l.type === "added");
    expect(added?.text).toBe("universe");
    const removed = result.find((l) => l.type === "removed");
    expect(removed?.text).toBe("world");
  });

  it("returns only header for both empty strings", () => {
    const result = diffText("", "");
    const diffLines = result.filter((l) => l.type !== "header");
    // split("") gives [""] for each, which matches as unchanged
    expect(diffLines).toEqual([{ type: "unchanged", text: "" }]);
  });

  it("produces unified diff output with === header", () => {
    const result = diffText("aaa\nbbb\nccc", "aaa\nbbc\nccc");
    const header = result.find((l) => l.type === "header");
    expect(header).toBeDefined();
    expect(header?.type).toBe("header");
  });

  it("handles single line text", () => {
    const result = diffText("hello", "world");
    const diffLines = result.filter((l) => l.type !== "header");
    const removed = diffLines.filter((l) => l.type === "removed");
    const added = diffLines.filter((l) => l.type === "added");
    expect(removed.length).toBe(1);
    expect(added.length).toBe(1);
  });
});

describe("diffModelConfig", () => {
  it("returns empty diff for identical configs", () => {
    const a = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const b = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const result = diffModelConfig(a, b);
    expect(result).toEqual([]);
  });

  it("detects model name change", () => {
    const a = { model: "gpt-3.5", temperature: 0.7, max_tokens: 256 };
    const b = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const result = diffModelConfig(a, b);
    expect(result).toContainEqual({ field: "model", oldValue: "gpt-3.5", newValue: "gpt-4" });
  });

  it("detects temperature change", () => {
    const a = { model: "gpt-4", temperature: 0.5, max_tokens: 256 };
    const b = { model: "gpt-4", temperature: 0.9, max_tokens: 256 };
    const result = diffModelConfig(a, b);
    expect(result).toContainEqual({ field: "temperature", oldValue: 0.5, newValue: 0.9 });
  });

  it("detects max_tokens change", () => {
    const a = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const b = { model: "gpt-4", temperature: 0.7, max_tokens: 512 };
    const result = diffModelConfig(a, b);
    expect(result).toContainEqual({ field: "max_tokens", oldValue: 256, newValue: 512 });
  });

  it("detects multiple changes", () => {
    const a = { model: "gpt-3.5", temperature: 0.5, max_tokens: 256 };
    const b = { model: "gpt-4", temperature: 0.9, max_tokens: 1024 };
    const result = diffModelConfig(a, b);
    expect(result.length).toBe(3);
    expect(result.map((d) => d.field)).toContain("model");
    expect(result.map((d) => d.field)).toContain("temperature");
    expect(result.map((d) => d.field)).toContain("max_tokens");
  });

  it("handles null/undefined baseline (compare against empty)", () => {
    const b = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const result = diffModelConfig(undefined, b);
    expect(result).toContainEqual({ field: "model", oldValue: "—", newValue: "gpt-4" });
    expect(result).toContainEqual({ field: "temperature", oldValue: "—", newValue: 0.7 });
    expect(result).toContainEqual({ field: "max_tokens", oldValue: "—", newValue: 256 });
  });

  it("handles null/undefined current", () => {
    const a = { model: "gpt-4", temperature: 0.7, max_tokens: 256 };
    const result = diffModelConfig(a, undefined);
    expect(result).toContainEqual({ field: "model", oldValue: "gpt-4", newValue: "—" });
  });
});
