package lock

import (
	"context"
	"encoding/hex"
	"testing"
	"time"
)

func TestNewLocalLock(t *testing.T) {
	l := NewLocalLock()
	if l == nil {
		t.Fatal("expected non-nil LocalLock")
	}
}

func TestLocalLock_BasicLockUnlock(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	value, err := l.Lock(ctx, "test-key", 10*time.Second)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	if value == "" {
		t.Error("Lock returned empty value")
	}

	err = l.Unlock(ctx, "test-key", value)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestLocalLock_WrongValueUnlock(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	_, _ = l.Lock(ctx, "key1", 10*time.Second)
	err := l.Unlock(ctx, "key1", "wrong-value")
	if err == nil {
		t.Error("expected error for wrong value")
	}
}

func TestLocalLock_UnlockUnknownKey(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	err := l.Unlock(ctx, "unknown-key", "any-value")
	if err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestLocalLock_TryLock(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	value, ok, err := l.TryLock(ctx, "try-key", 10*time.Second)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Error("TryLock should succeed on first attempt")
	}

	// Second TryLock on same key should fail
	_, ok, _ = l.TryLock(ctx, "try-key", 10*time.Second)
	if ok {
		t.Error("TryLock should fail when key is already locked")
	}

	// Unlock and try again
	_ = l.Unlock(ctx, "try-key", value)
	_, ok, _ = l.TryLock(ctx, "try-key", 10*time.Second)
	if !ok {
		t.Error("TryLock should succeed after unlock")
	}
}

func TestLocalLock_LockWithRetry(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	// Lock the key
	value1, _ := l.Lock(ctx, "retry-key", 10*time.Second)

	// Try to acquire with retries - should fail since key is locked
	_, err := l.LockWithRetry(ctx, "retry-key", 10*time.Second, 10*time.Millisecond, 3)
	if err == nil {
		t.Error("expected error when key is locked")
	}

	// Unlock
	_ = l.Unlock(ctx, "retry-key", value1)

	// Now retry should succeed
	value2, err := l.LockWithRetry(ctx, "retry-key", 10*time.Second, 10*time.Millisecond, 3)
	if err != nil {
		t.Fatalf("LockWithRetry failed after unlock: %v", err)
	}
	if value2 == "" {
		t.Error("LockWithRetry returned empty value")
	}
}

func TestLocalLock_ExpiredLock(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	// Lock with short expiration
	value, _ := l.Lock(ctx, "expire-key", 50*time.Millisecond)

	// Should be locked
	_, ok, _ := l.TryLock(ctx, "expire-key", 10*time.Second)
	if ok {
		t.Error("key should be locked")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be available now
	_, ok, _ = l.TryLock(ctx, "expire-key", 10*time.Second)
	if !ok {
		t.Error("key should be available after expiration")
	}

	// Clean up
	_ = l.Unlock(ctx, "expire-key", value)
}

func TestLocalLock_CancelledContext(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	// Lock the key
	_, _ = l.Lock(ctx, "cancel-key", 10*time.Second)

	// Create a cancelled context
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	// Lock on already-locked key with cancelled context should fail
	_, err := l.Lock(cancelCtx, "cancel-key", 10*time.Second)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestLocalLock_DifferentKeys(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	v1, err := l.Lock(ctx, "key-a", 10*time.Second)
	if err != nil {
		t.Fatalf("Lock key-a failed: %v", err)
	}
	v2, err := l.Lock(ctx, "key-b", 10*time.Second)
	if err != nil {
		t.Fatalf("Lock key-b failed: %v", err)
	}

	// Both should have different values
	if v1 == v2 {
		t.Error("different keys should have different values")
	}

	// Unlock both
	_ = l.Unlock(ctx, "key-a", v1)
	_ = l.Unlock(ctx, "key-b", v2)
}

func TestDefaultKeyLockIsNotNil(t *testing.T) {
	if DefaultKeyLock == nil {
		t.Error("DefaultKeyLock should not be nil")
	}
}

// 回归（H3）：等待者 B 在持有者 A 正常释放后抢到的锁，必须带"未来"有效期。
// 修复前 expiredAt 在循环外只算一次，B 等久后把已过期的陈旧值装进 LoadOrStore，
// 导致锁即刻被判过期、被 CAS 抢占 → 两个 goroutine 同时进临界区。
func TestLocalLock_LockAfterWaitUsesFreshExpiry(t *testing.T) {
	l := NewLocalLock()
	ctx := context.Background()

	// 持有者 A：长有效期，靠 Unlock 正常释放（而非自然过期）。
	vA, err := l.Lock(ctx, "stale-expiry", time.Hour)
	if err != nil {
		t.Fatalf("holder lock failed: %v", err)
	}

	// 等待者 B：短有效期。B 抢锁时的 expiredAt 取决于是"入口时刻"还是"抢占时刻"。
	const shortTTL = 40 * time.Millisecond
	type result struct {
		value string
		err   error
	}
	acquired := make(chan result, 1)
	go func() {
		vB, err := l.Lock(ctx, "stale-expiry", shortTTL)
		acquired <- result{vB, err}
	}()

	// 让 B 等待超过 shortTTL，使"入口时刻"算出的 expiredAt 成为过去时。
	time.Sleep(80 * time.Millisecond)

	// A 正常释放（CompareAndDelete），B 的下一次 LoadOrStore 会装上自己的锁。
	if err := l.Unlock(ctx, "stale-expiry", vA); err != nil {
		t.Fatalf("holder unlock failed: %v", err)
	}

	var got result
	select {
	case got = <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not acquire after holder release")
	}
	if got.err != nil {
		t.Fatalf("waiter lock failed: %v", got.err)
	}
	if got.value == "" {
		t.Fatal("waiter lock returned empty value")
	}

	// 关键断言：B 刚抢到锁，锁必须仍未过期，TryLock 必须失败。
	// 若 B 用陈旧 expiredAt 装锁，锁即刻过期，TryLock 会 CAS 抢占成功（双持有者）。
	_, ok, err := l.TryLock(ctx, "stale-expiry", time.Hour)
	if err != nil {
		t.Fatalf("tryLock failed: %v", err)
	}
	if ok {
		t.Fatal("freshly acquired lock is immediately expired (second holder acquired)")
	}
}

func TestGenerateLockValue(t *testing.T) {
	v1, err := generateLockValue()
	if err != nil {
		t.Fatalf("generateLockValue failed: %v", err)
	}
	if len(v1) != 32 {
		t.Fatalf("expected 32-char hex value, got len=%d (%q)", len(v1), v1)
	}
	if _, err := hex.DecodeString(v1); err != nil {
		t.Fatalf("expected hex-encoded value: %v", err)
	}

	v2, err := generateLockValue()
	if err != nil {
		t.Fatalf("second generateLockValue failed: %v", err)
	}
	if v1 == v2 {
		t.Error("two generated values should differ")
	}
}
