package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/config"
	workflowdomain "aegis/internal/domain/workflow"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

type WorkflowService struct {
	log         *zap.Logger
	pg          *pgrepo.Repository
	temporal    client.Client
	temporalCfg config.TemporalConfig
}

func NewWorkflowService(log *zap.Logger, pg *pgrepo.Repository, temporalClient client.Client, temporalCfg config.TemporalConfig) *WorkflowService {
	svc := &WorkflowService{log: log, pg: pg, temporal: temporalClient, temporalCfg: temporalCfg}
	_ = svc.RefreshSchedules(context.Background())
	return svc
}

func (s *WorkflowService) List(ctx context.Context, appID int64, query workflowdomain.ListQuery) (*workflowdomain.ListResult, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	items, total, err := s.pg.ListWorkflows(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	return &workflowdomain.ListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: calcPagesForService(total, limit)}, nil
}

func (s *WorkflowService) Detail(ctx context.Context, appID int64, workflowID int64) (*workflowdomain.Workflow, error) {
	item, err := s.pg.GetWorkflowByID(ctx, appID, workflowID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40480, http.StatusNotFound, "工作流不存在")
	}
	return item, nil
}

func (s *WorkflowService) Save(ctx context.Context, mutation workflowdomain.WorkflowMutation) (*workflowdomain.Workflow, error) {
	if _, err := s.requireApp(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetWorkflowByID(ctx, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := workflowdomain.Workflow{
		ID:            mutation.ID,
		AppID:         mutation.AppID,
		Name:          "新建工作流",
		Status:        "draft",
		Version:       1,
		Definition:    workflowdomain.Definition{},
		CreatedBy:     mutation.CreatedBy,
		UpdatedBy:     mutation.UpdatedBy,
		Permissions:   map[string]any{},
		UIConfig:      map[string]any{},
		TriggerConfig: map[string]any{},
	}
	if current != nil {
		item = *current
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.Category != nil {
		item.Category = strings.TrimSpace(*mutation.Category)
	}
	if mutation.Status != nil {
		item.Status = strings.TrimSpace(*mutation.Status)
	}
	if mutation.Definition != nil {
		item.Definition = *mutation.Definition
	}
	if mutation.TriggerConfig != nil {
		item.TriggerConfig = mutation.TriggerConfig
	}
	if mutation.UIConfig != nil {
		item.UIConfig = mutation.UIConfig
	}
	if mutation.Permissions != nil {
		item.Permissions = mutation.Permissions
	}
	if item.Name == "" {
		return nil, apperrors.New(40080, http.StatusBadRequest, "工作流名称不能为空")
	}
	if err := s.ValidateDefinition(item.Definition); err != nil {
		return nil, err
	}
	saved, err := s.pg.UpsertWorkflow(ctx, item)
	if err != nil {
		return nil, err
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{
		AppID:      saved.AppID,
		WorkflowID: &saved.ID,
		Level:      "info",
		Event:      "workflow_saved",
		Message:    "工作流已保存",
		Metadata:   map[string]any{"status": saved.Status, "version": saved.Version},
	})
	_ = s.syncWorkflowSchedule(ctx, saved)
	return saved, nil
}

func (s *WorkflowService) Delete(ctx context.Context, appID int64, workflowID int64) error {
	_ = s.deleteWorkflowSchedule(ctx, appID, workflowID)
	deleted, err := s.pg.DeleteWorkflow(ctx, appID, workflowID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40480, http.StatusNotFound, "工作流不存在")
	}
	return nil
}

func (s *WorkflowService) Start(ctx context.Context, appID int64, workflowID int64, startedBy *int64, inputData map[string]any, instanceName string, priority int) (*workflowdomain.Instance, error) {
	if s.temporal == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "工作流引擎不可用")
	}
	workflowItem, err := s.Detail(ctx, appID, workflowID)
	if err != nil {
		return nil, err
	}
	if workflowItem.Status != "active" && workflowItem.Status != "draft" {
		return nil, apperrors.New(40081, http.StatusBadRequest, "当前工作流状态不可执行")
	}
	if priority <= 0 {
		priority = 5
	}
	startedAt := time.Now()
	instance, err := s.pg.CreateWorkflowInstance(ctx, workflowdomain.Instance{
		WorkflowID: workflowID, AppID: appID, InstanceName: strings.TrimSpace(instanceName), Status: "running",
		Priority: priority, StartedBy: startedBy, InputData: inputData, OutputData: map[string]any{}, StartedAt: &startedAt,
	})
	if err != nil {
		return nil, err
	}
	input := temporalWorkflowInput{
		AppID: appID, WorkflowID: workflowID, WorkflowName: workflowItem.Name, InstanceID: instance.ID,
		InstanceName: instance.InstanceName, Priority: priority, StartedBy: startedBy, InputData: inputData,
		OutputData: map[string]any{}, Definition: workflowItem.Definition,
	}
	if err := s.executeManagedWorkflow(ctx, input); err != nil {
		now := time.Now()
		instance.Status = "failed"
		instance.ErrorMessage = err.Error()
		instance.EndedAt = &now
		_ = s.pg.UpdateWorkflowInstance(ctx, *instance)
		return nil, err
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{
		AppID: appID, WorkflowID: &workflowID, InstanceID: &instance.ID, Level: "info", Event: "instance_started",
		Message: "工作流实例已启动", Metadata: map[string]any{"priority": priority, "temporalWorkflowId": temporalInstanceWorkflowID(appID, workflowID, instance.ID)},
	})
	return instance, nil
}

func (s *WorkflowService) Instances(ctx context.Context, appID int64, query workflowdomain.InstanceQuery) (*workflowdomain.InstanceListResult, error) {
	items, total, err := s.pg.ListWorkflowInstances(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	return &workflowdomain.InstanceListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: calcPagesForService(total, limit)}, nil
}

func (s *WorkflowService) InstanceDetail(ctx context.Context, appID int64, instanceID int64) (*workflowdomain.Instance, error) {
	item, err := s.pg.GetWorkflowInstance(ctx, appID, instanceID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40481, http.StatusNotFound, "工作流实例不存在")
	}
	return item, nil
}

func (s *WorkflowService) PauseInstance(ctx context.Context, appID int64, instanceID int64) (*workflowdomain.Instance, error) {
	instance, err := s.InstanceDetail(ctx, appID, instanceID)
	if err != nil {
		return nil, err
	}
	instance.Status = "paused"
	if err := s.pg.UpdateWorkflowInstance(ctx, *instance); err != nil {
		return nil, err
	}
	if err := s.signalWorkflowControl(ctx, instance, temporalControlSignal{Action: "pause"}); err != nil {
		s.log.Warn("pause workflow signal failed", zap.Int64("appid", appID), zap.Int64("instance_id", instanceID), zap.Error(err))
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{AppID: appID, WorkflowID: &instance.WorkflowID, InstanceID: &instance.ID, Level: "info", Event: "instance_paused", Message: "工作流实例已暂停"})
	return instance, nil
}

func (s *WorkflowService) ResumeInstance(ctx context.Context, appID int64, instanceID int64) (*workflowdomain.Instance, error) {
	instance, err := s.InstanceDetail(ctx, appID, instanceID)
	if err != nil {
		return nil, err
	}
	instance.Status = "running"
	if err := s.pg.UpdateWorkflowInstance(ctx, *instance); err != nil {
		return nil, err
	}
	if err := s.signalWorkflowControl(ctx, instance, temporalControlSignal{Action: "resume"}); err != nil {
		s.log.Warn("resume workflow signal failed", zap.Int64("appid", appID), zap.Int64("instance_id", instanceID), zap.Error(err))
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{AppID: appID, WorkflowID: &instance.WorkflowID, InstanceID: &instance.ID, Level: "info", Event: "instance_resumed", Message: "工作流实例已恢复"})
	return instance, nil
}

func (s *WorkflowService) CancelInstance(ctx context.Context, appID int64, instanceID int64) (*workflowdomain.Instance, error) {
	instance, err := s.InstanceDetail(ctx, appID, instanceID)
	if err != nil {
		return nil, err
	}
	endedAt := time.Now()
	instance.Status = "cancelled"
	instance.EndedAt = &endedAt
	if err := s.pg.UpdateWorkflowInstance(ctx, *instance); err != nil {
		return nil, err
	}
	if s.temporal != nil {
		if err := s.temporal.CancelWorkflow(ctx, temporalInstanceWorkflowID(instance.AppID, instance.WorkflowID, instance.ID), ""); err != nil {
			s.log.Warn("cancel workflow failed", zap.Int64("appid", appID), zap.Int64("instance_id", instanceID), zap.Error(err))
		}
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{AppID: appID, WorkflowID: &instance.WorkflowID, InstanceID: &instance.ID, Level: "warn", Event: "instance_cancelled", Message: "工作流实例已取消"})
	return instance, nil
}

func (s *WorkflowService) UserTasks(ctx context.Context, appID int64, query workflowdomain.TaskQuery) (*workflowdomain.TaskListResult, error) {
	items, total, err := s.pg.ListWorkflowTasks(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	return &workflowdomain.TaskListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: calcPagesForService(total, limit)}, nil
}

func (s *WorkflowService) TaskDetail(ctx context.Context, appID int64, taskID int64) (*workflowdomain.Task, error) {
	item, err := s.pg.GetWorkflowTask(ctx, appID, taskID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40482, http.StatusNotFound, "任务不存在")
	}
	return item, nil
}

func (s *WorkflowService) CompleteTask(ctx context.Context, appID int64, taskID int64, output map[string]any, comment string) (*workflowdomain.Task, error) {
	task, err := s.TaskDetail(ctx, appID, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status == "completed" {
		return task, nil
	}
	now := time.Now()
	task.Status = "completed"
	task.OutputData = output
	task.Comment = strings.TrimSpace(comment)
	task.CompletedAt = &now
	if err := s.pg.UpdateWorkflowTask(ctx, *task); err != nil {
		return nil, err
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{
		AppID: appID, WorkflowID: &task.WorkflowID, InstanceID: &task.InstanceID, TaskID: &task.ID,
		Level: "info", Event: "task_completed", Message: "人工任务已完成", Metadata: map[string]any{"comment": task.Comment},
	})
	instance, err := s.InstanceDetail(ctx, appID, task.InstanceID)
	if err == nil && s.temporal != nil {
		signal := temporalTaskCompleteSignal{TaskID: task.ID, Output: output, Comment: task.Comment}
		if err := s.temporal.SignalWorkflow(ctx, temporalInstanceWorkflowID(instance.AppID, instance.WorkflowID, instance.ID), "", temporalSignalTaskComplete, signal); err != nil {
			s.log.Warn("signal task complete failed", zap.Int64("appid", appID), zap.Int64("task_id", taskID), zap.Error(err))
		}
	}
	return task, nil
}

func (s *WorkflowService) AssignTask(ctx context.Context, appID int64, taskID int64, userID int64, comment string) (*workflowdomain.Task, error) {
	task, err := s.TaskDetail(ctx, appID, taskID)
	if err != nil {
		return nil, err
	}
	task.AssignedTo = &userID
	if strings.TrimSpace(comment) != "" {
		task.Comment = strings.TrimSpace(comment)
	}
	if err := s.pg.UpdateWorkflowTask(ctx, *task); err != nil {
		return nil, err
	}
	_ = s.pg.CreateWorkflowLog(ctx, workflowdomain.LogEntry{AppID: appID, WorkflowID: &task.WorkflowID, InstanceID: &task.InstanceID, TaskID: &task.ID, Level: "info", Event: "task_assigned", Message: "任务已分配", Metadata: map[string]any{"assigned_to": userID}})
	return task, nil
}

func (s *WorkflowService) TaskHistory(ctx context.Context, appID int64, taskID int64) ([]workflowdomain.LogEntry, error) {
	return s.pg.ListWorkflowLogs(ctx, appID, 0, 0, taskID, 200)
}

func (s *WorkflowService) Templates(ctx context.Context, appID int64, category string, page int, limit int) (map[string]any, error) {
	items, total, err := s.pg.ListWorkflowTemplates(ctx, appID, category, page, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "total": total, "page": page, "limit": limit, "totalPages": calcPagesForService(total, limit)}, nil
}

func (s *WorkflowService) CreateFromTemplate(ctx context.Context, appID int64, templateID int64, name string, description string, createdBy int64) (*workflowdomain.Workflow, error) {
	template, err := s.pg.GetWorkflowTemplate(ctx, appID, templateID)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, apperrors.New(40483, http.StatusNotFound, "工作流模板不存在")
	}
	status := "draft"
	return s.Save(ctx, workflowdomain.WorkflowMutation{
		AppID: appID, Name: &name, Description: &description, Category: &template.Category, Status: &status,
		Definition: &template.Definition, TriggerConfig: template.TriggerConfig, UIConfig: template.UIConfig, CreatedBy: createdBy, UpdatedBy: createdBy,
	})
}

func (s *WorkflowService) SaveAsTemplate(ctx context.Context, appID int64, workflowID int64, name string, description string, category string, isPublic bool) (*workflowdomain.Template, error) {
	workflowItem, err := s.Detail(ctx, appID, workflowID)
	if err != nil {
		return nil, err
	}
	return s.pg.UpsertWorkflowTemplate(ctx, workflowdomain.Template{
		AppID: appID, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Category: strings.TrimSpace(category), IsPublic: isPublic,
		Definition: workflowItem.Definition, TriggerConfig: workflowItem.TriggerConfig, UIConfig: workflowItem.UIConfig, Metadata: map[string]any{"workflowId": workflowID, "workflowName": workflowItem.Name},
	})
}

func (s *WorkflowService) Statistics(ctx context.Context, appID int64) (*workflowdomain.Statistics, error) {
	return s.pg.GetWorkflowStatistics(ctx, appID)
}

func (s *WorkflowService) Logs(ctx context.Context, appID int64, workflowID int64, instanceID int64, limit int) ([]workflowdomain.LogEntry, error) {
	return s.pg.ListWorkflowLogs(ctx, appID, workflowID, instanceID, 0, limit)
}

func (s *WorkflowService) EngineStatus() map[string]any {
	scheduled := 0
	if workflows, err := s.pg.ListSchedulableWorkflows(context.Background()); err == nil {
		for _, item := range workflows {
			if isWorkflowCronSchedulable(&item) {
				scheduled++
			}
		}
	}
	return map[string]any{"engine": "temporal", "connected": s.temporal != nil, "namespace": s.temporalCfg.Namespace, "taskQueue": s.temporalCfg.TaskQueue, "scheduledWorkflows": scheduled}
}

func (s *WorkflowService) ValidateDefinition(def workflowdomain.Definition) error {
	if len(def.Nodes) == 0 {
		return apperrors.New(40082, http.StatusBadRequest, "工作流节点不能为空")
	}
	nodeMap := make(map[string]workflowdomain.Node, len(def.Nodes))
	startCount := 0
	endCount := 0
	for _, node := range def.Nodes {
		if strings.TrimSpace(node.ID) == "" || strings.TrimSpace(node.Type) == "" {
			return apperrors.New(40083, http.StatusBadRequest, "工作流节点缺少必要字段")
		}
		if _, ok := nodeMap[node.ID]; ok {
			return apperrors.New(40084, http.StatusBadRequest, "工作流节点ID重复")
		}
		nodeMap[node.ID] = node
		if node.Type == "start" {
			startCount++
		}
		if node.Type == "end" {
			endCount++
		}
	}
	if startCount != 1 || endCount == 0 {
		return apperrors.New(40085, http.StatusBadRequest, "工作流必须包含1个开始节点和至少1个结束节点")
	}
	for _, edge := range def.Edges {
		if _, ok := nodeMap[edge.Source]; !ok {
			return apperrors.New(40086, http.StatusBadRequest, "工作流边来源节点不存在")
		}
		if _, ok := nodeMap[edge.Target]; !ok {
			return apperrors.New(40087, http.StatusBadRequest, "工作流边目标节点不存在")
		}
	}
	return nil
}

func (s *WorkflowService) NodeTypes() []map[string]any {
	return []map[string]any{
		{"type": "start", "label": "开始节点"},
		{"type": "condition", "label": "条件节点"},
		{"type": "task", "label": "人工任务"},
		{"type": "webhook", "label": "Webhook 调用"},
		{"type": "end", "label": "结束节点"},
	}
}

func (s *WorkflowService) RefreshSchedules(ctx context.Context) error {
	workflows, err := s.pg.ListSchedulableWorkflows(ctx)
	if err != nil {
		return err
	}
	for i := range workflows {
		if err := s.syncWorkflowSchedule(ctx, &workflows[i]); err != nil {
			s.log.Warn("sync workflow schedule failed", zap.Int64("workflow_id", workflows[i].ID), zap.Error(err))
		}
	}
	return nil
}

func (s *WorkflowService) executeManagedWorkflow(ctx context.Context, input temporalWorkflowInput) error {
	_, err := s.temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       temporalInstanceWorkflowID(input.AppID, input.WorkflowID, input.InstanceID),
		TaskQueue:                s.temporalCfg.TaskQueue,
		WorkflowExecutionTimeout: s.temporalCfg.WorkflowExecutionTimeout,
		WorkflowRunTimeout:       s.temporalCfg.WorkflowRunTimeout,
		WorkflowTaskTimeout:      s.temporalCfg.WorkflowTaskTimeout,
	}, temporalManagedWorkflowType, input)
	if err != nil {
		return apperrors.New(50081, http.StatusInternalServerError, "启动工作流失败")
	}
	return nil
}

func (s *WorkflowService) signalWorkflowControl(ctx context.Context, instance *workflowdomain.Instance, control temporalControlSignal) error {
	if s.temporal == nil || instance == nil {
		return nil
	}
	return s.temporal.SignalWorkflow(ctx, temporalInstanceWorkflowID(instance.AppID, instance.WorkflowID, instance.ID), "", temporalSignalControl, control)
}

func (s *WorkflowService) syncWorkflowSchedule(ctx context.Context, workflowItem *workflowdomain.Workflow) error {
	if s.temporal == nil || workflowItem == nil {
		return nil
	}
	if !isWorkflowCronSchedulable(workflowItem) {
		return s.deleteWorkflowSchedule(ctx, workflowItem.AppID, workflowItem.ID)
	}
	spec, ok := buildWorkflowScheduleSpec(workflowItem)
	if !ok {
		return nil
	}
	options := client.ScheduleOptions{
		ID:            temporalScheduleID(workflowItem.AppID, workflowItem.ID),
		Spec:          spec,
		Action:        &client.ScheduleWorkflowAction{ID: temporalScheduleID(workflowItem.AppID, workflowItem.ID) + ":launcher", Workflow: temporalScheduleLauncherWorkflowType, Args: []interface{}{newTemporalScheduledLaunchInput(workflowItem)}, TaskQueue: s.temporalCfg.TaskQueue},
		Overlap:       enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: time.Minute,
	}
	handle, createErr := s.temporal.ScheduleClient().Create(ctx, options)
	if createErr == nil {
		_ = handle
		return nil
	}
	handle = s.temporal.ScheduleClient().GetHandle(ctx, options.ID)
	return handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			return &client.ScheduleUpdate{Schedule: &client.Schedule{
				Spec:   &options.Spec,
				Action: options.Action,
				Policy: &client.SchedulePolicies{Overlap: options.Overlap, CatchupWindow: options.CatchupWindow},
				State:  &client.ScheduleState{Paused: false},
			}}, nil
		},
	})
}

func (s *WorkflowService) deleteWorkflowSchedule(ctx context.Context, appID int64, workflowID int64) error {
	if s.temporal == nil {
		return nil
	}
	if err := s.temporal.ScheduleClient().GetHandle(ctx, temporalScheduleID(appID, workflowID)).Delete(ctx); err != nil {
		return nil
	}
	return nil
}

func (s *WorkflowService) requireApp(ctx context.Context, appID int64) (appNameHolder, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appNameHolder{}, err
	}
	if app == nil {
		return appNameHolder{}, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return appNameHolder{Name: app.Name}, nil
}

func isWorkflowCronSchedulable(item *workflowdomain.Workflow) bool {
	if item == nil || item.Status != "active" {
		return false
	}
	return strings.EqualFold(stringFromAny(item.TriggerConfig["type"]), "cron") && strings.TrimSpace(stringFromAny(item.TriggerConfig["expression"])) != ""
}

func buildWorkflowScheduleSpec(item *workflowdomain.Workflow) (client.ScheduleSpec, bool) {
	if !isWorkflowCronSchedulable(item) {
		return client.ScheduleSpec{}, false
	}
	spec := client.ScheduleSpec{CronExpressions: []string{strings.TrimSpace(stringFromAny(item.TriggerConfig["expression"]))}}
	if timezone := strings.TrimSpace(stringFromAny(item.TriggerConfig["timezone"])); timezone != "" {
		spec.TimeZoneName = timezone
	}
	return spec, true
}

func newTemporalScheduledLaunchInput(item *workflowdomain.Workflow) temporalScheduledLaunchInput {
	return temporalScheduledLaunchInput{
		AppID: item.AppID, WorkflowID: item.ID, WorkflowName: item.Name,
		Priority: intFromAny(item.TriggerConfig["priority"]), InputData: mapFromAny(item.TriggerConfig["input"]), Definition: item.Definition,
	}
}

func temporalInstanceWorkflowID(appID int64, workflowID int64, instanceID int64) string {
	return fmt.Sprintf("aegis:%d:%d:%d", appID, workflowID, instanceID)
}

func temporalScheduleID(appID int64, workflowID int64) string {
	return fmt.Sprintf("aegis-schedule:%d:%d", appID, workflowID)
}

func mapFromAny(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if ok && typed != nil {
		return typed
	}
	return map[string]any{}
}
