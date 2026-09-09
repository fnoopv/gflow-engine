// This file contains small private helpers shared across TaskServiceImpl methods:
// rule-string accessor, task-to-history conversion, and the numeric
// type-coercion helper used by countersign progress aggregation.

package service

import (
	"context"
	"fmt"

	"github.com/rulego/gflow-engine/model"
)

// getApprovalRuleString 读取审批规则字符串（nil 安全，空串兜底）。
func (s *TaskServiceImpl) getApprovalRuleString(rule *string) string {
	if rule == nil {
		return ""
	}
	return *rule
}

// taskToHiTask converts a WfTask to WfHiTask for archiving to history.
func taskToHiTask(task *model.WfTask) *model.WfHiTask {
	return &model.WfHiTask{
		ID:                task.ID,
		ProcessInstanceID: task.ProcessInstanceID,
		ProcessID:         task.ProcessID,
		TaskDefKey:        &task.TaskDefKey,
		TaskType:          task.TaskType,
		Name:              task.Name,
		Description:       task.Description,
		ParentID:          task.ParentID,
		Status:            task.Status,
		Assignee:          task.Assignee,
		Owner:             task.Owner,
		DueDate:           task.DueDate,
		Priority:          task.Priority,
		FormKey:           task.FormKey,
		Variables:         task.Variables,
		ClaimedAt:         task.ClaimedAt,
		ApprovalType:      task.ApprovalType,
		ApprovalRule:      task.ApprovalRule,
		DelegateFrom:      task.DelegateFrom,
		DelegateReason:    task.DelegateReason,
		DelegateTime:      task.DelegateTime,
		EndedAt:           task.EndedAt,
		Comment:           task.Comment,
		EndReason:         task.EndReason,
		Duration:          task.Duration,
		TenantID:          task.TenantID,
		CreatedBy:         task.CreatedBy,
		CreatedAt:         task.CreatedAt,
		UpdatedBy:         task.UpdatedBy,
		UpdatedAt:         task.UpdatedAt,
		SequenceOrder:     task.SequenceOrder,
	}
}

// ensureTargetUserInTenant 转办/委派/改派/加签的目标用户租户归属校验。
// 统一走租户归属鉴权守卫（未实现 TenantMembershipChecker 时跳过，缺口由装配期
// TenantMembershipGuard.Validate 统一告警/严格模式拒绝），action 仅用于错误信息标注动作来源。
func (s *TaskServiceImpl) ensureTargetUserInTenant(ctx context.Context, task *model.WfTask, userID, action string) error {
	if s.workflowEngine == nil || task == nil {
		return nil
	}
	guard := NewTenantMembershipGuard(s.workflowEngine.GetIdentityService())
	if err := guard.EnsureUserInTenant(ctx, task.TenantID, userID); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}
