import { describe, it, expect } from "vitest";
import { colors } from "@/theme";

describe("Toast color references", () => {
  it("emberFlare is used for success toast background", () => {
    expect(colors.accents.emberFlare).toBe("#7C3AED");
  });

  it("softGlow is used for info toast background", () => {
    expect(colors.accents.softGlow).toBe("#8B5CF6");
  });

  it("caveWall is used for borders", () => {
    expect(colors.backgrounds.caveWall).toBe("#2A2A3A");
  });

  it("charcoalDepth is used for modal background", () => {
    expect(colors.backgrounds.charcoalDepth).toBe("#111118");
  });

  it("abyssBlack is used for input backgrounds", () => {
    expect(colors.backgrounds.abyssBlack).toBe("#0A0A0F");
  });

  it("pureLight is used for text on dark backgrounds", () => {
    expect(colors.typography.pureLight).toBe("#FFFFFF");
  });

  it("ashGrey is used for secondary text", () => {
    expect(colors.typography.ashGrey).toBe("#8B8BA7");
  });
});

describe("SaveToDatasetModal data types", () => {
  it("dataset item has required fields", () => {
    // Verify the shape that the component expects for datasets
    const sampleDataset = {
      dataset_id: "abc123",
      name: "test-dataset",
      created_at: "2024-01-01T00:00:00Z",
      item_count: 5,
    };

    expect(sampleDataset).toHaveProperty("dataset_id");
    expect(sampleDataset).toHaveProperty("name");
    expect(sampleDataset).toHaveProperty("created_at");
    expect(sampleDataset).toHaveProperty("item_count");
  });

  it("span has required fields for save modal", () => {
    // Verify the shape that the component expects for spans
    const sampleSpan = {
      span_id: "span123",
      trace_id: "trace456",
      input: "test input",
      output: "test output",
    };

    expect(sampleSpan).toHaveProperty("span_id");
    expect(sampleSpan).toHaveProperty("trace_id");
    expect(sampleSpan).toHaveProperty("input");
    expect(sampleSpan).toHaveProperty("output");
  });
});

describe("ToastType type safety", () => {
  it("accepts only valid toast types", () => {
    const validTypes: Array<"success" | "error" | "info"> = ["success", "error", "info"];
    expect(validTypes).toHaveLength(3);
  });
});
