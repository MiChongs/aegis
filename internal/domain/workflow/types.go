package workflow

import "time"

type Node struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

type Edge struct {
	ID        string `json:"id,omitempty"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Condition string `json:"condition,omitempty"`
}

type Definition struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Workflow struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Category      string         `json:"category,omitempty"`
	Status        string         `json:"status"`
	Version       int            `json:"version"`
	Definition    Definition     `json:"definition"`
	TriggerConfig map[string]any `json:"trigger_config,omitempty"`
	UIConfig      map[string]any `json:"ui_config,omitempty"`
	Permissions   map[string]any `json:"permissions,omitempty"`
	CreatedBy     int64          `json:"created_by,omitempty"`
	UpdatedBy     int64          `json:"updated_by,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type WorkflowMutation struct {
	ID            int64
	AppID         int64
	Name          *string
	Description   *string
	Category      *string
	Status        *string
	Definition    *Definition
	TriggerConfig map[string]any
	UIConfig      map[string]any
	Permissions   map[string]any
	CreatedBy     int64
	UpdatedBy     int64
}

type ListQuery struct {
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
	Status   string `json:"status"`
	Category string `json:"category"`
	Keyword  string `json:"keyword"`
}

type ListResult struct {
	Items      []Workflow `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

type Instance struct {
	ID            int64          `json:"id"`
	WorkflowID    int64          `json:"workflow_id"`
	AppID         int64          `json:"appid"`
	InstanceName  string         `json:"instance_name,omitempty"`
	Status        string         `json:"status"`
	Priority      int            `json:"priority"`
	StartedBy     *int64         `json:"started_by,omitempty"`
	CurrentNodeID string         `json:"current_node_id,omitempty"`
	InputData     map[string]any `json:"input_data,omitempty"`
	OutputData    map[string]any `json:"output_data,omitempty"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	EndedAt       *time.Time     `json:"ended_at,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type InstanceQuery struct {
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	WorkflowID int64  `json:"workflow_id"`
	Status     string `json:"status"`
}

type InstanceListResult struct {
	Items      []Instance `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

type Task struct {
	ID          int64          `json:"id"`
	WorkflowID  int64          `json:"workflow_id"`
	InstanceID  int64          `json:"instance_id"`
	AppID       int64          `json:"appid"`
	NodeID      string         `json:"node_id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Priority    int            `json:"priority"`
	AssignedTo  *int64         `json:"assigned_to,omitempty"`
	InputData   map[string]any `json:"input_data,omitempty"`
	OutputData  map[string]any `json:"output_data,omitempty"`
	FormSchema  map[string]any `json:"form_schema,omitempty"`
	Comment     string         `json:"comment,omitempty"`
	DueAt       *time.Time     `json:"due_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type TaskQuery struct {
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
	UserID   int64  `json:"user_id"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

type TaskListResult struct {
	Items      []Task `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
}

type LogEntry struct {
	ID         int64          `json:"id"`
	AppID      int64          `json:"appid"`
	WorkflowID *int64         `json:"workflow_id,omitempty"`
	InstanceID *int64         `json:"instance_id,omitempty"`
	TaskID     *int64         `json:"task_id,omitempty"`
	Level      string         `json:"level"`
	Event      string         `json:"event"`
	Message    string         `json:"message"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type Template struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Category      string         `json:"category,omitempty"`
	IsPublic      bool           `json:"is_public"`
	Definition    Definition     `json:"definition"`
	TriggerConfig map[string]any `json:"trigger_config,omitempty"`
	UIConfig      map[string]any `json:"ui_config,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type TemplateMutation struct {
	ID            int64
	AppID         int64
	Name          *string
	Description   *string
	Category      *string
	IsPublic      *bool
	Definition    *Definition
	TriggerConfig map[string]any
	UIConfig      map[string]any
	Metadata      map[string]any
}

type Statistics struct {
	AppID              int64            `json:"appid"`
	TotalWorkflows     int64            `json:"totalWorkflows"`
	ActiveWorkflows    int64            `json:"activeWorkflows"`
	TotalInstances     int64            `json:"totalInstances"`
	RunningInstances   int64            `json:"runningInstances"`
	CompletedInstances int64            `json:"completedInstances"`
	FailedInstances    int64            `json:"failedInstances"`
	PendingTasks       int64            `json:"pendingTasks"`
	StatusCounts       map[string]int64 `json:"statusCounts"`
}
