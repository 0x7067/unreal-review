package review

// DiffSpan owns a half-open byte range in the full selected unified diff.
// Context may overlap, but local task ownership covers every byte exactly once.
type DiffSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// PlanTask is one bounded, source-bound review scope. Boundary tasks own no
// source bytes and explicitly inspect interactions across local scopes.
type PlanTask struct {
	ID     string     `json:"id"`
	Kind   string     `json:"kind"`
	Paths  []string   `json:"paths"`
	Spans  []DiffSpan `json:"spans,omitempty"`
	Prompt string     `json:"prompt"`
}

// ReviewPlan is a deterministic coverage contract, not a new findings schema.
// It is passed through the Agent seam and persisted by stateful adapters.
type ReviewPlan struct {
	Version   string     `json:"version"`
	Digest    string     `json:"digest"`
	DiffSHA   string     `json:"diff_sha"`
	DiffBytes int        `json:"diff_bytes"`
	Tasks     []PlanTask `json:"tasks"`
}

// PlanCoverage acknowledges successful task completion and verification-pipeline
// coverage for all required task output. Review never completes a planned run
// from summary/cost alone.
type PlanCoverage struct {
	Digest    string
	Completed []string
	Verified  []string
}

const MaxPlanPromptBytes = 120000
