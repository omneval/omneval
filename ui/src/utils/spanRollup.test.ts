import { describe, it, expect } from "vitest";
import { annotateSpanTree, type Span } from "./spanRollup";

// --- Test: flat single-leaf case ---
// A root span with a single child that has all the tokens.
// The root's cumulative should equal the child's own values.

describe("annotateSpanTree — flat single-leaf case", () => {
  it("propagates a leaf's tokens and cost up to its parent", () => {
    const child: Span = {
      span_id: "leaf",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "llm-call",
      kind: "llm",
      input_tokens: 100,
      output_tokens: 200,
      cost_usd: 0.05,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
    };

    const root: Span = {
      span_id: "root",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "chain",
      kind: "chain",
      input_tokens: 0,
      output_tokens: 0,
      cost_usd: 0,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
      children: [child],
    };

    const result = annotateSpanTree(root);

    // Root rollup = sum of all descendants
    expect((result as any).rollup_input_tokens).toBe(100);
    expect((result as any).rollup_output_tokens).toBe(200);
    expect((result as any).rollup_cost_usd).toBe(0.05);

    // Leaf (no children) rollup = its own values
    const leafResult = (result as any).children![0];
    expect(leafResult.rollup_input_tokens).toBe(100);
    expect(leafResult.rollup_output_tokens).toBe(200);
    expect(leafResult.rollup_cost_usd).toBe(0.05);
  });
});

// --- Test: multi-branch case ---
// A root span with two sibling children, each having distinct tokens and cost.

describe("annotateSpanTree — multi-branch case", () => {
  it("sums tokens and cost from multiple sibling branches", () => {
    const child1: Span = {
      span_id: "child-1",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "llm-call-1",
      kind: "llm",
      input_tokens: 500,
      output_tokens: 300,
      cost_usd: 0.10,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
    };

    const child2: Span = {
      span_id: "child-2",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "llm-call-2",
      kind: "llm",
      input_tokens: 200,
      output_tokens: 100,
      cost_usd: 0.04,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
    };

    const root: Span = {
      span_id: "root",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "chain",
      kind: "chain",
      input_tokens: 0,
      output_tokens: 0,
      cost_usd: 0,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
      children: [child1, child2],
    };

    const result = annotateSpanTree(root);

    // Root rollup = sum of both children's values
    const r = result as any;
    expect(r.rollup_input_tokens).toBe(700);
    expect(r.rollup_output_tokens).toBe(400);
    expect(r.rollup_cost_usd).toBe(0.14);

    // Each child is still just its own value
    expect(r.children[0].rollup_input_tokens).toBe(500);
    expect(r.children[0].rollup_cost_usd).toBe(0.10);
    expect(r.children[1].rollup_input_tokens).toBe(200);
    expect(r.children[1].rollup_cost_usd).toBe(0.04);
  });
});

// --- Test: non-leaf span with nonzero tokens/cost ---
// A chain span has its own tokens and cost, and also a child LLM span.
// The parent's cumulative must add both its own values and the child's rollup.

describe("annotateSpanTree — non-leaf span with nonzero values", () => {
  it("additively rolls up own tokens/cost plus all descendants", () => {
    const llm: Span = {
      span_id: "llm",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "llm-call",
      kind: "llm",
      input_tokens: 1000,
      output_tokens: 500,
      cost_usd: 0.25,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
    };

    // Parent is itself an LLM-like span (e.g. a proxy or aggregator) with its own usage.
    const parent: Span = {
      span_id: "parent",
      trace_id: "t",
      parent_id: "",
      project_id: "p",
      name: "agent.loop",
      kind: "chain",
      input_tokens: 200,
      output_tokens: 100,
      cost_usd: 0.05,
      start_time: "2024-01-01T00:00:00Z",
      end_time: "2024-01-01T00:00:01Z",
      duration_ms: 1000,
      model_unpriced: false,
      children: [llm],
    };

    const result = annotateSpanTree(parent);

    // Parent rollup = its own + child's own
    const p = result as any;
    expect(p.rollup_input_tokens).toBe(1200); // 200 + 1000
    expect(p.rollup_output_tokens).toBe(600); // 100 + 500
    expect(p.rollup_cost_usd).toBe(0.30); // 0.05 + 0.25

    // Child's rollup is just its own (no grandchildren)
    expect(p.children[0].rollup_input_tokens).toBe(1000);
    expect(p.children[0].rollup_cost_usd).toBe(0.25);
  });
});