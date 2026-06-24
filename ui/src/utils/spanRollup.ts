/**
 * A single span in a trace tree — matches the shape returned by the
 * Trace Detail API endpoint (which fetches all children in one call).
 */
export interface Span {
  span_id: string;
  trace_id: string;
  parent_id: string;
  project_id: string;
  name: string;
  kind: string;
  model?: string;
  start_time: string;
  end_time: string;
  cost_usd: number;
  input_tokens: number;
  output_tokens: number;
  children?: Span[];
  input?: string;
  output?: string;
  status_code?: string;
  scores?: { eval_name: string; value: number }[];
  attributes?: Record<string, unknown>;
}

/**
 * A Span with cumulative token/cost rollup fields added by `annotateSpanTree`.
 */
export interface AnnotatedSpan extends Span {
  rollup_input_tokens: number;
  rollup_output_tokens: number;
  rollup_cost_usd: number;
}

/**
 * Walk a span tree post-order and annotate every node with cumulative
 * (self + all descendants) token and cost totals.
 *
 * Returns the same tree (mutated in-place) so callers can continue using
 * the original `Span` references — the extra `rollup_*` fields are just
 * attached as markers.
 *
 * This is a pure function: it does not affect any shared state beyond the
 * returned tree.
 */
export function annotateSpanTree(span: Span): AnnotatedSpan {
  const acc: AnnotatedSpan = {
    ...span,
    rollup_input_tokens: 0,
    rollup_output_tokens: 0,
    rollup_cost_usd: 0,
  };

  const children: AnnotatedSpan[] = [];
  if (span.children) {
    for (const child of span.children) {
      const childAnnotated = annotateSpanTree(child) as AnnotatedSpan;
      children.push(childAnnotated);
      acc.rollup_input_tokens += childAnnotated.rollup_input_tokens;
      acc.rollup_output_tokens += childAnnotated.rollup_output_tokens;
      acc.rollup_cost_usd += childAnnotated.rollup_cost_usd;
    }
  }
  if (children.length > 0) {
    acc.children = children;
  }

  // Include the node's own values
  acc.rollup_input_tokens += span.input_tokens;
  acc.rollup_output_tokens += span.output_tokens;
  acc.rollup_cost_usd += span.cost_usd;

  return acc;
}