package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rulego/gflow-engine/model"
)

// authzCtx 组装带操作人 + 调用模式的 ctx，供属主/归属校验单测使用。
func authzCtx(actor *Actor, mode CallingMode) context.Context {
	ctx := context.Background()
	if mode != CallingModeUnknown {
		ctx = SetCallingMode(ctx, mode)
	}
	if actor != nil {
		ctx = SetUserToCtx(ctx, actor)
	}
	return ctx
}

func TestRequireInstanceOwnerAuthorized(t *testing.T) {
	inst := &model.WfInstance{StartUserID: "owner", TenantID: "t1"}

	cases := []struct {
		name    string
		actor   *Actor
		mode    CallingMode
		wantErr error
	}{
		{"owner allowed", &Actor{UserID: "owner", TenantID: "t1"}, CallingModeAPI, nil},
		{"super admin allowed", &Actor{UserID: "admin", TenantID: "t1", WorkflowAdmin: true}, CallingModeAPI, nil},
		{"system allowed", &Actor{UserID: "system"}, CallingModeAPI, nil},
		{"non-owner same tenant denied", &Actor{UserID: "eve", TenantID: "t1"}, CallingModeAPI, ErrPermissionDenied},
		{"nil actor denied", nil, CallingModeAPI, ErrAuthenticationRequired},
		{"empty user denied", &Actor{UserID: "", TenantID: "t1"}, CallingModeAPI, ErrAuthenticationRequired},
		{"internal cascade skips owner check", &Actor{UserID: "eve", TenantID: "t1"}, CallingModeInternal, nil},
		{"unknown mode treated as external", &Actor{UserID: "eve", TenantID: "t1"}, CallingModeUnknown, ErrPermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireInstanceOwnerAuthorized(authzCtx(tc.actor, tc.mode), inst)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
		})
	}

	// 系统触发（StartUserID 为空）的实例：非管理员/非系统用户不得操作。
	t.Run("empty starter non-admin denied", func(t *testing.T) {
		err := requireInstanceOwnerAuthorized(
			authzCtx(&Actor{UserID: "eve", TenantID: "t1"}, CallingModeAPI),
			&model.WfInstance{StartUserID: "", TenantID: "t1"})
		require.ErrorIs(t, err, ErrPermissionDenied)
	})
}

func TestRequireTaskOperatorAuthorized(t *testing.T) {
	assignee := "owner"
	assigned := &model.WfTask{Assignee: &assignee, TenantID: "t1"}
	unassigned := &model.WfTask{Assignee: nil, TenantID: "t1"}

	cases := []struct {
		name    string
		task    *model.WfTask
		actor   *Actor
		wantErr error
	}{
		{"assignee allowed", assigned, &Actor{UserID: "owner", TenantID: "t1"}, nil},
		{"super admin allowed", assigned, &Actor{UserID: "admin", TenantID: "t1", WorkflowAdmin: true}, nil},
		{"system allowed", assigned, &Actor{UserID: "system"}, nil},
		{"non-assignee denied", assigned, &Actor{UserID: "eve", TenantID: "t1"}, ErrPermissionDenied},
		{"unassigned non-admin denied", unassigned, &Actor{UserID: "eve", TenantID: "t1"}, ErrPermissionDenied},
		{"nil actor denied", assigned, nil, ErrAuthenticationRequired},
		{"empty user denied", assigned, &Actor{UserID: "", TenantID: "t1"}, ErrAuthenticationRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireTaskOperatorAuthorized(SetUserToCtx(context.Background(), tc.actor), tc.task)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

// 额外的编译期/语义哨兵：确保 ErrPermissionDenied 与 ErrAuthenticationRequired
// 都可被 errors.Is 捕获（防止 helper 用 fmt.Errorf 裸字符串拼接丢链）。
func TestOperatorAuthzErrorsWrapSentinel(t *testing.T) {
	require.True(t, errors.Is(
		requireInstanceOwnerAuthorized(authzCtx(&Actor{UserID: "eve", TenantID: "t1"}, CallingModeAPI), &model.WfInstance{StartUserID: "owner", TenantID: "t1"}),
		ErrPermissionDenied))
	require.True(t, errors.Is(
		requireInstanceOwnerAuthorized(authzCtx(nil, CallingModeAPI), &model.WfInstance{StartUserID: "owner", TenantID: "t1"}),
		ErrAuthenticationRequired))
}
