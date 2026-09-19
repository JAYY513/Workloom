package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"workloom/internal/app"
	"workloom/internal/config"
)

// toolSpec is one registrable tool: its name, the profiles that expose it,
// its tier (core tools are the daily subset; the rest is standard), and the
// registration function that binds it to the SDK server. An empty tier means
// standard: a tool must opt in to core explicitly, never by omission.
type toolSpec struct {
	name     string
	profiles []string
	tier     string
	register func(*mcpsdk.Server, Config)
}

// allTools lists every tool the server can expose. Registration is the only
// place profile membership is decided.
func allTools() []toolSpec {
	return []toolSpec{
		// health
		{"health", []string{ProfileSession}, TierCore, registerHealth},

		// project (方案 §8.2 project_*)
		{"project_list", []string{ProfileSession}, "", registerProjectList},
		{"project_get", []string{ProfileSession}, TierCore, registerProjectGet},
		{"project_create", []string{ProfileAdmin}, "", registerProjectCreate},
		{"project_update", []string{ProfileAdmin}, "", registerProjectUpdate},
		{"project_state_update", []string{ProfileAdmin}, "", registerProjectStateUpdate},

		// workitem (方案 §8.2 workitem_*)
		{"workitem_list", []string{ProfileSession}, TierCore, registerWorkitemList},
		{"workitem_get", []string{ProfileSession}, TierCore, registerWorkitemGet},
		{"workitem_next", []string{ProfileSession}, "", registerWorkitemNext},
		{"workitem_create", []string{ProfileExecutor}, TierCore, registerWorkitemCreate},
		{"workitem_update", []string{ProfileExecutor}, "", registerWorkitemUpdate},
		{"workitem_transition", []string{ProfileExecutor}, TierCore, registerWorkitemTransition},
		{"workitem_claim", []string{ProfileExecutor}, TierCore, registerWorkitemClaim},
		{"workitem_release", []string{ProfileExecutor}, "", registerWorkitemRelease},
		{"workitem_start", []string{ProfileExecutor}, "", registerWorkitemStart},
		{"workitem_block", []string{ProfileExecutor}, "", registerWorkitemBlock},
		{"workitem_complete", []string{ProfileExecutor}, "", registerWorkitemComplete},
		{"workitem_comment", []string{ProfileExecutor}, TierCore, registerWorkitemComment},
		{"workitem_add_dependency", []string{ProfileExecutor}, "", registerWorkitemAddDependency},
		{"workitem_remove_dependency", []string{ProfileExecutor}, "", registerWorkitemRemoveDependency},

		// workflow (方案 §8.2 workflow_*)
		{"workflow_list", []string{ProfileSession}, "", registerWorkflowList},
		{"workflow_get", []string{ProfileSession}, "", registerWorkflowGet},
		{"workflow_start", []string{ProfileExecutor}, "", registerWorkflowStart},
		{"workflow_step_next", []string{ProfileSession}, "", registerWorkflowStepNext},
		{"workflow_step_complete", []string{ProfileExecutor}, "", registerWorkflowStepComplete},
		{"workflow_pause", []string{ProfileExecutor}, "", registerWorkflowPause},
		{"workflow_resume", []string{ProfileExecutor}, "", registerWorkflowResume},
		{"workflow_cancel", []string{ProfileExecutor}, "", registerWorkflowCancel},

		// approval (方案 §4.9/§8.2 approval_*)
		{"approval_list", []string{ProfileSession}, "", registerApprovalList},
		{"approval_get", []string{ProfileSession}, "", registerApprovalGet},
		{"approval_request", []string{ProfileExecutor}, TierCore, registerApprovalRequest},
		{"approval_decide", []string{ProfileReviewer}, "", registerApprovalDecide},

		// records (方案 §8.2 decision_*/finding_*/event_*/artifact_*)
		{"decision_list", []string{ProfileSession}, "", registerDecisionList},
		{"decision_get", []string{ProfileSession}, "", registerDecisionGet},
		{"decision_create", []string{ProfileExecutor}, TierCore, registerDecisionCreate},
		{"decision_approve", []string{ProfileReviewer}, "", registerDecisionApprove},
		{"finding_list", []string{ProfileSession}, "", registerFindingList},
		{"finding_get", []string{ProfileSession}, "", registerFindingGet},
		{"finding_create", []string{ProfileExecutor}, TierCore, registerFindingCreate},
		{"finding_resolve", []string{ProfileExecutor}, "", registerFindingResolve},
		{"event_list", []string{ProfileSession}, "", registerEventList},
		{"event_record", []string{ProfileExecutor}, TierCore, registerEventRecord},
		{"artifact_list", []string{ProfileSession}, "", registerArtifactList},
		{"artifact_get", []string{ProfileSession}, "", registerArtifactGet},
		{"artifact_register", []string{ProfileExecutor}, TierCore, registerArtifactRegister},
		{"artifact_update", []string{ProfileExecutor}, "", registerArtifactUpdate},
		{"artifact_history", []string{ProfileSession}, "", registerArtifactHistory},

		// runs (方案 §8.2 run_*；生命周期推进属 M6.3/M6.6)
		{"run_list", []string{ProfileSession}, "", registerRunList},
		{"run_get", []string{ProfileSession}, "", registerRunGet},
		{"run_log", []string{ProfileSession}, "", registerRunLog},
		{"run_create", []string{ProfileExecutor}, TierCore, registerRunCreate},
		{"run_update", []string{ProfileExecutor}, "", registerRunUpdate},
		{"run_heartbeat", []string{ProfileExecutor}, "", registerRunHeartbeat},
		{"run_verify", []string{ProfileSession}, TierCore, registerRunVerify},
		{"run_complete", []string{ProfileExecutor}, TierCore, registerRunFinish},
		{"run_fail", []string{ProfileExecutor}, "", registerRunFinish},
		{"run_cancel", []string{ProfileExecutor}, "", registerRunFinish},

		// context and knowledge (方案 §8.2 context_*/knowledge_*)
		{"context_get", []string{ProfileSession}, TierCore, registerContextGet},
		{"context_for_workitem", []string{ProfileSession}, "", registerContextForWorkitem},
		{"context_refresh", []string{ProfileSession}, "", registerContextRefresh},
		{"context_compact", []string{ProfileSession}, "", registerContextCompact},
		{"agent_session_start", []string{ProfileSession}, TierCore, registerAgentSessionStart},
		{"knowledge_status", []string{ProfileSession}, TierCore, registerKnowledgeStatus},
		{"knowledge_validate", []string{ProfileSession}, "", registerKnowledgeValidate},
		{"knowledge_refresh", []string{ProfileSession}, "", registerKnowledgeRefresh},
	}
}

// toolError converts a service failure into the structured tool error both
// surfaces share: code is the four-class taxonomy (usage, precondition,
// invalid, internal), problems keep the same located shape the CLI prints.
type toolError struct {
	Code     string           `json:"code"`
	Message  string           `json:"message"`
	Problems []config.Problem `json:"problems,omitempty"`
	// Notice carries a last-known-good fallback note that accompanied a
	// refusal, so the client sees why the policy in force was not the
	// current file.
	Notice string `json:"notice,omitempty"`
}

// fail renders an error as an isError tool result. The SDK wraps plain
// handler errors the same way, but that path loses the structure; returning
// the payload ourselves keeps code and problems machine-readable.
func fail[Out any](err error) (*mcpsdk.CallToolResult, Out, error) {
	var zero Out
	payload := toolError{Code: "internal", Message: err.Error()}
	var ae *app.Error
	if errors.As(err, &ae) {
		payload.Code = ae.Class()
		payload.Message = ae.Message
		payload.Problems = ae.Problems
	}
	body, merr := json.MarshalIndent(payload, "", "  ")
	if merr != nil {
		body = []byte(fmt.Sprintf(`{"code":"internal","message":%q}`, err.Error()))
	}
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(body)}},
	}, zero, nil
}

// failNotice renders an error together with the notice that accompanied it.
func failNotice[Out any](err error, notice string) (*mcpsdk.CallToolResult, Out, error) {
	res, out, herr := fail[Out](err)
	if notice != "" && res != nil && len(res.Content) == 1 {
		if text, ok := res.Content[0].(*mcpsdk.TextContent); ok {
			var payload toolError
			if json.Unmarshal([]byte(text.Text), &payload) == nil {
				payload.Notice = notice
				if body, merr := json.MarshalIndent(payload, "", "  "); merr == nil {
					text.Text = string(body)
				}
			}
		}
	}
	return res, out, herr
}

// usageFail reports invalid input the schema could not express.
func usageFail[Out any](format string, a ...any) (*mcpsdk.CallToolResult, Out, error) {
	return fail[Out](app.Usagef(format, a...))
}

// readOnly marks a tool that never mutates state; clients use it to decide
// whether a call needs approval.
func readOnly() *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{ReadOnlyHint: true}
}
