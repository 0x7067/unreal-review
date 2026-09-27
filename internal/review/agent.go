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
	Identify     func(findings.Finding) (findings.Finding, error)
	Resolve      func(findings.Resolution) (findings.Resolution, error)
}

type AgentResult struct {
	Cost findings.Cost
}
