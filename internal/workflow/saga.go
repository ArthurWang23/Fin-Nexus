package workflow

import (
	"go.temporal.io/sdk/workflow"
)

// Saga implements the Saga compensation pattern for multi-step workflows.
// Each step can register a compensation function that will be executed in
// reverse order if the workflow needs to roll back.
//
// Usage:
//
//	saga := NewSaga()
//	// step 1
//	err := workflow.ExecuteActivity(ctx, "PlaceOrder", ...).Get(ctx, &orderID)
//	saga.AddCompensation(func(ctx workflow.Context) error {
//	    return workflow.ExecuteActivity(ctx, "CancelOrder", orderID).Get(ctx, nil)
//	})
//	// step 2 fails
//	if err := workflow.ExecuteActivity(ctx, "ChargePayment", ...).Get(ctx, nil); err != nil {
//	    saga.Compensate(ctx)  // rolls back step 1 by calling CancelOrder
//	    return err
//	}
type Saga struct {
	compensations []func(ctx workflow.Context) error
}

func NewSaga() *Saga {
	return &Saga{}
}

// AddCompensation registers a rollback function for the most recent successful step.
func (s *Saga) AddCompensation(f func(ctx workflow.Context) error) {
	s.compensations = append(s.compensations, f)
}

// Compensate executes all registered compensations in reverse order (LIFO).
// Individual compensation failures are logged but do not prevent subsequent compensations.
func (s *Saga) Compensate(ctx workflow.Context) {
	logger := workflow.GetLogger(ctx)
	for i := len(s.compensations) - 1; i >= 0; i-- {
		if err := s.compensations[i](ctx); err != nil {
			logger.Error("Saga compensation failed", "step", i, "error", err)
		}
	}
}
