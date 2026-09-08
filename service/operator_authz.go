package service

import (
	"context"
	"fmt"

	"github.com/rulego/gflow-engine/model"
)

// 本文件是引擎鉴权的唯一收口：租户隔离、属主、办理人、管理员/系统身份的全部
// 口径集中在此（helper 带错误返回、谓词只做判定）。新增入口的鉴权必须复用或
// 扩展这里的定义，不得在业务方法里内联展开身份判断——口径调整时只改这一处。
//
// 口径总览：
//   - ensureTenantAccess               单资源读的跨租户隔离
//   - requireNonEmptyTenantForRealUser 列表/批量入口对真实用户的空租户 fail-closed
//   - requireInstanceOwnerAuthorized   实例级变更＝发起人/管理员/系统
//   - requireTaskOperatorAuthorized    任务级变更＝办理人/管理员/系统
//   - requireAdminIdentity             管理操作＝管理员/系统（改派/候选人池/定义变更）
//   - requireInspectionTenant          巡检＝管理员/系统，且非系统须携带租户
//   - isWorkflowAdmin / isAdminOrSystem / isInstanceStarterOrAdmin  谓词形态

// ---------------------------------------------------------------------------
// 谓词
// ---------------------------------------------------------------------------

// isWorkflowAdmin 判断操作人是否为宿主置位的工作流管理员（Actor.WorkflowAdmin）。
// 仅判管理员标记、不含系统身份：系统路径在各入口显式分支（如 DeleteTask 的
// isSystem 前置），避免把两类身份的语义混在一个谓词里。
func isWorkflowAdmin(a *Actor) bool {
	return a != nil && a.WorkflowAdmin
}

// isAdminOrSystem 管理员或系统身份谓词，放行集与 requireAdminIdentity 一致，
// 供已加载身份、只差判定的路径（委派归还校验、全量恢复巡检）使用。
func isAdminOrSystem(a *Actor) bool {
	return a != nil && (a.WorkflowAdmin || IsSystemActor(a))
}

// isInstanceStarterOrAdmin 实例发起人或管理员谓词（撤回、未指派任务删除等
// 属主口径）。空发起人按非属主处理，与 requireInstanceOwnerAuthorized 一致。
func isInstanceStarterOrAdmin(instance *model.WfInstance, userID string, isAdmin bool) bool {
	return isAdmin || (instance.StartUserID != "" && instance.StartUserID == userID)
}

// ---------------------------------------------------------------------------
// helper（带错误返回）
// ---------------------------------------------------------------------------

// requireInstanceOwnerAuthorized 校验流程实例级变更（生命周期：终止/挂起/删除/激活/重启/
// 强恢复/重驱动/救援；实例变量写入：SetProcessInstanceVariables 等）的属主/管理员权限。
// 放行三类：实例发起人自己、管理员（WorkflowAdmin）、系统身份；其余（含无操作人）一律拒绝。
//
// 引擎内部级联（CallingModeInternal，如失败终止 handleProcessInstanceFailure、驳回
// 级联终止 terminateInstance、AI 节点输出缓存持久化）携带的是"参与该实例的真实用户"
// 而非发起人，属流程语义驱动的副作用，不套用属主规则——这些调用方已显式以
// WithInternalCallingMode 标记 ctx，本方法对其跳过，与 ensureTenantAccess（租户仍照常
// 校验）互不干扰。
func requireInstanceOwnerAuthorized(ctx context.Context, instance *model.WfInstance) error {
	if GetCallingMode(ctx) == CallingModeInternal {
		return nil
	}
	u := GetUserFromCtx(ctx)
	if u == nil || u.UserID == "" {
		return fmt.Errorf("operator identity required: %w", ErrAuthenticationRequired)
	}
	if isAdminOrSystem(u) {
		return nil
	}
	if instance.StartUserID != "" && instance.StartUserID == u.UserID {
		return nil
	}
	return fmt.Errorf("only the process initiator (or admin) can perform this operation: %w", ErrPermissionDenied)
}

// requireTaskOperatorAuthorized 校验任务属性变更（优先级/截止时间/评论文本等）的
// 操作权限：任务归属人（assignee）、管理员或系统身份。无操作人、或任务未指派给
// 当前操作人的任务一律拒绝（fail-closed）。带 assignee 的任务仅其本人（或管理员/系统）
// 可改；未指派任务仅管理员/系统可改。
//
// 无 CallingModeInternal 豁免：引擎内部没有改任务属性的级联路径，
// 内部 ctx 带真实用户进来即宿主误用，按规则照常判定。
func requireTaskOperatorAuthorized(ctx context.Context, task *model.WfTask) error {
	u := GetUserFromCtx(ctx)
	if u == nil || u.UserID == "" {
		return fmt.Errorf("operator identity required: %w", ErrAuthenticationRequired)
	}
	if isAdminOrSystem(u) {
		return nil
	}
	if task.Assignee != nil && *task.Assignee != "" && *task.Assignee == u.UserID {
		return nil
	}
	return fmt.Errorf("task is not assigned to current user: %w", ErrPermissionDenied)
}

// requireAdminIdentity 校验操作人为工作流管理员（Actor.WorkflowAdmin）或系统身份。
// 用于跳过 assignee/候选人校验的强制改派类管理操作（Reassign/SetAssignee/SetOwner）
// 与流程定义变更类操作（Deploy/Create/Update/Delete/Activate/UpdateStatus）：
// 这些操作绕过"仅本人可操作"语义，引擎内部无其他鉴权点，必须在入口强制校验，
// 否则任意同租户用户可拿到 taskID 即劫持他人任务。
//
// 系统身份（IsSystemActor）放行，供定时巡检/跨服务级联等引擎内部机制使用；
// 管理员身份由宿主服务端按角色判定后置 WorkflowAdmin 标记（该字段带 json:"-"，
// 无法从请求体反序列化伪造）。
func requireAdminIdentity(actor *Actor) error {
	if actor == nil || actor.UserID == "" {
		return fmt.Errorf("admin identity required: %w", ErrAuthenticationRequired)
	}
	if isAdminOrSystem(actor) {
		return nil
	}
	return fmt.Errorf("operation requires admin or system identity: %w", ErrPermissionDenied)
}

// requireInspectionTenant 校验巡检端点（卡死实例/超期 delay 任务）的操作人身份，
// 返回巡检租户。仅管理员（WorkflowAdmin）或系统身份可巡；系统身份空租户＝平台级
// 全租户扫描（定时巡检跨租户是设计内行为），非系统身份必须携带本租户，杜绝
// 调用方自定裸 tenantID 越权扫其它租户（原形参 tenantID 为空即全租户）。
func requireInspectionTenant(actor *Actor) (string, error) {
	if err := requireAdminIdentity(actor); err != nil {
		return "", err
	}
	if !IsSystemActor(actor) && actor.TenantID == "" {
		return "", fmt.Errorf("tenant ID required for non-system inspection: %w", ErrValidation)
	}
	return actor.TenantID, nil
}

// requireNonEmptyTenantForRealUser 对非系统身份的空租户 fail-closed。列表/批量只读
// DAO 遇空租户会跳过租户过滤退化为跨租户全量（与单读 ensureTenantAccess 的空租户
// 拒绝不一致），故真实用户必须携带租户；系统身份空租户放行（平台级巡检/管理视角
// 跨租户扫描是设计内行为）。
func requireNonEmptyTenantForRealUser(actor *Actor) error {
	if actor == nil {
		return fmt.Errorf("operator identity required: %w", ErrAuthenticationRequired)
	}
	if !IsSystemActor(actor) && actor.TenantID == "" {
		return fmt.Errorf("tenant ID required: %w", ErrValidation)
	}
	return nil
}

// ensureTenantAccess 校验 ctx 操作人与资源属同租户。resourceDesc 用于错误信息
// （如 "process instance"）。跨租户按 ErrPermissionDenied 拒绝；需要隐藏资源
// 存在性的路径（claim/withdraw 等）仍应各自按 ErrNotFound 处理，不走本方法。
//
// 放行三类：资源侧租户为空（单租户部署/历史数据）；显式系统身份（IsSystemActor，
// 平台自身跨租户操作是设计内的，如定时巡检、级联清理）；ctx 无 actor——引擎
// 内部级联（aspect/节点回调）不带 actor，与 ActorFromCtx 的"无用户视为系统"
// 约定一致。API 入口都经 bindActor 绑定操作人，真正要拦的是"半构造 actor"：
// UserID 非空但租户为空（无租户 claim 的旧 token、漏传租户的调用方）。
func ensureTenantAccess(ctx context.Context, resourceDesc, resourceTenantID string) error {
	if resourceTenantID == "" {
		return nil
	}
	u := GetUserFromCtx(ctx)
	if u == nil || IsSystemActor(u) {
		return nil
	}
	if u.TenantID == resourceTenantID {
		return nil
	}
	return fmt.Errorf("%s belongs to another tenant: %w", resourceDesc, ErrPermissionDenied)
}
