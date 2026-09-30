package review

import (
	"context"

	"unreal-review/internal/findings"
)

type Agent interface {
	Run(ctx context.Context, req AgentRequest) (AgentResult, error)
}

type AgentRequest struct {
	Workspace    string
	ReviewID     string
	FindingsPath string
	Prompt       string
	SystemPrompt string
	Model        string
	// PriorCost is already included in the root checkpoint. Stateful adapters
	// return only cost not yet included there, including durably staged work
	// whose caller was interrupted before publishing its checkpoint.
	PriorCost findings.Cost
	Resuming  bool
	Plan      *ReviewPlan
}

type AgentResult struct {
	Cost     findings.Cost
	Coverage *PlanCoverage
}
