package service

import (
	"context"
	"fmt"

	"github.com/rulego/gflow-engine/model"
)

// requireInstanceOwnerAuthorized 校验流程实例级生命周期变更（终止/挂起/删除/激活/重启/
// 强恢复/重驱动/救援）的属主/管理员权限。放行三类：实例发起人自己、管理员
// （SuperAdmin）、系统身份；其余（含无操作人）一律拒绝（fail-closed）。
//
// 引擎内部级联（CallingModeInternal，如失败终止 handleProcessInstanceFailure、驳回
// 级联终止 terminateInstance）携带的是"参与该实例的真实用户"而非发起人，属流程语义
// 驱动的副作用，不套用属主规则——这些调用方已显式以 WithInternalCallingMode 标记 ctx，
// 本方法对其跳过，与 ensureTenantAccess（租户仍照常校验）互不干扰。
func requireInstanceOwnerAuthorized(ctx context.Context, instance *model.WfInstance) error {
	if GetCallingMode(ctx) == CallingModeInternal {
		return nil
	}
	u := GetUserFromCtx(ctx)
	if u == nil || u.UserID == "" {
		return fmt.Errorf("operator identity required: %w", ErrAuthenticationRequired)
	}
	if u.SuperAdmin || IsSystemActor(u) {
		return nil
	}
	if instance.StartUserID != "" && instance.StartUserID == u.UserID {
		return nil
	}
	return fmt.Errorf("only the process initiator (or admin) can perform this operation: %w", ErrPermissionDenied)
}

// requireTaskOperatorAuthorized 校验任务属性变更（优先级/截止时间等）的操作权限：
// 任务归属人（assignee）、管理员或系统身份。无操作人、或任务未指派给当前操作人
// 的任务一律拒绝（fail-closed）。带 assignee 的任务仅其本人（或管理员/系统）可改；
// 未指派任务仅管理员/系统可改。
func requireTaskOperatorAuthorized(ctx context.Context, task *model.WfTask) error {
	u := GetUserFromCtx(ctx)
	if u == nil || u.UserID == "" {
		return fmt.Errorf("operator identity required: %w", ErrAuthenticationRequired)
	}
	if u.SuperAdmin || IsSystemActor(u) {
		return nil
	}
	if task.Assignee != nil && *task.Assignee != "" && *task.Assignee == u.UserID {
		return nil
	}
	return fmt.Errorf("task is not assigned to current user: %w", ErrPermissionDenied)
}
