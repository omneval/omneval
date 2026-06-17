package metadata

import (
	"context"

	"github.com/omneval/omneval/internal/domain"
)

// EvalRuleStore is the domain interface for evaluation rule CRUD and filter operations.
type EvalRuleStore interface {
	CreateEvalRule(ctx context.Context, rule *domain.EvalRule) error
	GetEvalRule(ctx context.Context, ruleID string) (*domain.EvalRule, error)
	ListEvalRules(ctx context.Context, projectID string) ([]*domain.EvalRule, error)
	UpdateEvalRule(ctx context.Context, rule *domain.EvalRule) error
	DeleteEvalRule(ctx context.Context, ruleID string) error
}