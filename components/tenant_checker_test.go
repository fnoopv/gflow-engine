package components

import (
	"context"
	"sync"

	"github.com/rulego/gflow-engine/service"
)

// fakeTenantChecker 测试用身份源：仅实现 TenantMembershipChecker，其余 IdentityService
// 方法经 nil 嵌入接口占位（被测路径只走 IsUserInTenant）。默认所有用户"在租户内"，
// 可用 deny 标为跨租户、reset 清空。
type fakeTenantChecker struct {
	service.IdentityService
	mu          sync.Mutex
	notInTenant map[string]bool
}

func (f *fakeTenantChecker) IsUserInTenant(_ context.Context, _ string, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.notInTenant[userID], nil
}

func (f *fakeTenantChecker) deny(userIDs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notInTenant == nil {
		f.notInTenant = map[string]bool{}
	}
	for _, id := range userIDs {
		f.notInTenant[id] = true
	}
}

func (f *fakeTenantChecker) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notInTenant = nil
}
