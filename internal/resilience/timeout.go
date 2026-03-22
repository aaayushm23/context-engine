package resilience

import (
	"context"
	"time"
)

// BudgetManager distributes a total timeout across stages
type BudgetManager struct {
	totalBudget time.Duration
	allocations map[string]time.Duration
}

func NewBudgetManager(total time.Duration) *BudgetManager {
	return &BudgetManager{
		totalBudget: total,
		allocations: map[string]time.Duration{
			"context_fetch":   5 * time.Second,
			"partner_match":   2 * time.Second,
			"llm_call":        30 * time.Second,
			"decision_engine": 2 * time.Second,
		},
	}
}

// StageContext returns a context with the stage's allocated timeout
func (bm *BudgetManager) StageContext(parent context.Context, stage string) (context.Context, context.CancelFunc) {
	allocation, ok := bm.allocations[stage]
	if !ok {
		allocation = 50 * time.Millisecond // default
	}

	// Never exceed parent's remaining deadline
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if allocation > remaining {
			allocation = remaining
		}
	}

	return context.WithTimeout(parent, allocation)
}

func (bm *BudgetManager) TotalBudget() time.Duration {
	return bm.totalBudget
}
