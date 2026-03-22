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
			"context_fetch":   120 * time.Millisecond,
			"partner_match":   30 * time.Millisecond,
			"llm_call":        120 * time.Millisecond,
			"decision_engine": 30 * time.Millisecond,
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
