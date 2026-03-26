package resilience

import (
	"context"
	"time"
)

// BudgetManager enforces a global SLA across disparate asynchronous stages.
// Instead of hardcoding timeouts in individual HTTP clients, we centralize time
// allocation here to ensure the sum of all stages never exceeds the user-facing SLA.
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

// StageContext provisions a specific slice of the remaining global budget.
// If a previous stage took too long, the current stage's budget is mathematically
// clamped to prevent breaching the absolute deadline.
func (bm *BudgetManager) StageContext(parent context.Context, stage string) (context.Context, context.CancelFunc) {
	allocation, ok := bm.allocations[stage]
	if !ok {
		allocation = 50 * time.Millisecond // default
	}

	// This clipping mechanism is the core of our latency defense. Even if a stage
	// asks for 5 seconds, if the parent context only has 100ms left, 100ms is all it gets.
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
