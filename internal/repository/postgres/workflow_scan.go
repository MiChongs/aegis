package postgres

import (
	"encoding/json"

	workflowdomain "aegis/internal/domain/workflow"
)

func scanWorkflow(row interface{ Scan(dest ...any) error }) (*workflowdomain.Workflow, error) {
	var item workflowdomain.Workflow
	var definition []byte
	var trigger []byte
	var ui []byte
	var permissions []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Category, &item.Status, &item.Version, &definition, &trigger, &ui, &permissions, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(definition, &item.Definition)
	_ = json.Unmarshal(trigger, &item.TriggerConfig)
	_ = json.Unmarshal(ui, &item.UIConfig)
	_ = json.Unmarshal(permissions, &item.Permissions)
	return &item, nil
}

func scanWorkflowInstance(row interface{ Scan(dest ...any) error }) (*workflowdomain.Instance, error) {
	var item workflowdomain.Instance
	var input []byte
	var output []byte
	if err := row.Scan(&item.ID, &item.WorkflowID, &item.AppID, &item.InstanceName, &item.Status, &item.Priority, &item.StartedBy, &item.CurrentNodeID, &input, &output, &item.ErrorMessage, &item.StartedAt, &item.EndedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(input, &item.InputData)
	_ = json.Unmarshal(output, &item.OutputData)
	return &item, nil
}

func scanWorkflowTask(row interface{ Scan(dest ...any) error }) (*workflowdomain.Task, error) {
	var item workflowdomain.Task
	var input []byte
	var output []byte
	var formSchema []byte
	if err := row.Scan(&item.ID, &item.WorkflowID, &item.InstanceID, &item.AppID, &item.NodeID, &item.Name, &item.Type, &item.Status, &item.Priority, &item.AssignedTo, &input, &output, &formSchema, &item.Comment, &item.DueAt, &item.CompletedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(input, &item.InputData)
	_ = json.Unmarshal(output, &item.OutputData)
	_ = json.Unmarshal(formSchema, &item.FormSchema)
	return &item, nil
}

func scanWorkflowLog(row interface{ Scan(dest ...any) error }) (*workflowdomain.LogEntry, error) {
	var item workflowdomain.LogEntry
	var metadata []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.WorkflowID, &item.InstanceID, &item.TaskID, &item.Level, &item.Event, &item.Message, &metadata, &item.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func scanWorkflowTemplate(row interface{ Scan(dest ...any) error }) (*workflowdomain.Template, error) {
	var item workflowdomain.Template
	var definition []byte
	var trigger []byte
	var ui []byte
	var metadata []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Category, &item.IsPublic, &definition, &trigger, &ui, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(definition, &item.Definition)
	_ = json.Unmarshal(trigger, &item.TriggerConfig)
	_ = json.Unmarshal(ui, &item.UIConfig)
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}
