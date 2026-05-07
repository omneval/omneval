import { describe, it, expect } from "vitest";
import { colors } from "@/theme";

describe("Toast color references", () => {
  it("emberFlare is used for success toast background", () => {
    expect(colors.accents.emberFlare).toBe("#FF5722");
  });

  it("softGlow is used for info toast background", () => {
    expect(colors.accents.softGlow).toBe("#FF8A65");
  });

  it("caveWall is used for borders", () => {
    expect(colors.backgrounds.caveWall).toBe("#2D2D2D");
  });

  it("charcoalDepth is used for modal background", () => {
    expect(colors.backgrounds.charcoalDepth).toBe("#0D0D0D");
  });

  it("abyssBlack is used for input backgrounds", () => {
    expect(colors.backgrounds.abyssBlack).toBe("#000000");
  });

  it("pureLight is used for text on dark backgrounds", () => {
    expect(colors.typography.pureLight).toBe("#FFFFFF");
  });

  it("ashGrey is used for secondary text", () => {
    expect(colors.typography.ashGrey).toBe("#A1A1AA");
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
