package service

import (
	"context"
	"errors"
	"testing"
)

// membershipIdentity 仅实现 TenantMembershipChecker 的测试身份源：sqrt 其余
// IdentityService 方法经 nil 嵌入接口占位（EnsureUserInTenant 只会走 IsUserInTenant）。
type membershipIdentity struct {
	IdentityService
	inTenant bool
	err      error
}

func (m *membershipIdentity) IsUserInTenant(_ context.Context, _ string, _ string) (bool, error) {
	return m.inTenant, m.err
}

func TestEnsureUserInTenant(t *testing.T) {
	ctx := context.Background()

	// identity 为 nil：引擎无用户目录，跳过校验（不报错）
	if err := EnsureUserInTenant(ctx, nil, "t1", "u1"); err != nil {
		t.Fatalf("nil identity should skip check, got %v", err)
	}

	// IdentityService 未实现 TenantMembershipChecker：跳过校验（不报错）
	if err := EnsureUserInTenant(ctx, &IdentityServiceImpl{}, "t1", "u1"); err != nil {
		t.Fatalf("non-checker identity should skip check, got %v", err)
	}

	// 空 userID：直接校验失败，不触发 SPI 调用
	if err := EnsureUserInTenant(ctx, &membershipIdentity{inTenant: true}, "t1", ""); err == nil {
		t.Fatal("empty userID should fail validation")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty userID should be ErrValidation, got %v", err)
	}

	// 在租户内：通过
	if err := EnsureUserInTenant(ctx, &membershipIdentity{inTenant: true}, "t1", "u1"); err != nil {
		t.Fatalf("in-tenant user should pass, got %v", err)
	}

	// 不在租户内：ErrPermissionDenied
	if err := EnsureUserInTenant(ctx, &membershipIdentity{inTenant: false}, "t1", "u1"); err == nil {
		t.Fatal("out-of-tenant user should fail")
	} else if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("out-of-tenant user should be ErrPermissionDenied, got %v", err)
	}

	// checker 查询错误：原样透传（fail-closed）
	checkErr := errors.New("identity backend unavailable")
	if err := EnsureUserInTenant(ctx, &membershipIdentity{err: checkErr}, "t1", "u1"); err == nil {
		t.Fatal("checker error should propagate")
	} else if !errors.Is(err, checkErr) {
		t.Fatalf("checker error should wrap original, got %v", err)
	}
}
