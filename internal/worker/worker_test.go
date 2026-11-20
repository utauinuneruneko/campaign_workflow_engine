package worker

import (
	"math"
	"testing"
	"time"
)

func TestCalculateNextWorkflowBudget(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		name        string
		total       time.Duration
		remaining   time.Duration
		budget      float64
		current     time.Duration
		want        float64
		wantErr     bool
	}{
		{
			name: "exact seven day campaign allocates full budget",
			total: 7 * day, remaining: 7 * day, budget: 700, current: 7 * day, want: 700,
		},
		{
			name: "first longer period for non-multiple campaign term",
			total: 15 * day, remaining: 15 * day, budget: 1500, current: 8 * day, want: 800,
		},
		{
			name: "remaining seven days receive remaining budget",
			total: 15 * day, remaining: 7 * day, budget: 700, current: 7 * day, want: 700,
		},
		{
			name: "proportional allocation after prior workflows",
			total: 21 * day, remaining: 14 * day, budget: 1000, current: 7 * day, want: 500,
		},
		{
			name: "reject zero campaign duration",
			total: 0, remaining: 7 * day, budget: 10, current: 7 * day, wantErr: true,
		},
		{
			name: "reject remaining duration greater than total",
			total: 7 * day, remaining: 8 * day, budget: 10, current: 7 * day, wantErr: true,
		},
		{
			name: "reject current period greater than remaining duration",
			total: 14 * day, remaining: 7 * day, budget: 10, current: 8 * day, wantErr: true,
		},
		{
			name: "reject negative remaining budget",
			total: 7 * day, remaining: 7 * day, budget: -1, current: 7 * day, wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateNextWorkflowBudget(tt.total, tt.remaining, tt.budget, tt.current)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("budget = %.12f, want %.12f", got, tt.want)
			}
		})
	}
}
