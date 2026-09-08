/*
 * Copyright 2025 The RuleGo Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"context"

	"github.com/rulego/gflow-engine/types/constants"
)

// Actor 操作人：引擎公共 API 的变更类操作全部以显式 actor 参数传入。
type Actor struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	TenantID string `json:"tenantId"`
	// WorkflowAdmin：工作流管理员（持 workflow:instance:view 的运营/管理角色）。
	// 实例详情 IDOR 校验对其放行（管理侧需要查看所有实例）；普通审批用户不设此标记。
	// 仅由宿主服务端按角色判定设置；json:"-" 禁止反序列化，防止客户端伪造管理员标记。
	WorkflowAdmin bool `json:"-"`
}

// SystemActor 引擎内部机制（节点自动推进/巡检等）代替用户执行时的操作人，
// 用户 ID 取 constants.UserSystem。
func SystemActor() Actor {
	return Actor{UserID: constants.UserSystem, UserName: constants.UserSystem}
}

// IsSystemActor 判断操作人是否为系统身份：系统代表平台自身操作，不冒充任何用户，
// 不受"仅本人可操作"类限制（租户校验等照常执行）。注意 ActivateProcessInstance
// 等入口经 bindActor 把 actor 写进 ctx，系统上下文里 GetUserFromCtx 永远非 nil，
// 判断系统身份必须用本方法，不能依赖"ctx 无用户"。
func IsSystemActor(a *Actor) bool {
	return a != nil && a.UserID == constants.UserSystem
}

// ActorFromCtx 取 ctx 已绑定操作人，未绑定返回 SystemActor。
// 供引擎内部回调（aspect/节点/级联）使用，保留原身份可维持租户校验与事件归属。
func ActorFromCtx(ctx context.Context) Actor {
	if u := GetUserFromCtx(ctx); u != nil {
		return *u
	}
	return SystemActor()
}

// bindActor 把显式 actor 绑进 ctx 并标记调用模式。
// ctx 已带 CallingModeInternal 时保留内部模式，避免节点自动推进被误判为 API 入口。
func bindActor(ctx context.Context, actor Actor) context.Context {
	ctx = SetUserToCtx(ctx, &actor)
	if GetCallingMode(ctx) == CallingModeUnknown {
		ctx = WithAPICallingMode(ctx)
	}
	return ctx
}

// bindActorAPI 在 bindActor 之上把携带真实用户的内部标记 ctx 降级为 API 模式
// （见 forceAPICallingModeForRealUser），防宿主复用内部 ctx 绕过 assignee/属主校验。
// 无引擎内部级联的公共入口用本方法；TerminateProcessInstance 等依赖内部模式豁免
// 属主校验的入口必须保持 bindActor。
func bindActorAPI(ctx context.Context, actor Actor) context.Context {
	return forceAPICallingModeForRealUser(bindActor(ctx, actor))
}
