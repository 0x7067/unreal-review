package review

import (
	"context"

	"github.com/0x7067/unreal-review/findings"
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
	// HeadSHA is the reviewed commit. It is empty for a working-tree review,
	// which has no single committed tree to copy into a verifier scratch.
	HeadSHA string
	// Shell is the Bash interpreter for this session. Empty means /bin/sh.
	// Verifier tasks set a sandbox or environment-scrubbing wrapper.
	Shell string
	// ReadOnlyBash selects the zero-budget Bash tool description. The verifier
	// sets it when a sandboxed batch has no code-execution budget.
	ReadOnlyBash bool
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
