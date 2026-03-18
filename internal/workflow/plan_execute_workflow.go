package workflow

import (
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"
	"go-nexus/internal/usecase"
	"go-nexus/internal/workflow/activities"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PlanExecuteWorkflow implements the Plan-Execute pattern:
//
//	Phase 1: One LLM call generates a complete execution plan
//	Phase 2: Steps are executed in parallel where dependencies allow
//	         - Context Injection: dependent steps receive prior results
//	         - ApprovableAgent: two-phase (preview → approve → execute)
//	Phase 3: Artifacts are sent to frontend, results aggregated into final reply
func PlanExecuteWorkflow(ctx workflow.Context, userQuery string, streamID string, sessionID string, userID string) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// ── Load chat history ──
	var contextHistory []domain.Message
	if sessionID != "" {
		err := workflow.ExecuteActivity(ctx, "LoadChatHistory", sessionID).Get(ctx, &contextHistory)
		if err != nil {
			workflow.GetLogger(ctx).Error("Failed to load history", "error", err)
			contextHistory = []domain.Message{}
		}
	}

	fullHistory := append([]domain.Message{}, contextHistory...)
	fullHistory = append(fullHistory, domain.Message{Role: domain.RoleUser, Content: userQuery})

	// ── Phase 1: Plan ──
	input := activities.SupervisorInput{Query: userQuery, History: fullHistory, UserID: userID}
	var plan usecase.ExecutionPlan
	if err := workflow.ExecuteActivity(ctx, "PlannerDecide", input, streamID).Get(ctx, &plan); err != nil {
		return "", fmt.Errorf("planning failed: %w", err)
	}

	// Handle direct reply or empty plan (chitchat, no agents needed)
	if len(plan.Steps) == 0 {
		if plan.DirectReply == "" {
			plan.DirectReply = "无需执行特定任务，请直接回答用户的问题。"
		}
		_ = publishEvent(ctx, streamID, "step", "直接回复（无需调度下属）")
		promptForFinal := append(fullHistory, domain.Message{
			Role:    domain.RoleSystem,
			Content: fmt.Sprintf("请直接回复用户: %s", plan.DirectReply),
		})
		var finalAnswer string
		err := workflow.ExecuteActivity(ctx, "FinalReplyStream", promptForFinal, streamID, userID).Get(ctx, &finalAnswer)
		saveTurn(ctx, sessionID, userID, userQuery, finalAnswer)
		return finalAnswer, err
	}

	// Publish the plan to frontend
	planJSON, _ := json.Marshal(plan)
	_ = publishEvent(ctx, streamID, string(usecase.EventPlan), string(planJSON))
	_ = publishEvent(ctx, streamID, "step", fmt.Sprintf("📋 执行计划已生成，共 %d 个步骤", len(plan.Steps)))

	// ── Phase 2: Execute (parallel where possible) ──
	results := make(map[int]*domain.AgentResult)
	levels := buildLevels(plan.Steps)

	for levelIdx, level := range levels {
		workflow.GetLogger(ctx).Info("Executing level", "level", levelIdx, "steps", len(level))

		// Mark all steps in this level as "running"
		for _, step := range level {
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"running"}`, step.ID))
		}

		// ── Context Injection: build AgentInput with prior results for dependent steps ──
		agentInputs := make(map[int]domain.AgentInput)
		for _, step := range level {
			agentInput := domain.AgentInput{
				Instruction: step.Instruction,
				UserID:      userID,
			}
			for _, depID := range step.DependsOn {
				if r, ok := results[depID]; ok {
					agentInput.PriorResults = append(agentInput.PriorResults, *r)
				}
			}
			agentInputs[step.ID] = agentInput
		}

		// Launch all steps in this level concurrently via generic Activity names
		futures := make(map[int]workflow.Future)
		for _, step := range level {
			s := step
			_ = publishEvent(ctx, streamID, "step",
				fmt.Sprintf("▶ Step %d [%s]: %s", s.ID, s.Agent, s.Instruction))

			if s.RequiresApproval {
				futures[s.ID] = workflow.ExecuteActivity(ctx, "AgentPreview", s.Agent, agentInputs[s.ID])
			} else {
				futures[s.ID] = workflow.ExecuteActivity(ctx, "AgentExecute", s.Agent, agentInputs[s.ID], streamID)
			}
		}

		// Collect non-approvable results first (they never block on user input)
		var approvableSteps []usecase.PlanStep
		for _, step := range level {
			if step.RequiresApproval {
				approvableSteps = append(approvableSteps, step)
				continue
			}
			f, ok := futures[step.ID]
			if !ok {
				continue
			}
			stepStatus := "done"
			var result domain.AgentResult
			if err := f.Get(ctx, &result); err != nil {
				results[step.ID] = &domain.AgentResult{
					AgentName: step.Agent, StepID: step.ID,
					Summary: fmt.Sprintf("Error: %v", err), Error: err.Error(),
				}
				stepStatus = "error"
			} else {
				result.StepID = step.ID
				results[step.ID] = &result
			}
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"%s"}`, step.ID, stepStatus))
		}

		// Then handle approvable steps (may wait for human approval signal)
		for _, step := range approvableSteps {
			f, ok := futures[step.ID]
			if !ok {
				continue
			}
			stepStatus := "done"
			var previewContent string
			if err := f.Get(ctx, &previewContent); err != nil {
				results[step.ID] = &domain.AgentResult{
					AgentName: step.Agent, StepID: step.ID,
					Summary: fmt.Sprintf("Preview generation failed: %v", err), Error: err.Error(),
				}
				stepStatus = "error"
			} else if previewContent == "" {
				results[step.ID] = &domain.AgentResult{
					AgentName: step.Agent, StepID: step.ID, Summary: "No action needed.",
				}
			} else {
				// Send structured approval request with step identification
				approvalPayload, _ := json.Marshal(map[string]interface{}{
					"step_id": step.ID,
					"agent":   step.Agent,
					"code":    previewContent,
				})
				_ = publishEvent(ctx, streamID, "approval_required", string(approvalPayload))
				approvalCh := workflow.GetSignalChannel(ctx, SignalApprove)
				var signal ApprovalSignal
				approvalCh.Receive(ctx, &signal)

				if signal.Approved {
					contentToRun := previewContent
					if signal.ModifiedCode != "" {
						contentToRun = signal.ModifiedCode
					}
					_ = publishEvent(ctx, streamID, "step", "✅ 已批准，执行中...")
					var execResult domain.AgentResult
					if err := workflow.ExecuteActivity(ctx, "AgentExecuteApproved", step.Agent, contentToRun).Get(ctx, &execResult); err != nil {
						results[step.ID] = &domain.AgentResult{
							AgentName: step.Agent, StepID: step.ID,
							Summary: fmt.Sprintf("Execution failed: %v", err), Error: err.Error(),
						}
						stepStatus = "error"
					} else {
						execResult.StepID = step.ID
						results[step.ID] = &execResult
					}
				} else {
					results[step.ID] = &domain.AgentResult{
						AgentName: step.Agent, StepID: step.ID,
						Summary: "User REJECTED: " + signal.Reason,
					}
					_ = publishEvent(ctx, streamID, "step", "❌ 用户拒绝了执行")
					stepStatus = "error"
				}
			}
			_ = publishEvent(ctx, streamID, string(usecase.EventStepComplete),
				fmt.Sprintf(`{"id":%d,"status":"%s"}`, step.ID, stepStatus))
		}
	}

	// ── Send structured artifacts to frontend ──
	var allArtifacts []domain.Artifact
	for _, r := range results {
		if r != nil && len(r.Artifacts) > 0 {
			allArtifacts = append(allArtifacts, r.Artifacts...)
		}
	}
	if len(allArtifacts) > 0 {
		artifactsJSON, _ := json.Marshal(allArtifacts)
		_ = publishEvent(ctx, streamID, string(usecase.EventArtifacts), string(artifactsJSON))
	}

	// ── Phase 3: Final summary ──
	var summaryHistory []domain.Message
	summaryHistory = append(summaryHistory, fullHistory...)
	for _, step := range plan.Steps {
		summary := ""
		if r, ok := results[step.ID]; ok && r != nil {
			summary = r.Summary
		}
		summaryHistory = append(summaryHistory, domain.Message{
			Role:    domain.RoleUser,
			Content: fmt.Sprintf("【%s 汇报 (Step %d)】:\n%s", step.Agent, step.ID, summary),
		})
	}
	summaryHistory = append(summaryHistory, domain.Message{
		Role:    domain.RoleSystem,
		Content: "所有步骤已完成。请综合以上所有汇报，给用户一个完整、友好的最终回复。重要：如果汇报中包含 Markdown 图片标签（如 ![描述](/images/xxx.png)），你必须在回复中原样保留这些图片标签。",
	})

	var finalAnswer string
	err := workflow.ExecuteActivity(ctx, "FinalReplyStream", summaryHistory, streamID, userID).Get(ctx, &finalAnswer)
	saveTurn(ctx, sessionID, userID, userQuery, finalAnswer)
	return finalAnswer, err
}

// buildLevels groups plan steps into execution levels based on dependencies.
// Steps in the same level have no mutual dependencies and can run in parallel.
// Uses slice index (not step ID) to track progress, preventing infinite loops
// when duplicate step IDs are present.
func buildLevels(steps []usecase.PlanStep) [][]usecase.PlanStep {
	if len(steps) == 0 {
		return nil
	}

	n := len(steps)
	placedIdx := make([]bool, n)
	completedIDs := make(map[int]bool)
	var levels [][]usecase.PlanStep
	totalPlaced := 0

	for totalPlaced < n {
		var level []usecase.PlanStep
		var levelIdxs []int
		for i, s := range steps {
			if placedIdx[i] {
				continue
			}
			ready := true
			for _, dep := range s.DependsOn {
				if !completedIDs[dep] {
					ready = false
					break
				}
			}
			if ready {
				level = append(level, s)
				levelIdxs = append(levelIdxs, i)
			}
		}
		if len(level) == 0 {
			for i, s := range steps {
				if !placedIdx[i] {
					level = append(level, s)
					levelIdxs = append(levelIdxs, i)
				}
			}
		}
		levels = append(levels, level)
		for _, idx := range levelIdxs {
			placedIdx[idx] = true
			completedIDs[steps[idx].ID] = true
		}
		totalPlaced += len(levelIdxs)
	}
	return levels
}

func publishEvent(ctx workflow.Context, streamID, eventType, content string) error {
	return workflow.ExecuteActivity(ctx, "PublishStreamEvent", streamID, eventType, content).Get(ctx, nil)
}

func saveTurn(ctx workflow.Context, sessionID, userID, query, answer string) {
	if sessionID == "" || answer == "" {
		return
	}
	saveInput := activities.SaveChatInput{
		SessionID:   sessionID,
		UserID:      userID,
		UserQuery:   query,
		FinalAnswer: answer,
	}
	_ = workflow.ExecuteActivity(ctx, "SaveChatTurn", saveInput).Get(ctx, nil)
}
