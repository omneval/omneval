package judge

// Judge executes an LLM-as-a-Judge evaluation for a single EvalJob,
// renders the judge prompt template with span data, calls the judge model,
// and returns a structured Score.
type Judge struct{}
