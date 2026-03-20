package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	workflowdomain "aegis/internal/domain/workflow"
	pgrepo "aegis/internal/repository/postgres"
	"github.com/expr-lang/expr"
	"github.com/go-resty/resty/v2"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

const (
	temporalManagedWorkflowType             = "aegis.workflow.managed"
	temporalScheduleLauncherWorkflowType    = "aegis.workflow.schedule_launcher"
	temporalActivityCreateScheduledInstance = "aegis.workflow.activity.create_scheduled_instance"
	temporalActivityUpdateInstanceState     = "aegis.workflow.activity.update_instance_state"
	temporalActivityResolveNextNode         = "aegis.workflow.activity.resolve_next_node"
	temporalActivityCreateHumanTask         = "aegis.workflow.activity.create_human_task"
	temporalActivityExecuteWebhook          = "aegis.workflow.activity.execute_webhook"
	temporalActivityCreateLog               = "aegis.workflow.activity.create_log"
	temporalSignalTaskComplete              = "task.complete"
	temporalSignalControl                   = "control"
)

type temporalWorkflowInput struct {
	AppID        int64
	WorkflowID   int64
	WorkflowName string
	InstanceID   int64
	InstanceName string
	Priority     int
	StartedBy    *int64
	InputData    map[string]any
	OutputData   map[string]any
	Definition   workflowdomain.Definition
}

type temporalScheduledLaunchInput struct {
	AppID        int64
	WorkflowID   int64
	WorkflowName string
	Priority     int
	InputData    map[string]any
	Definition   workflowdomain.Definition
}

type temporalInstanceStateInput struct {
	AppID         int64
	WorkflowID    int64
	InstanceID    int64
	Status        string
	CurrentNodeID string
	OutputData    map[string]any
	ErrorMessage  string
	EndedAt       *time.Time
}

type temporalResolveNextNodeInput struct {
	AppID      int64
	WorkflowID int64
	Definition workflowdomain.Definition
	NodeID     string
	InputData  map[string]any
	OutputData map[string]any
}

type temporalCreateTaskInput struct {
	AppID      int64
	WorkflowID int64
	InstanceID int64
	NodeID     string
	NodeName   string
	Priority   int
	AssignedTo *int64
	InputData  map[string]any
	FormSchema map[string]any
}

type temporalWebhookInput struct {
	AppID      int64
	WorkflowID int64
	InstanceID int64
	URL        string
	Method     string
	InputData  map[string]any
	OutputData map[string]any
}

type temporalWebhookResult struct {
	StatusCode int
	Body       string
	OutputData map[string]any
}

type temporalTaskCompleteSignal struct {
	TaskID  int64
	Output  map[string]any
	Comment string
}

type temporalControlSignal struct {
	Action string
}

type temporalWorkflowActivities struct {
	log    *zap.Logger
	pg     *pgrepo.Repository
	client *resty.Client
}

func RegisterTemporalWorkflowEngine(w worker.Worker, log *zap.Logger, pg *pgrepo.Repository) {
	activities := &temporalWorkflowActivities{log: log, pg: pg, client: resty.New().SetTimeout(10 * time.Second).SetRetryCount(1)}
	w.RegisterWorkflowWithOptions(TemporalManagedWorkflow, workflow.RegisterOptions{Name: temporalManagedWorkflowType})
	w.RegisterWorkflowWithOptions(TemporalScheduleLauncherWorkflow, workflow.RegisterOptions{Name: temporalScheduleLauncherWorkflowType})
	w.RegisterActivityWithOptions(activities.CreateScheduledInstance, activity.RegisterOptions{Name: temporalActivityCreateScheduledInstance})
	w.RegisterActivityWithOptions(activities.UpdateInstanceState, activity.RegisterOptions{Name: temporalActivityUpdateInstanceState})
	w.RegisterActivityWithOptions(activities.ResolveNextNode, activity.RegisterOptions{Name: temporalActivityResolveNextNode})
	w.RegisterActivityWithOptions(activities.CreateHumanTask, activity.RegisterOptions{Name: temporalActivityCreateHumanTask})
	w.RegisterActivityWithOptions(activities.ExecuteWebhook, activity.RegisterOptions{Name: temporalActivityExecuteWebhook})
	w.RegisterActivityWithOptions(activities.CreateLog, activity.RegisterOptions{Name: temporalActivityCreateLog})
}

func TemporalScheduleLauncherWorkflow(ctx workflow.Context, input temporalScheduledLaunchInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 5 * time.Second, MaximumAttempts: 3},
	})
	var managedInput temporalWorkflowInput
	if err := workflow.ExecuteActivity(ctx, temporalActivityCreateScheduledInstance, input).Get(ctx, &managedInput); err != nil {
		return err
	}
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{WorkflowID: temporalInstanceWorkflowID(managedInput.AppID, managedInput.WorkflowID, managedInput.InstanceID)})
	return workflow.ExecuteChildWorkflow(childCtx, temporalManagedWorkflowType, managedInput).Get(childCtx, nil)
}

func TemporalManagedWorkflow(ctx workflow.Context, input temporalWorkflowInput) (map[string]any, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 5 * time.Second, MaximumAttempts: 3},
	})
	output := cloneWorkflowMap(input.OutputData)
	currentNodeID := firstStartNode(input.Definition)
	if currentNodeID == "" {
		now := workflow.Now(ctx)
		_ = workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "failed", ErrorMessage: "工作流缺少开始节点", EndedAt: &now}).Get(ctx, nil)
		return output, fmt.Errorf("workflow start node missing")
	}
	controlCh := workflow.GetSignalChannel(ctx, temporalSignalControl)
	taskCh := workflow.GetSignalChannel(ctx, temporalSignalTaskComplete)
	paused := false
	for step := 0; step < 128; step++ {
		var err error
		paused, err = drainControlSignals(ctx, controlCh, input, currentNodeID, output, paused)
		if err != nil {
			return output, err
		}
		for paused {
			paused, err = waitForResume(ctx, controlCh, input, currentNodeID, output)
			if err != nil {
				return output, err
			}
		}
		node, ok := findNodeByID(input.Definition, currentNodeID)
		if !ok {
			return failManagedWorkflow(ctx, input, output, currentNodeID, "节点不存在: "+currentNodeID)
		}
		if err := workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "running", CurrentNodeID: currentNodeID, OutputData: output}).Get(ctx, nil); err != nil {
			return output, err
		}
		switch node.Type {
		case "start", "condition":
			nextID, err := resolveNextNode(ctx, input, currentNodeID, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			currentNodeID = nextID
		case "webhook":
			result, err := executeWebhookNode(ctx, input, node, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			mergeWorkflowMap(output, result.OutputData)
			_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, Level: "info", Event: "webhook_executed", Message: "Webhook 节点执行完成", Metadata: map[string]any{"nodeId": node.ID, "statusCode": result.StatusCode}}).Get(ctx, nil)
			nextID, err := resolveNextNode(ctx, input, currentNodeID, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			currentNodeID = nextID
		case "task", "user_task", "approval", "form":
			task, err := createHumanTask(ctx, input, node)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			taskResult, stillPaused, err := waitForTaskCompletion(ctx, controlCh, taskCh, input, currentNodeID, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			paused = stillPaused
			if taskResult.TaskID != 0 {
				mergeWorkflowMap(output, taskResult.Output)
				_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, TaskID: &task.ID, Level: "info", Event: "task_signal_consumed", Message: "人工任务信号已接收"}).Get(ctx, nil)
			}
			nextID, err := resolveNextNode(ctx, input, currentNodeID, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			currentNodeID = nextID
		case "end":
			now := workflow.Now(ctx)
			_ = workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "completed", CurrentNodeID: currentNodeID, OutputData: output, EndedAt: &now}).Get(ctx, nil)
			_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, Level: "info", Event: "instance_completed", Message: "工作流执行完成"}).Get(ctx, nil)
			return output, nil
		default:
			nextID, err := resolveNextNode(ctx, input, currentNodeID, output)
			if err != nil {
				return failManagedWorkflow(ctx, input, output, currentNodeID, err.Error())
			}
			if nextID == "" {
				now := workflow.Now(ctx)
				_ = workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "completed", CurrentNodeID: currentNodeID, OutputData: output, EndedAt: &now}).Get(ctx, nil)
				return output, nil
			}
			currentNodeID = nextID
		}
	}
	return failManagedWorkflow(ctx, input, output, currentNodeID, "工作流执行步数超限")
}

func (a *temporalWorkflowActivities) CreateScheduledInstance(ctx context.Context, input temporalScheduledLaunchInput) (temporalWorkflowInput, error) {
	if input.Priority <= 0 {
		input.Priority = 5
	}
	startedAt := time.Now()
	instance, err := a.pg.CreateWorkflowInstance(ctx, workflowdomain.Instance{
		WorkflowID: input.WorkflowID, AppID: input.AppID, InstanceName: input.WorkflowName + "-schedule", Status: "running",
		Priority: input.Priority, InputData: input.InputData, OutputData: map[string]any{}, StartedAt: &startedAt,
	})
	if err != nil {
		return temporalWorkflowInput{}, err
	}
	_ = a.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{
		AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &instance.ID, Level: "info",
		Event: "instance_started", Message: "计划任务触发工作流实例", Metadata: map[string]any{"source": "temporal_schedule"},
	})
	return temporalWorkflowInput{
		AppID: input.AppID, WorkflowID: input.WorkflowID, WorkflowName: input.WorkflowName, InstanceID: instance.ID,
		InstanceName: instance.InstanceName, Priority: input.Priority, InputData: input.InputData, OutputData: map[string]any{}, Definition: input.Definition,
	}, nil
}

func (a *temporalWorkflowActivities) UpdateInstanceState(ctx context.Context, input temporalInstanceStateInput) error {
	instance, err := a.pg.GetWorkflowInstance(ctx, input.AppID, input.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("workflow instance %d not found", input.InstanceID)
	}
	if strings.TrimSpace(input.Status) != "" {
		instance.Status = input.Status
	}
	if input.CurrentNodeID != "" {
		instance.CurrentNodeID = input.CurrentNodeID
	}
	if input.OutputData != nil {
		instance.OutputData = input.OutputData
	}
	if input.ErrorMessage != "" {
		instance.ErrorMessage = input.ErrorMessage
	}
	if input.EndedAt != nil {
		instance.EndedAt = input.EndedAt
	}
	return a.pg.UpdateWorkflowInstance(ctx, *instance)
}

func (a *temporalWorkflowActivities) ResolveNextNode(_ context.Context, input temporalResolveNextNodeInput) (string, error) {
	edges := make([]workflowdomain.Edge, 0, 2)
	for _, edge := range input.Definition.Edges {
		if edge.Source == input.NodeID {
			edges = append(edges, edge)
		}
	}
	if len(edges) == 0 {
		return "", nil
	}
	env := map[string]any{"input": input.InputData, "output": input.OutputData, "workflowId": input.WorkflowID, "appid": input.AppID}
	for _, edge := range edges {
		if strings.TrimSpace(edge.Condition) == "" {
			return edge.Target, nil
		}
		program, err := expr.Compile(edge.Condition, expr.Env(env))
		if err != nil {
			continue
		}
		result, err := expr.Run(program, env)
		if err != nil {
			continue
		}
		if passed, ok := result.(bool); ok && passed {
			return edge.Target, nil
		}
	}
	return edges[0].Target, nil
}

func (a *temporalWorkflowActivities) CreateHumanTask(ctx context.Context, input temporalCreateTaskInput) (workflowdomain.Task, error) {
	task, err := a.pg.CreateWorkflowTask(ctx, workflowdomain.Task{
		WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, AppID: input.AppID, NodeID: input.NodeID,
		Name: firstNonEmpty(input.NodeName, "人工任务"), Type: "user_task", Status: "pending", Priority: input.Priority,
		AssignedTo: input.AssignedTo, InputData: input.InputData, FormSchema: input.FormSchema,
	})
	if err != nil {
		return workflowdomain.Task{}, err
	}
	_ = a.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{
		AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, TaskID: &task.ID, Level: "info",
		Event: "task_created", Message: "人工任务已创建", Metadata: map[string]any{"nodeId": input.NodeID},
	})
	return *task, nil
}

func (a *temporalWorkflowActivities) ExecuteWebhook(ctx context.Context, input temporalWebhookInput) (temporalWebhookResult, error) {
	resp, err := a.client.R().SetContext(ctx).SetHeader("Content-Type", "application/json").SetBody(map[string]any{
		"appid": input.AppID, "workflowId": input.WorkflowID, "instanceId": input.InstanceID, "input": input.InputData, "output": input.OutputData,
	}).Execute(strings.ToUpper(firstNonEmpty(input.Method, http.MethodPost)), strings.TrimSpace(input.URL))
	if err != nil {
		return temporalWebhookResult{}, err
	}
	if !resp.IsSuccess() {
		return temporalWebhookResult{}, fmt.Errorf("webhook returned status %d", resp.StatusCode())
	}
	return temporalWebhookResult{StatusCode: resp.StatusCode(), Body: resp.String(), OutputData: map[string]any{"lastWebhookStatus": resp.StatusCode(), "lastWebhookBody": resp.String()}}, nil
}

func (a *temporalWorkflowActivities) CreateLog(ctx context.Context, entry workflowdomain.LogEntry) error {
	return a.pg.CreateWorkflowLog(ctx, entry)
}

func resolveNextNode(ctx workflow.Context, input temporalWorkflowInput, currentNodeID string, output map[string]any) (string, error) {
	var nextID string
	err := workflow.ExecuteActivity(ctx, temporalActivityResolveNextNode, temporalResolveNextNodeInput{
		AppID: input.AppID, WorkflowID: input.WorkflowID, Definition: input.Definition, NodeID: currentNodeID, InputData: input.InputData, OutputData: output,
	}).Get(ctx, &nextID)
	return nextID, err
}

func createHumanTask(ctx workflow.Context, input temporalWorkflowInput, node workflowdomain.Node) (workflowdomain.Task, error) {
	assignedTo := int64FromAny(node.Config["assignedTo"])
	var assigned *int64
	if assignedTo > 0 {
		assigned = &assignedTo
	} else if input.StartedBy != nil {
		assigned = input.StartedBy
	}
	var task workflowdomain.Task
	err := workflow.ExecuteActivity(ctx, temporalActivityCreateHumanTask, temporalCreateTaskInput{
		AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, NodeID: node.ID, NodeName: node.Name,
		Priority: input.Priority, AssignedTo: assigned, InputData: input.InputData, FormSchema: mapFromAny(node.Config["formSchema"]),
	}).Get(ctx, &task)
	return task, err
}

func executeWebhookNode(ctx workflow.Context, input temporalWorkflowInput, node workflowdomain.Node, output map[string]any) (temporalWebhookResult, error) {
	var result temporalWebhookResult
	err := workflow.ExecuteActivity(ctx, temporalActivityExecuteWebhook, temporalWebhookInput{
		AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, URL: stringFromAny(node.Config["url"]),
		Method: firstNonEmpty(stringFromAny(node.Config["method"]), http.MethodPost), InputData: input.InputData, OutputData: output,
	}).Get(ctx, &result)
	return result, err
}

func waitForTaskCompletion(ctx workflow.Context, controlCh workflow.ReceiveChannel, taskCh workflow.ReceiveChannel, input temporalWorkflowInput, currentNodeID string, output map[string]any) (temporalTaskCompleteSignal, bool, error) {
	paused := false
	for {
		if paused {
			var err error
			paused, err = waitForResume(ctx, controlCh, input, currentNodeID, output)
			if err != nil {
				return temporalTaskCompleteSignal{}, paused, err
			}
			continue
		}
		var completed temporalTaskCompleteSignal
		if taskCh.ReceiveAsync(&completed) {
			return completed, paused, nil
		}
		var control temporalControlSignal
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(taskCh, func(c workflow.ReceiveChannel, more bool) { c.Receive(ctx, &completed) })
		selector.AddReceive(controlCh, func(c workflow.ReceiveChannel, more bool) { c.Receive(ctx, &control) })
		selector.Select(ctx)
		if completed.TaskID != 0 {
			return completed, paused, nil
		}
		var err error
		paused, err = applyControlSignal(ctx, input, currentNodeID, output, paused, control)
		if err != nil {
			return temporalTaskCompleteSignal{}, paused, err
		}
	}
}

func drainControlSignals(ctx workflow.Context, controlCh workflow.ReceiveChannel, input temporalWorkflowInput, currentNodeID string, output map[string]any, paused bool) (bool, error) {
	var control temporalControlSignal
	for controlCh.ReceiveAsync(&control) {
		var err error
		paused, err = applyControlSignal(ctx, input, currentNodeID, output, paused, control)
		if err != nil {
			return paused, err
		}
		control = temporalControlSignal{}
	}
	return paused, nil
}

func waitForResume(ctx workflow.Context, controlCh workflow.ReceiveChannel, input temporalWorkflowInput, currentNodeID string, output map[string]any) (bool, error) {
	for {
		var control temporalControlSignal
		controlCh.Receive(ctx, &control)
		paused, err := applyControlSignal(ctx, input, currentNodeID, output, true, control)
		if err != nil {
			return true, err
		}
		if !paused {
			return false, nil
		}
	}
}

func applyControlSignal(ctx workflow.Context, input temporalWorkflowInput, currentNodeID string, output map[string]any, paused bool, control temporalControlSignal) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(control.Action)) {
	case "pause":
		if paused {
			return true, nil
		}
		if err := workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "paused", CurrentNodeID: currentNodeID, OutputData: output}).Get(ctx, nil); err != nil {
			return true, err
		}
		_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, Level: "info", Event: "instance_paused", Message: "工作流实例进入暂停状态"}).Get(ctx, nil)
		return true, nil
	case "resume":
		if !paused {
			return false, nil
		}
		if err := workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "running", CurrentNodeID: currentNodeID, OutputData: output}).Get(ctx, nil); err != nil {
			return false, err
		}
		_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, Level: "info", Event: "instance_resumed", Message: "工作流实例已恢复执行"}).Get(ctx, nil)
		return false, nil
	default:
		return paused, nil
	}
}

func failManagedWorkflow(ctx workflow.Context, input temporalWorkflowInput, output map[string]any, currentNodeID string, message string) (map[string]any, error) {
	now := workflow.Now(ctx)
	_ = workflow.ExecuteActivity(ctx, temporalActivityUpdateInstanceState, temporalInstanceStateInput{
		AppID: input.AppID, WorkflowID: input.WorkflowID, InstanceID: input.InstanceID, Status: "failed", CurrentNodeID: currentNodeID,
		OutputData: output, ErrorMessage: message, EndedAt: &now,
	}).Get(ctx, nil)
	_ = workflow.ExecuteActivity(ctx, temporalActivityCreateLog, workflowdomain.LogEntry{
		AppID: input.AppID, WorkflowID: &input.WorkflowID, InstanceID: &input.InstanceID, Level: "error",
		Event: "instance_failed", Message: message, Metadata: map[string]any{"nodeId": currentNodeID},
	}).Get(ctx, nil)
	return output, errors.New(message)
}

func findNodeByID(def workflowdomain.Definition, nodeID string) (workflowdomain.Node, bool) {
	for _, node := range def.Nodes {
		if node.ID == nodeID {
			return node, true
		}
	}
	return workflowdomain.Node{}, false
}

func cloneWorkflowMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for _, key := range workflow.DeterministicKeys(input) {
		output[key] = input[key]
	}
	return output
}

func mergeWorkflowMap(dst map[string]any, src map[string]any) {
	if src == nil || dst == nil {
		return
	}
	for _, key := range workflow.DeterministicKeys(src) {
		dst[key] = src[key]
	}
}

func firstStartNode(def workflowdomain.Definition) string {
	for _, node := range def.Nodes {
		if node.Type == "start" {
			return node.ID
		}
	}
	return ""
}
