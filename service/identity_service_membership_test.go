package service

import (
	"context"
	"errors"
	"testing"
)

// membershipIdentity 仅实现 TenantMembershipChecker 的测试身份源：其余
// IdentityService 方法经 nil 嵌入接口占位（EnsureUserInTenant 只会走 IsUserInTenant）。
type membershipIdentity struct {
	IdentityService
	inTenant bool
	err      error
	calls    int
}

func (m *membershipIdentity) IsUserInTenant(_ context.Context, _ string, _ string) (bool, error) {
	m.calls++
	return m.inTenant, m.err
}

// batchMembershipIdentity 仅实现 TenantMembershipBatchChecker 的测试身份源。
type batchMembershipIdentity struct {
	IdentityService
	notInTenant map[string]bool
	err         error
	calls       int
}

func (m *batchMembershipIdentity) AreUsersInTenant(_ context.Context, _ string, userIDs []string) (map[string]bool, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		out[id] = !m.notInTenant[id]
	}
	return out, nil
}

// dualMembershipIdentity 同时实现逐人/批量两个可选接口：批量失败降级逐人的用例用。
type dualMembershipIdentity struct {
	membershipIdentity
	notInTenant map[string]bool
	batchErr    error
	batchCalls  int
}

func (d *dualMembershipIdentity) AreUsersInTenant(_ context.Context, _ string, userIDs []string) (map[string]bool, error) {
	d.batchCalls++
	if d.batchErr != nil {
		return nil, d.batchErr
	}
	out := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		out[id] = !d.notInTenant[id]
	}
	return out, nil
}

func TestTenantMembershipGuard_EnsureUserInTenant(t *testing.T) {
	ctx := context.Background()

	// identity 为 nil：引擎无用户目录，跳过校验（不报错）
	if err := NewTenantMembershipGuard(nil).EnsureUserInTenant(ctx, "t1", "u1"); err != nil {
		t.Fatalf("nil identity should skip check, got %v", err)
	}

	// IdentityService 未实现 TenantMembershipChecker：跳过校验（不报错）
	if err := NewTenantMembershipGuard(&IdentityServiceImpl{}).EnsureUserInTenant(ctx, "t1", "u1"); err != nil {
		t.Fatalf("non-checker identity should skip check, got %v", err)
	}

	// 空 userID：直接校验失败，不触发 SPI 调用
	if err := NewTenantMembershipGuard(&membershipIdentity{inTenant: true}).EnsureUserInTenant(ctx, "t1", ""); err == nil {
		t.Fatal("empty userID should fail validation")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("empty userID should be ErrValidation, got %v", err)
	}

	// 在租户内：通过
	if err := NewTenantMembershipGuard(&membershipIdentity{inTenant: true}).EnsureUserInTenant(ctx, "t1", "u1"); err != nil {
		t.Fatalf("in-tenant user should pass, got %v", err)
	}

	// 不在租户内：ErrPermissionDenied
	if err := NewTenantMembershipGuard(&membershipIdentity{inTenant: false}).EnsureUserInTenant(ctx, "t1", "u1"); err == nil {
		t.Fatal("out-of-tenant user should fail")
	} else if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("out-of-tenant user should be ErrPermissionDenied, got %v", err)
	}

	// checker 查询错误：原样透传（fail-closed）
	checkErr := errors.New("identity backend unavailable")
	if err := NewTenantMembershipGuard(&membershipIdentity{err: checkErr}).EnsureUserInTenant(ctx, "t1", "u1"); err == nil {
		t.Fatal("checker error should propagate")
	} else if !errors.Is(err, checkErr) {
		t.Fatalf("checker error should wrap original, got %v", err)
	}
}

func TestTenantMembershipGuard_CheckUsersInTenant(t *testing.T) {
	ctx := context.Background()

	// identity 为 nil：全部放行（缺口由装配期探测统一告警）
	res := NewTenantMembershipGuard(nil).CheckUsersInTenant(ctx, "t1", []string{"u1", "u2"})
	if res["u1"] != nil || res["u2"] != nil {
		t.Fatalf("nil identity should allow all, got %v", res)
	}

	// 未实现任何可选接口：全部放行
	res = NewTenantMembershipGuard(&IdentityServiceImpl{}).CheckUsersInTenant(ctx, "t1", []string{"u1"})
	if res["u1"] != nil {
		t.Fatalf("non-checker identity should allow all, got %v", res["u1"])
	}

	// 批量路径：去重后单次查询，越租户逐项拒绝，空 id 直接 ErrValidation
	batch := &batchMembershipIdentity{notInTenant: map[string]bool{"u2": true}}
	res = NewTenantMembershipGuard(batch).CheckUsersInTenant(ctx, "t1", []string{"u1", "u2", "u2", "", "u1"})
	if res["u1"] != nil {
		t.Fatalf("in-tenant user should pass, got %v", res["u1"])
	}
	if !errors.Is(res["u2"], ErrPermissionDenied) {
		t.Fatalf("out-of-tenant user should be ErrPermissionDenied, got %v", res["u2"])
	}
	if !errors.Is(res[""], ErrValidation) {
		t.Fatalf("empty id should be ErrValidation, got %v", res[""])
	}
	if batch.calls != 1 {
		t.Fatalf("batch checker should be called once, got %d", batch.calls)
	}

	// 批量查询失败：降级逐人，失败语义保持（在租户内放行）
	dual := &dualMembershipIdentity{
		membershipIdentity: membershipIdentity{inTenant: true},
		notInTenant:        map[string]bool{},
		batchErr:           errors.New("batch down"),
	}
	res = NewTenantMembershipGuard(dual).CheckUsersInTenant(ctx, "t1", []string{"u1", "u2"})
	if res["u1"] != nil || res["u2"] != nil {
		t.Fatalf("fallback should allow in-tenant users, got %v", res)
	}
	if dual.batchCalls != 1 || dual.calls != 2 {
		t.Fatalf("expected 1 batch call + 2 per-user fallback calls, got batch=%d perUser=%d", dual.batchCalls, dual.calls)
	}
}

func TestTenantMembershipGuard_Validate(t *testing.T) {
	// 已实现 checker：通过（严格/非严格皆然，且不产生错误）
	if err := NewTenantMembershipGuard(&membershipIdentity{}).Validate(false); err != nil {
		t.Fatalf("checker implemented should pass, got %v", err)
	}
	if err := NewTenantMembershipGuard(&membershipIdentity{}).Validate(true); err != nil {
		t.Fatalf("checker implemented strict should pass, got %v", err)
	}

	// 未实现：非严格只告警放行
	if err := NewTenantMembershipGuard(&IdentityServiceImpl{}).Validate(false); err != nil {
		t.Fatalf("non-strict should not fail, got %v", err)
	}

	// 未实现：严格模式拒绝启动
	if err := NewTenantMembershipGuard(&IdentityServiceImpl{}).Validate(true); err == nil {
		t.Fatal("strict mode should fail without checker")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("strict failure should wrap ErrValidation, got %v", err)
	}

	// identity 为 nil：与非实现同等处理
	if err := NewTenantMembershipGuard(nil).Validate(false); err != nil {
		t.Fatalf("nil identity non-strict should not fail, got %v", err)
	}
	if err := NewTenantMembershipGuard(nil).Validate(true); err == nil {
		t.Fatal("nil identity strict should fail")
	}
}
