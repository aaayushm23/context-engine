package resilience

import (
	"context"
	"testing"
	"time"
)

func TestBudgetManager_StageContext(t *testing.T) {
	bm := NewBudgetManager(300 * time.Millisecond)

	tests := []struct {
		name           string
		stage          string
		parentTimeout  time.Duration
		wantMaxTimeout time.Duration
	}{
		{
			name:           "llm_call gets its allocated budget",
			stage:          "llm_call",
			parentTimeout:  60 * time.Second,
			wantMaxTimeout: 31 * time.Second,
		},
		{
			name:           "unknown stage gets default 50ms",
			stage:          "unknown_stage",
			parentTimeout:  15 * time.Second,
			wantMaxTimeout: 60 * time.Millisecond,
		},
		{
			name:           "stage budget capped by parent remaining time",
			stage:          "llm_call",
			parentTimeout:  50 * time.Millisecond, // parent has less than stage budget
			wantMaxTimeout: 55 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, parentCancel := context.WithTimeout(context.Background(), tt.parentTimeout)
			defer parentCancel()

			stageCtx, stageCancel := bm.StageContext(parent, tt.stage)
			defer stageCancel()

			deadline, ok := stageCtx.Deadline()
			if !ok {
				t.Fatal("stage context should have a deadline")
			}

			timeout := time.Until(deadline)
			if timeout > tt.wantMaxTimeout {
				t.Errorf("timeout %v exceeds max %v", timeout, tt.wantMaxTimeout)
			}
		})
	}
}

func TestBudgetManager_StageContextCancellation(t *testing.T) {
	bm := NewBudgetManager(300 * time.Millisecond)

	parent, parentCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer parentCancel()

	stageCtx, stageCancel := bm.StageContext(parent, "llm_call")
	defer stageCancel()

	// Wait for context to expire
	<-stageCtx.Done()

	if stageCtx.Err() != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", stageCtx.Err())
	}
}
