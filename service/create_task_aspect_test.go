package service

import (
	"context"
	"testing"

	"github.com/rulego/gflow-engine/dao"
	"github.com/rulego/gflow-engine/model"
	"github.com/rulego/gflow-engine/types/constants"
	"github.com/rulego/gflow-engine/utils/lock"
	"github.com/rulego/rulego/api/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 测试替身：仅覆写被测路径调用的方法。
// ---------------------------------------------------------------------------

type endNodeCtx struct{ types.NodeCtx }

func (f *endNodeCtx) Type() string { return types.NodeTypeEnd }

type fakeRuleContext struct {
	types.RuleContext
	node   types.NodeCtx
	selfID string
}

func (f *fakeRuleContext) Self() types.NodeCtx         { return f.node }
func (f *fakeRuleContext) GetSelfId() string           { return f.selfID }
func (f *fakeRuleContext) GetContext() context.Context { return context.Background() }
func (f *fakeRuleContext) RuleChain() types.NodeCtx    { return nil }

type fakeTaskService struct{ TaskService }

func (f *fakeTaskService) CreateTask(_ context.Context, _ Actor, task *model.WfTask) (string, error) {
	task.ID = "task-1"
	return "task-1", nil
}

func (f *fakeTaskService) Complete(_ context.Context, _ Actor, _ string, _ map[string]interface{}) error {
	return nil
}

type fakeRuntimeService struct {
	RuntimeService
	completed string
}

func (f *fakeRuntimeService) CompleteProcessInstance(_ context.Context, _ Actor, id, _ string) error {
	f.completed = id
	return nil
}

type fakeEngine struct {
	WorkflowEngine
	locker  lock.Locker
	taskSvc TaskService
	rtSvc   RuntimeService
}

func (f *fakeEngine) GetLocker() lock.Locker                  { return f.locker }
func (f *fakeEngine) GetTaskService() TaskService             { return f.taskSvc }
func (f *fakeEngine) GetRuntimeService() RuntimeService       { return f.rtSvc }
func (f *fakeEngine) GetRuleChainExecutor() RuleChainExecutor { return nil }

// end 节点去重锁服务异常时，实例仍应完成归档。
func TestTaskCreator_EndDedupLockErrorStillCompletesInstance(t *testing.T) {
	q := rtImplTestDB(t)
	rtSvc := &fakeRuntimeService{}
	aspect := &TaskCreator{
		instanceDAO: dao.NewInstanceDAOWithQuery(q),
		workflowEngine: &fakeEngine{
			locker:  &failingLocker{},
			taskSvc: &fakeTaskService{},
			rtSvc:   rtSvc,
		},
	}

	md := types.NewMetadata()
	md.PutValue(constants.KeyInstanceID, "inst-end")
	md.PutValue(constants.KeyProcessID, "proc-1")
	md.PutValue(constants.KeyTenantID, "t1")
	msg := types.NewMsg(0, "wf", types.JSON, md, `{}`)
	rctx := &fakeRuleContext{node: &endNodeCtx{}, selfID: "end1"}

	// 锁服务异常时 Before 仍须写入非空 KeyEndExecLock。
	out := aspect.Before(rctx, msg, types.Success)
	require.NotEmpty(t, out.GetMetadata().GetValue(constants.KeyEndExecLock),
		"lock service error must still mark KeyEndExecLock")

	// 锁服务异常时 After 仍应完成实例归档。
	aspect.After(rctx, out, nil, types.Success)
	require.Equal(t, "inst-end", rtSvc.completed, "instance must complete even when end-dedup lock errors")
}
