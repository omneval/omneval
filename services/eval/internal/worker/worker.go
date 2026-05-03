package worker

// Worker drains the Redis eval queue and dispatches jobs to the judge pipeline.
type Worker struct{}
