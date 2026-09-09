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
	// batchErr 非空时批量查询返回该错误，模拟宿主批量实现故障（降级逐人路径用）
	batchErr error
	// batchCalls 批量查询调用计数：断言整单名单只查一次（无逐人 N+1）
	batchCalls int
}

func (f *fakeTenantChecker) IsUserInTenant(_ context.Context, _ string, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.notInTenant[userID], nil
}

func (f *fakeTenantChecker) AreUsersInTenant(_ context.Context, _ string, userIDs []string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls++
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	out := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		out[id] = !f.notInTenant[id]
	}
	return out, nil
}

func (f *fakeTenantChecker) batchCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.batchCalls
}

func (f *fakeTenantChecker) failBatch(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchErr = err
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
	f.batchErr = nil
	f.batchCalls = 0
}
