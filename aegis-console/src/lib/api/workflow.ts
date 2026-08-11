import { apiRequest } from "./client";
import type {
  WorkflowDefinition,
  WorkflowDetail,
  WorkflowInstance,
  WorkflowInstancesResult,
  WorkflowListResult,
  WorkflowLogEntry,
  WorkflowNodeType,
  WorkflowStatistics,
  WorkflowTask,
  WorkflowTaskListResult,
  WorkflowTemplate,
  WorkflowTemplatesResult
} from "./types";

export function getWorkflowStatistics(token: string, appid: number) {
  return apiRequest<WorkflowStatistics>("/api/app/workflow/statistics", {
    method: "POST",
    token,
    body: JSON.stringify({ appid })
  });
}

export function getWorkflowList(
  token: string,
  payload: {
    appid: number;
    status?: string;
    category?: string;
    keyword?: string;
    page?: number;
    limit?: number;
  }
) {
  return apiRequest<WorkflowListResult>("/api/app/workflow/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowDetail(
  token: string,
  payload: {
    appid: number;
    workflowId: number;
  }
) {
  return apiRequest<WorkflowDetail>("/api/app/workflow/detail", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid: payload.appid,
      workflowId: payload.workflowId,
      workflow_id: payload.workflowId
    })
  });
}

export function getWorkflowInstances(
  token: string,
  payload: {
    appid: number;
    workflowId?: number;
    status?: string;
    page?: number;
    limit?: number;
  }
) {
  return apiRequest<WorkflowInstancesResult>("/api/app/workflow/instances", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid: payload.appid,
      workflowId: payload.workflowId,
      workflow_id: payload.workflowId,
      status: payload.status,
      page: payload.page,
      limit: payload.limit
    })
  });
}

export function getWorkflowLogs(
  token: string,
  payload: {
    appid: number;
    workflowId?: number;
    instanceId?: number;
    limit?: number;
  }
) {
  return apiRequest<WorkflowLogEntry[]>("/api/app/workflow/logs", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid: payload.appid,
      workflowId: payload.workflowId,
      workflow_id: payload.workflowId,
      instanceId: payload.instanceId,
      instance_id: payload.instanceId,
      limit: payload.limit
    })
  });
}

export function getWorkflowTemplates(
  token: string,
  payload: {
    appid: number;
    category?: string;
    page?: number;
    limit?: number;
  }
) {
  return apiRequest<WorkflowTemplatesResult>("/api/app/workflow/templates", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid: payload.appid,
      category: payload.category,
      page: payload.page,
      limit: payload.limit
    })
  });
}

export function getWorkflowEngineStatus(token: string) {
  return apiRequest<Record<string, unknown>>("/api/app/workflow/engine/status", {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function createWorkflow(
  token: string,
  payload: {
    appid: number;
    name: string;
    description?: string;
    category?: string;
    status?: string;
    definition: WorkflowDefinition | Record<string, unknown>;
    trigger_config?: Record<string, unknown>;
    ui_config?: Record<string, unknown>;
    permissions?: Record<string, unknown>;
  }
) {
  return apiRequest<WorkflowDetail>("/api/app/workflow/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateWorkflow(
  token: string,
  payload: {
    appid: number;
    workflow_id: number;
    name: string;
    description?: string;
    category?: string;
    status?: string;
    definition: WorkflowDefinition | Record<string, unknown>;
    trigger_config?: Record<string, unknown>;
    ui_config?: Record<string, unknown>;
    permissions?: Record<string, unknown>;
  }
) {
  return apiRequest<WorkflowDetail>("/api/app/workflow/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteWorkflow(token: string, payload: { appid: number; workflow_id: number }) {
  return apiRequest<{ success?: boolean }>("/api/app/workflow/delete", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function startWorkflow(
  token: string,
  payload: {
    appid: number;
    workflow_id: number;
    input_data?: Record<string, unknown>;
    instance_name?: string;
    priority?: number;
  }
) {
  return apiRequest<WorkflowInstance>("/api/app/workflow/start", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowInstanceDetail(token: string, payload: { appid: number; instance_id: number }) {
  return apiRequest<WorkflowInstance>("/api/app/workflow/instance/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function pauseWorkflowInstance(token: string, payload: { appid: number; instance_id: number }) {
  return apiRequest<WorkflowInstance>("/api/app/workflow/instance/pause", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function resumeWorkflowInstance(token: string, payload: { appid: number; instance_id: number }) {
  return apiRequest<WorkflowInstance>("/api/app/workflow/instance/resume", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function cancelWorkflowInstance(token: string, payload: { appid: number; instance_id: number }) {
  return apiRequest<WorkflowInstance>("/api/app/workflow/instance/cancel", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowTasksTodo(
  token: string,
  payload: { appid: number; user_id?: number; status?: string; priority?: number; page?: number; limit?: number }
) {
  return apiRequest<WorkflowTaskListResult>("/api/app/workflow/tasks/todo", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowTaskDetail(token: string, payload: { appid: number; task_id: number }) {
  return apiRequest<WorkflowTask>("/api/app/workflow/task/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function completeWorkflowTask(
  token: string,
  payload: { appid: number; task_id: number; output_data?: Record<string, unknown>; comment?: string }
) {
  return apiRequest<WorkflowTask>("/api/app/workflow/task/complete", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function assignWorkflowTask(
  token: string,
  payload: { appid: number; task_id: number; assigned_to: number; comment?: string }
) {
  return apiRequest<WorkflowTask>("/api/app/workflow/task/assign", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowTaskHistory(token: string, payload: { appid: number; task_id: number }) {
  return apiRequest<WorkflowLogEntry[]>("/api/app/workflow/task/history", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function createWorkflowFromTemplate(
  token: string,
  payload: { appid: number; template_id: number; name: string; description?: string }
) {
  return apiRequest<WorkflowDetail>("/api/app/workflow/create-from-template", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function saveWorkflowAsTemplate(
  token: string,
  payload: {
    appid: number;
    workflow_id: number;
    template_name: string;
    template_description?: string;
    category?: string;
    is_public?: boolean;
  }
) {
  return apiRequest<WorkflowTemplate>("/api/app/workflow/save-as-template", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function validateWorkflowDefinition(
  token: string,
  payload: { appid: number; definition: WorkflowDefinition | Record<string, unknown> }
) {
  return apiRequest<{ valid: boolean }>("/api/app/workflow/validate", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getWorkflowNodeTypes(token: string) {
  return apiRequest<WorkflowNodeType[]>("/api/app/workflow/node-types", {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}
