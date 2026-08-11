"use client";

import {
  startTransition,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import {
  ArrowRight,
  CirclePause,
  CirclePlay,
  CopyPlus,
  GitBranch,
  Maximize2,
  Minimize2,
  Play,
  Plus,
  Save,
  ShieldCheck,
  SquarePen,
  Trash2,
  UserCog
} from "lucide-react";
import { toast } from "sonner";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type {
  WorkflowDetail,
  WorkflowEdge,
  WorkflowLogEntry,
  WorkflowNode,
  WorkflowTemplate
} from "@/lib/api-client";
import {
  useAdminAppsQuery,
  useAssignWorkflowTaskMutation,
  useCancelWorkflowInstanceMutation,
  useCompleteWorkflowTaskMutation,
  useCreateWorkflowFromTemplateMutation,
  useCreateWorkflowMutation,
  useDeleteWorkflowMutation,
  usePauseWorkflowInstanceMutation,
  useResumeWorkflowInstanceMutation,
  useSaveWorkflowAsTemplateMutation,
  useStartWorkflowMutation,
  useUpdateWorkflowMutation,
  useValidateWorkflowDefinitionMutation,
  useWorkflowDetailQuery,
  useWorkflowEngineStatusQuery,
  useWorkflowInstanceDetailQuery,
  useWorkflowInstancesQuery,
  useWorkflowListQuery,
  useWorkflowLogsQuery,
  useWorkflowNodeTypesQuery,
  useWorkflowStatisticsQuery,
  useWorkflowTaskDetailQuery,
  useWorkflowTaskHistoryQuery,
  useWorkflowTasksTodoQuery,
  useWorkflowTemplatesQuery
} from "@/lib/admin-hooks";
import {
  buildDraft,
  buildNodeId,
  createEmptyDraft,
  DEFAULT_NODE_TYPES,
  formatDate,
  getErrorMessage,
  INSTANCE_STATUS_FILTERS,
  nodeTone,
  parseJsonText,
  readBoolean,
  readString,
  statusBadgeVariant,
  stringifyJson,
  TASK_STATUS_FILTERS,
  toText,
  WORKFLOW_STATUS_OPTIONS,
  type WorkflowDraft
} from "./workflow-helpers";
import { WorkflowCanvas } from "./workflow-canvas";
import {
  WorkflowFromTemplateDialog,
  WorkflowSaveTemplateDialog,
  WorkflowStartDialog
} from "./workflow-dialogs";

function MetricCard({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <SurfaceCard className="border border-border/80">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
          <div className="mt-3 text-3xl font-semibold text-foreground">{value}</div>
        </div>
        <div className={`rounded-2xl border px-3 py-1 text-xs font-semibold ${tone}`}>{label}</div>
      </div>
    </SurfaceCard>
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function WorkflowStudio() {
  const designerViewportRef = useRef<HTMLDivElement | null>(null);
  const appsQuery = useAdminAppsQuery();
  const nodeTypesQuery = useWorkflowNodeTypesQuery();
  const engineStatusQuery = useWorkflowEngineStatusQuery();

  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<number | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(null);
  const [selectedTaskId, setSelectedTaskId] = useState<number | null>(null);
  const [creatingNew, setCreatingNew] = useState(false);
  const [isDesignerFullscreen, setIsDesignerFullscreen] = useState(false);
  const [isNativeDesignerFullscreen, setIsNativeDesignerFullscreen] = useState(false);
  const [showDesignerLeftPanel, setShowDesignerLeftPanel] = useState(true);
  const [showDesignerRightPanel, setShowDesignerRightPanel] = useState(true);
  const [workflowKeyword, setWorkflowKeyword] = useState("");
  const deferredKeyword = useDeferredValue(workflowKeyword);
  const [workflowStatus, setWorkflowStatus] = useState("all");
  const [instanceStatus, setInstanceStatus] = useState("all");
  const [taskStatus, setTaskStatus] = useState("all");
  const [draft, setDraft] = useState<WorkflowDraft>(createEmptyDraft());
  const [nodeConfigText, setNodeConfigText] = useState("{}");
  const [edgeDraft, setEdgeDraft] = useState({ source: "", target: "", condition: "" });
  const [startDialogOpen, setStartDialogOpen] = useState(false);
  const [startForm, setStartForm] = useState({ instanceName: "", priority: "5", inputText: "{}" });
  const [templateDialogOpen, setTemplateDialogOpen] = useState(false);
  const [templateForm, setTemplateForm] = useState({
    templateName: "",
    templateDescription: "",
    category: "",
    isPublic: "private"
  });
  const [fromTemplateDialogOpen, setFromTemplateDialogOpen] = useState(false);
  const [fromTemplateForm, setFromTemplateForm] = useState({ templateId: "", name: "", description: "" });
  const [completeForm, setCompleteForm] = useState({ outputText: "{}", comment: "" });
  const [assignForm, setAssignForm] = useState({ assignedTo: "", comment: "" });

  const selectedApp = useMemo(() => {
    const items = appsQuery.data || [];
    if (!items.length) {
      return null;
    }
    return items.find((item) => item.id === selectedAppId) || items[0];
  }, [appsQuery.data, selectedAppId]);

  const statisticsQuery = useWorkflowStatisticsQuery(selectedApp?.id);
  const workflowListQuery = useWorkflowListQuery(selectedApp?.id, {
    keyword: deferredKeyword || undefined,
    status: workflowStatus === "all" ? undefined : workflowStatus,
    page: 1,
    limit: 20
  });
  const workflowDetailQuery = useWorkflowDetailQuery(selectedApp?.id || null, selectedWorkflowId);
  const instancesQuery = useWorkflowInstancesQuery(selectedApp?.id, {
    workflowId: selectedWorkflowId || undefined,
    status: instanceStatus === "all" ? undefined : instanceStatus,
    page: 1,
    limit: 12
  });
  const instanceDetailQuery = useWorkflowInstanceDetailQuery(selectedApp?.id || null, selectedInstanceId);
  const tasksQuery = useWorkflowTasksTodoQuery(selectedApp?.id, {
    status: taskStatus === "all" ? undefined : taskStatus,
    page: 1,
    limit: 12
  });
  const taskDetailQuery = useWorkflowTaskDetailQuery(selectedApp?.id || null, selectedTaskId);
  const taskHistoryQuery = useWorkflowTaskHistoryQuery(selectedApp?.id || null, selectedTaskId);
  const templatesQuery = useWorkflowTemplatesQuery(selectedApp?.id, { page: 1, limit: 8 });
  const logsQuery = useWorkflowLogsQuery(selectedApp?.id, {
    workflowId: selectedWorkflowId || undefined,
    instanceId: selectedInstanceId || undefined,
    limit: 16
  });

  const createWorkflowMutation = useCreateWorkflowMutation();
  const updateWorkflowMutation = useUpdateWorkflowMutation();
  const deleteWorkflowMutation = useDeleteWorkflowMutation();
  const startWorkflowMutation = useStartWorkflowMutation();
  const validateWorkflowMutation = useValidateWorkflowDefinitionMutation();
  const createFromTemplateMutation = useCreateWorkflowFromTemplateMutation();
  const saveAsTemplateMutation = useSaveWorkflowAsTemplateMutation();
  const pauseInstanceMutation = usePauseWorkflowInstanceMutation();
  const resumeInstanceMutation = useResumeWorkflowInstanceMutation();
  const cancelInstanceMutation = useCancelWorkflowInstanceMutation();
  const completeTaskMutation = useCompleteWorkflowTaskMutation();
  const assignTaskMutation = useAssignWorkflowTaskMutation();

  const workflows = workflowListQuery.data?.items || [];
  const instances = instancesQuery.data?.items || [];
  const tasks = tasksQuery.data?.items || [];
  const templates = templatesQuery.data?.items || [];
  const logs = logsQuery.data || [];
  const nodeTypes = nodeTypesQuery.data?.length ? nodeTypesQuery.data : DEFAULT_NODE_TYPES;

  useEffect(() => {
    const apps = appsQuery.data || [];
    if (apps.length && (!selectedAppId || !apps.some((item) => item.id === selectedAppId))) {
      setSelectedAppId(apps[0].id);
    }
  }, [appsQuery.data, selectedAppId]);

  useEffect(() => {
    if (!creatingNew && workflows.length && (!selectedWorkflowId || !workflows.some((item) => item.id === selectedWorkflowId))) {
      setSelectedWorkflowId(workflows[0].id);
    }
    if (!workflows.length && !creatingNew) {
      setSelectedWorkflowId(null);
    }
  }, [creatingNew, selectedWorkflowId, workflows]);

  useEffect(() => {
    if (creatingNew) {
      setDraft(createEmptyDraft());
    } else if (workflowDetailQuery.data) {
      setDraft(buildDraft(workflowDetailQuery.data as WorkflowDetail));
    }
  }, [creatingNew, workflowDetailQuery.data]);

  useEffect(() => {
    const nodeIds = draft.definition.nodes.map((node) => node.id);
    if (!nodeIds.length) {
      setSelectedNodeId(null);
      return;
    }
    if (!selectedNodeId || !nodeIds.includes(selectedNodeId)) {
      setSelectedNodeId(nodeIds[0]);
    }
  }, [draft.definition.nodes, selectedNodeId]);

  useEffect(() => {
    const node = draft.definition.nodes.find((item) => item.id === selectedNodeId);
    setNodeConfigText(stringifyJson(node?.config));
  }, [draft.definition.nodes, selectedNodeId]);

  useEffect(() => {
    if (instances.length && (!selectedInstanceId || !instances.some((item) => item.id === selectedInstanceId))) {
      setSelectedInstanceId(instances[0].id);
    }
    if (!instances.length) {
      setSelectedInstanceId(null);
    }
  }, [instances, selectedInstanceId]);

  useEffect(() => {
    if (tasks.length && (!selectedTaskId || !tasks.some((item) => item.id === selectedTaskId))) {
      setSelectedTaskId(tasks[0].id);
    }
    if (!tasks.length) {
      setSelectedTaskId(null);
    }
  }, [selectedTaskId, tasks]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      const host = designerViewportRef.current;
      const activeElement = document.fullscreenElement;
      const active = !!host && activeElement === host;
      setIsNativeDesignerFullscreen(active);
      if (!active) {
        setIsDesignerFullscreen(false);
      }
    };

    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  useEffect(() => {
    if (!isDesignerFullscreen) {
      return;
    }

    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (!document.fullscreenElement) {
          setIsDesignerFullscreen(false);
        }
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        void saveWorkflow();
      }
    };

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", handleKeydown);

    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", handleKeydown);
    };
  }, [isDesignerFullscreen]);

  useEffect(() => {
    if (!isDesignerFullscreen) {
      setShowDesignerLeftPanel(true);
      setShowDesignerRightPanel(true);
    }
  }, [isDesignerFullscreen]);

  if (appsQuery.isLoading || engineStatusQuery.isLoading) {
    return <LoadingState title="正在加载工作流" description="正在读取控制台工作流资源。" />;
  }

  if (!selectedApp) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Workflow" title="工作流" description="当前没有可用应用。" />
        <EmptyState title="暂无应用" description="请先创建应用，再继续配置工作流。" />
      </div>
    );
  }

  const activeAppId = selectedApp.id;
  const stats = statisticsQuery.data;
  const engineStatus = engineStatusQuery.data || {};
  const selectedNode = draft.definition.nodes.find((node) => node.id === selectedNodeId) || null;
  const selectedInstance = instanceDetailQuery.data || instances.find((item) => item.id === selectedInstanceId) || null;
  const selectedTask = taskDetailQuery.data || tasks.find((item) => item.id === selectedTaskId) || null;
  const showLeftPanel = !isDesignerFullscreen || showDesignerLeftPanel;
  const showRightPanel = !isDesignerFullscreen || showDesignerRightPanel;
  const designerViewportClass = isDesignerFullscreen
    ? "workflow-designer-stage workflow-designer-stage--fullscreen"
    : "workflow-designer-stage";
  const designerLayoutClass = isDesignerFullscreen
    ? [
        "grid h-full min-h-0 gap-3 overflow-hidden rounded-[30px] border border-border/80 bg-background/95 p-3 shadow-[0_36px_120px_-40px_rgba(15,23,42,0.26)] backdrop-blur-xl dark:bg-background/88 dark:shadow-[0_36px_120px_-40px_rgba(2,6,23,0.84)]",
        showLeftPanel && showRightPanel
          ? "grid-cols-1 grid-rows-[auto_auto_auto] lg:grid-cols-[220px_minmax(0,2fr)_300px] lg:grid-rows-[auto_minmax(0,1fr)]"
          : showLeftPanel
            ? "grid-cols-1 grid-rows-[auto_auto] lg:grid-cols-[220px_minmax(0,1fr)] lg:grid-rows-[auto_minmax(0,1fr)]"
            : showRightPanel
              ? "grid-cols-1 grid-rows-[auto_auto] lg:grid-cols-[minmax(0,1fr)_300px] lg:grid-rows-[auto_minmax(0,1fr)]"
              : "grid-cols-1 grid-rows-[auto_minmax(0,1fr)]"
      ].join(" ")
    : "grid gap-6 2xl:grid-cols-[280px_minmax(0,1.45fr)_340px]";

  const selectWorkflow = (id: number) => {
    startTransition(() => {
      setCreatingNew(false);
      setSelectedWorkflowId(id);
      setSelectedInstanceId(null);
      setSelectedTaskId(null);
    });
  };

  const changeApp = (value: string) => {
    startTransition(() => {
      setSelectedAppId(Number(value));
      setSelectedWorkflowId(null);
      setSelectedInstanceId(null);
      setSelectedTaskId(null);
      setSelectedNodeId(null);
      setCreatingNew(false);
      setWorkflowKeyword("");
      setWorkflowStatus("all");
      setInstanceStatus("all");
      setTaskStatus("all");
    });
  };

  const createNewWorkflow = () => {
    setCreatingNew(true);
    setSelectedWorkflowId(null);
    setSelectedInstanceId(null);
    setSelectedTaskId(null);
    setDraft(createEmptyDraft());
  };

  const toggleDesignerFullscreen = async () => {
    const host = designerViewportRef.current;
    if (!host) {
      setIsDesignerFullscreen((current) => !current);
      return;
    }

    if (document.fullscreenElement === host) {
      await document.exitFullscreen();
      return;
    }

    if (document.fullscreenElement) {
      await document.exitFullscreen();
    }

    if (typeof host.requestFullscreen === "function") {
      try {
        await host.requestFullscreen();
        setIsDesignerFullscreen(true);
        return;
      } catch {
        setIsNativeDesignerFullscreen(false);
      }
    }

    setIsDesignerFullscreen((current) => !current);
  };

  const exitDesignerFullscreen = async () => {
    if (document.fullscreenElement === designerViewportRef.current) {
      await document.exitFullscreen();
      return;
    }

    setIsDesignerFullscreen(false);
  };

  const updateUiConfigText = (updater: (current: Record<string, unknown>) => Record<string, unknown>) => {
    setDraft((current) => {
      let base: Record<string, unknown> = {};

      try {
        const parsed = JSON.parse(current.uiConfigText);
        if (isRecord(parsed)) {
          base = parsed;
        }
      } catch {
        base = {};
      }

      return {
        ...current,
        uiConfigText: stringifyJson(updater(base))
      };
    });
  };

  const addNode = (type: string, label: string, position?: { x: number; y: number }) => {
    const nodeId = buildNodeId(type, draft.definition.nodes);
    const nextNode: WorkflowNode = { id: nodeId, type, name: label, config: {} };
    setDraft((current) => ({
      ...current,
      definition: { ...current.definition, nodes: [...current.definition.nodes, nextNode] }
    }));
    if (position) {
      updateUiConfigText((current) => {
        const designer = isRecord(current.designer) ? current.designer : {};
        const positions = isRecord(designer.positions) ? designer.positions : {};
        return {
          ...current,
          designer: {
            ...designer,
            positions: {
              ...positions,
              [nodeId]: position
            }
          }
        };
      });
    }
    setSelectedNodeId(nodeId);
  };

  const updateSelectedNode = (patch: Partial<WorkflowNode>) => {
    if (!selectedNodeId) {
      return;
    }
    setDraft((current) => ({
      ...current,
      definition: {
        ...current.definition,
        nodes: current.definition.nodes.map((node) => (node.id === selectedNodeId ? { ...node, ...patch } : node))
      }
    }));
  };

  const removeNode = (nodeId: string) => {
    setDraft((current) => ({
      ...current,
      definition: {
        nodes: current.definition.nodes.filter((node) => node.id !== nodeId),
        edges: current.definition.edges.filter((edge) => edge.source !== nodeId && edge.target !== nodeId)
      }
    }));
  };

  const applyNodeConfig = () => {
    try {
      updateSelectedNode({ config: parseJsonText(nodeConfigText, "节点配置") });
      toast.success("节点配置已更新");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  };

  const addEdge = () => {
    if (!edgeDraft.source || !edgeDraft.target) {
      toast.error("请选择连线的起点与终点");
      return;
    }
    const nextEdge: WorkflowEdge = {
      id: `edge_${Date.now()}`,
      source: edgeDraft.source,
      target: edgeDraft.target,
      condition: edgeDraft.condition.trim()
    };
    setDraft((current) => ({
      ...current,
      definition: { ...current.definition, edges: [...current.definition.edges, nextEdge] }
    }));
    setEdgeDraft({ source: "", target: "", condition: "" });
  };

  const startCount = draft.definition.nodes.filter((node) => node.type === "start").length;
  const endCount = draft.definition.nodes.filter((node) => node.type === "end").length;

  async function saveWorkflow() {
    const name = draft.name.trim();
    if (!name) {
      toast.error("请填写工作流名称");
      return;
    }

    try {
      const payload = {
        appid: activeAppId,
        name,
        description: draft.description.trim(),
        category: draft.category.trim(),
        status: draft.status,
        definition: draft.definition,
        trigger_config: parseJsonText(draft.triggerConfigText, "触发配置"),
        ui_config: parseJsonText(draft.uiConfigText, "界面配置"),
        permissions: parseJsonText(draft.permissionsText, "权限配置")
      };

      if (creatingNew || !selectedWorkflowId) {
        const created = await createWorkflowMutation.mutateAsync(payload);
        setCreatingNew(false);
        setSelectedWorkflowId(created.id);
        toast.success("工作流已创建");
      } else {
        await updateWorkflowMutation.mutateAsync({ ...payload, workflow_id: selectedWorkflowId });
        toast.success("工作流已更新");
      }
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function validateWorkflow() {
    try {
      await validateWorkflowMutation.mutateAsync({ appid: activeAppId, definition: draft.definition });
      toast.success("工作流定义校验通过");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function deleteWorkflow() {
    if (!selectedWorkflowId || creatingNew) {
      toast.error("当前没有可删除的工作流");
      return;
    }
    if (!window.confirm("确认删除当前工作流？此操作不可恢复。")) {
      return;
    }
    try {
      await deleteWorkflowMutation.mutateAsync({ appid: activeAppId, workflow_id: selectedWorkflowId });
      setSelectedWorkflowId(null);
      toast.success("工作流已删除");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function startWorkflow() {
    if (!selectedWorkflowId || creatingNew) {
      toast.error("请先保存并选择工作流");
      return;
    }
    try {
      const instance = await startWorkflowMutation.mutateAsync({
        appid: activeAppId,
        workflow_id: selectedWorkflowId,
        instance_name: startForm.instanceName.trim(),
        priority: Number(startForm.priority) || 5,
        input_data: parseJsonText(startForm.inputText, "启动输入")
      });
      setSelectedInstanceId(instance.id);
      setStartDialogOpen(false);
      toast.success("工作流实例已启动");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function saveAsTemplate() {
    if (!selectedWorkflowId || creatingNew) {
      toast.error("请先选择已保存的工作流");
      return;
    }
    try {
      await saveAsTemplateMutation.mutateAsync({
        appid: activeAppId,
        workflow_id: selectedWorkflowId,
        template_name: templateForm.templateName.trim(),
        template_description: templateForm.templateDescription.trim(),
        category: templateForm.category.trim(),
        is_public: templateForm.isPublic === "public"
      });
      setTemplateDialogOpen(false);
      toast.success("已保存为模板");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function createFromTemplate() {
    try {
      const created = await createFromTemplateMutation.mutateAsync({
        appid: activeAppId,
        template_id: Number(fromTemplateForm.templateId),
        name: fromTemplateForm.name.trim(),
        description: fromTemplateForm.description.trim()
      });
      setCreatingNew(false);
      setSelectedWorkflowId(created.id);
      setFromTemplateDialogOpen(false);
      toast.success("模板工作流已创建");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function completeTask() {
    if (!selectedTaskId) {
      return;
    }
    try {
      await completeTaskMutation.mutateAsync({
        appid: activeAppId,
        task_id: selectedTaskId,
        output_data: parseJsonText(completeForm.outputText, "任务输出"),
        comment: completeForm.comment.trim()
      });
      toast.success("任务已完成");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  async function assignTask() {
    if (!selectedTaskId) {
      return;
    }
    try {
      await assignTaskMutation.mutateAsync({
        appid: activeAppId,
        task_id: selectedTaskId,
        assigned_to: Number(assignForm.assignedTo),
        comment: assignForm.comment.trim()
      });
      toast.success("任务已分配");
    } catch (error) {
      toast.error(getErrorMessage(error));
    }
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Workflow"
        title="工作流"
        description="定义、模板、实例与任务在同一控制台中维护。"
        action={
          <Select value={String(activeAppId)} onValueChange={changeApp}>
            <SelectTrigger className="w-[240px]">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent>
              {(appsQuery.data || []).map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {item.name} ({item.id})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />

      <section className="metrics-grid">
        <MetricCard label="工作流总数" value={stats?.totalWorkflows || 0} tone="border-zinc-200 bg-zinc-100 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300" />
        <MetricCard label="启用工作流" value={stats?.activeWorkflows || 0} tone="border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" />
        <MetricCard label="运行中实例" value={stats?.runningInstances || 0} tone="border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300" />
        <MetricCard label="待办任务" value={stats?.pendingTasks || 0} tone="border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300" />
      </section>

      <SurfaceCard>
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="grid gap-3 md:grid-cols-[240px_180px]">
            <Input placeholder="搜索工作流" value={workflowKeyword} onChange={(event) => setWorkflowKeyword(event.target.value)} />
            <Select value={workflowStatus} onValueChange={setWorkflowStatus}>
              <SelectTrigger>
                <SelectValue placeholder="状态筛选" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                {WORKFLOW_STATUS_OPTIONS.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={createNewWorkflow}>
              <Plus className="size-4" />
              新建
            </Button>
            <Button variant="outline" onClick={() => setFromTemplateDialogOpen(true)} disabled={!templates.length}>
              <CopyPlus className="size-4" />
              模板创建
            </Button>
            <Button variant="outline" onClick={validateWorkflow}>
              <ShieldCheck className="size-4" />
              校验
            </Button>
            <Button onClick={saveWorkflow} disabled={createWorkflowMutation.isPending || updateWorkflowMutation.isPending}>
              <Save className="size-4" />
              保存
            </Button>
            <Button variant="outline" onClick={() => void toggleDesignerFullscreen()}>
              {isDesignerFullscreen ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
              {isDesignerFullscreen ? "退出全屏" : "进入全屏"}
            </Button>
            <Button variant="outline" onClick={() => setTemplateDialogOpen(true)} disabled={creatingNew}>
              <SquarePen className="size-4" />
              存为模板
            </Button>
            <Button variant="outline" onClick={() => setStartDialogOpen(true)} disabled={creatingNew || !selectedWorkflowId}>
              <Play className="size-4" />
              启动
            </Button>
            <Button variant="destructive" onClick={deleteWorkflow} disabled={creatingNew || !selectedWorkflowId}>
              <Trash2 className="size-4" />
              删除
            </Button>
          </div>
        </div>
      </SurfaceCard>

      <div ref={designerViewportRef} className={designerViewportClass}>
      {isDesignerFullscreen && !isNativeDesignerFullscreen ? <div className="fixed inset-0 z-40 bg-slate-950/45 backdrop-blur-sm dark:bg-slate-950/72" /> : null}

      <section className={designerLayoutClass}>
        {isDesignerFullscreen ? (
          <div className="col-span-full flex flex-wrap items-center justify-between gap-3 rounded-[22px] border border-border/80 bg-background/90 px-3 py-2 dark:bg-background/82">
            <div className="min-w-0">
              <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">全屏设计模式</div>
              <div className="mt-1 text-sm text-foreground">
                {isNativeDesignerFullscreen ? "浏览器全屏已启用，Esc 可退出。" : "已切换至工作台全屏模式。"}
                {" "}Ctrl/Cmd + S 可直接保存。
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant={showDesignerLeftPanel ? "outline" : "secondary"}
                onClick={() => setShowDesignerLeftPanel((current) => !current)}
              >
                {showDesignerLeftPanel ? "收起目录" : "展开目录"}
              </Button>
              <Button
                variant={showDesignerRightPanel ? "outline" : "secondary"}
                onClick={() => setShowDesignerRightPanel((current) => !current)}
              >
                {showDesignerRightPanel ? "收起检查器" : "展开检查器"}
              </Button>
              <Button variant="outline" onClick={validateWorkflow}>
                <ShieldCheck className="size-4" />
                校验
              </Button>
              <Button onClick={saveWorkflow} disabled={createWorkflowMutation.isPending || updateWorkflowMutation.isPending}>
                <Save className="size-4" />
                保存
              </Button>
              <Button variant="outline" onClick={() => void exitDesignerFullscreen()}>
                <Minimize2 className="size-4" />
                退出全屏
              </Button>
            </div>
          </div>
        ) : null}

        {showLeftPanel ? (
          <SurfaceCard className={`overflow-hidden ${isDesignerFullscreen ? "h-full min-h-0" : ""}`}>
            <div className="flex h-full min-h-0 flex-col">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">目录</div>
                  <h2 className="mt-2 text-lg font-semibold text-foreground">工作流</h2>
                </div>
                <Badge variant="outline">{workflows.length} 项</Badge>
              </div>

              <ScrollArea className={`mt-5 pr-4 ${isDesignerFullscreen ? "h-[calc(100dvh-11rem)]" : "h-[760px] max-h-[calc(100vh-16rem)]"}`}>
                <div className="space-y-5">
                  {workflowListQuery.isLoading ? (
                    <LoadingState title="正在加载列表" description="正在读取工作流目录。" />
                  ) : workflows.length === 0 ? (
                    <EmptyState title="暂无工作流" description="当前筛选条件下没有工作流。" />
                  ) : (
                    <div className="space-y-2">
                      {workflows.map((item) => (
                        <button
                          key={item.id}
                          type="button"
                          onClick={() => selectWorkflow(item.id)}
                          className={`w-full rounded-2xl border p-3 text-left transition ${
                            !creatingNew && item.id === selectedWorkflowId
                              ? "border-slate-900 bg-slate-950 text-white dark:border-sky-400/15 dark:bg-slate-950"
                              : "border-border/80 bg-background/75 hover:bg-accent/60 dark:bg-background/58 dark:hover:bg-accent/42"
                          }`}
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="truncate font-semibold">{item.name}</div>
                              <div className={`mt-1 text-xs ${item.id === selectedWorkflowId && !creatingNew ? "text-slate-300 dark:text-slate-300" : "text-muted-foreground"}`}>
                                {toText(item.category, "default")} · v{item.version}
                              </div>
                            </div>
                            <Badge variant={statusBadgeVariant(item.status)}>{toText(item.status, "draft")}</Badge>
                          </div>
                        </button>
                      ))}
                    </div>
                  )}

                  <Separator />

                  <div className="space-y-3">
                    <div className="flex items-center justify-between">
                      <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">模板</div>
                      <Badge variant="outline">{templates.length} 个</Badge>
                    </div>
                    {templates.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-border/80 p-4 text-sm text-muted-foreground">当前应用暂无模板。</div>
                    ) : (
                      <div className="space-y-2">
                        {templates.map((item) => {
                          const record = item as Record<string, unknown>;
                          const isPublic = readBoolean(record, "isPublic", "is_public");
                          return (
                            <div key={item.id} className="rounded-2xl border border-border/80 bg-background/70 p-3 dark:bg-background/56">
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                  <div className="truncate font-medium text-foreground">{toText(item.name, `模板 ${item.id}`)}</div>
                                  <div className="mt-1 text-xs text-muted-foreground">{toText(item.category, "default")}</div>
                                </div>
                                <Badge variant={isPublic ? "success" : "outline"}>{isPublic ? "公开" : "私有"}</Badge>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>

                  <Separator />

                  <div className="space-y-2">
                    <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">节点类型</div>
                    <div className="grid gap-2">
                      {nodeTypes.map((item) => (
                        <button
                          key={item.type}
                          type="button"
                          draggable
                          onDragStart={(event) => {
                            event.dataTransfer.setData("application/aegis-workflow-node-type", item.type);
                            event.dataTransfer.setData("application/aegis-workflow-node-label", item.label);
                            event.dataTransfer.effectAllowed = "copy";
                          }}
                          onClick={() => addNode(item.type, item.label)}
                          className={`rounded-2xl border px-3 py-2.5 text-left transition hover:translate-y-[-1px] ${nodeTone(item.type)}`}
                        >
                          <div className="flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="font-medium">{item.label}</div>
                              <div className="mt-1 text-xs opacity-80">{item.type} · 可拖入画布</div>
                            </div>
                            <Plus className="size-4 shrink-0" />
                          </div>
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              </ScrollArea>
            </div>
          </SurfaceCard>
        ) : null}

        <SurfaceCard className={`overflow-hidden ${isDesignerFullscreen ? "h-full min-h-0" : ""}`}>
          <div className={`section-stack ${isDesignerFullscreen ? "flex h-full min-h-0 flex-col" : ""}`}>
            <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
              <div>
                <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">设计器</div>
                <h2 className="mt-2 text-lg font-semibold text-foreground">{creatingNew ? "新建工作流" : draft.name || "未命名工作流"}</h2>
              </div>
              <div className="flex flex-wrap gap-2">
                <Badge variant={statusBadgeVariant(draft.status)}>{toText(draft.status, "draft")}</Badge>
                <Badge variant="outline">{draft.definition.nodes.length} 节点</Badge>
                <Badge variant="outline">{draft.definition.edges.length} 连线</Badge>
                <Badge variant={startCount === 1 && endCount > 0 ? "success" : "warning"}>开始 {startCount} / 结束 {endCount}</Badge>
              </div>
            </div>

            <WorkflowCanvas
              nodes={draft.definition.nodes}
              edges={draft.definition.edges}
              selectedNodeId={selectedNodeId}
              uiConfigText={draft.uiConfigText}
              isFullscreen={isDesignerFullscreen}
              onSelectNode={setSelectedNodeId}
              onNodesChange={(nodes) =>
                setDraft((current) => ({
                  ...current,
                  definition: {
                    ...current.definition,
                    nodes
                  }
                }))
              }
              onEdgesChange={(edges) =>
                setDraft((current) => ({
                  ...current,
                  definition: {
                    ...current.definition,
                    edges
                  }
                }))
              }
              onUiConfigTextChange={(text) => setDraft((current) => ({ ...current, uiConfigText: text }))}
              onCreateNode={addNode}
            />

            <div className={`rounded-[24px] border border-border/80 bg-background/70 p-4 ${isDesignerFullscreen ? "max-h-[28dvh] overflow-y-auto" : ""}`}>
              <div className="grid gap-3 md:grid-cols-[1fr_1fr_1.2fr_auto]">
                <Select value={edgeDraft.source || "placeholder-source"} onValueChange={(value) => setEdgeDraft((current) => ({ ...current, source: value === "placeholder-source" ? "" : value }))}>
                  <SelectTrigger><SelectValue placeholder="起点节点" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="placeholder-source">选择起点</SelectItem>
                    {draft.definition.nodes.map((node) => <SelectItem key={node.id} value={node.id}>{node.name}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Select value={edgeDraft.target || "placeholder-target"} onValueChange={(value) => setEdgeDraft((current) => ({ ...current, target: value === "placeholder-target" ? "" : value }))}>
                  <SelectTrigger><SelectValue placeholder="终点节点" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="placeholder-target">选择终点</SelectItem>
                    {draft.definition.nodes.map((node) => <SelectItem key={node.id} value={node.id}>{node.name}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Input placeholder="条件表达式，可选" value={edgeDraft.condition} onChange={(event) => setEdgeDraft((current) => ({ ...current, condition: event.target.value }))} />
                <Button type="button" variant="outline" onClick={addEdge}>
                  <GitBranch className="size-4" />
                  新增
                </Button>
              </div>
              <div className="mt-4 space-y-3">
                {draft.definition.edges.map((edge, index) => (
                  <div key={edge.id || `${edge.source}-${edge.target}-${index}`} className="grid gap-3 rounded-2xl border border-border/80 bg-background/80 p-3 md:grid-cols-[1fr_auto_1fr_1.2fr_auto] dark:bg-background/56">
                    <Select value={edge.source} onValueChange={(value) => setDraft((current) => ({ ...current, definition: { ...current.definition, edges: current.definition.edges.map((item, itemIndex) => itemIndex === index ? { ...item, source: value } : item) } }))}>
                      <SelectTrigger><SelectValue placeholder="起点" /></SelectTrigger>
                      <SelectContent>{draft.definition.nodes.map((node) => <SelectItem key={node.id} value={node.id}>{node.name}</SelectItem>)}</SelectContent>
                    </Select>
                    <div className="flex items-center justify-center text-muted-foreground"><ArrowRight className="size-4" /></div>
                    <Select value={edge.target} onValueChange={(value) => setDraft((current) => ({ ...current, definition: { ...current.definition, edges: current.definition.edges.map((item, itemIndex) => itemIndex === index ? { ...item, target: value } : item) } }))}>
                      <SelectTrigger><SelectValue placeholder="终点" /></SelectTrigger>
                      <SelectContent>{draft.definition.nodes.map((node) => <SelectItem key={node.id} value={node.id}>{node.name}</SelectItem>)}</SelectContent>
                    </Select>
                    <Input value={edge.condition || ""} onChange={(event) => setDraft((current) => ({ ...current, definition: { ...current.definition, edges: current.definition.edges.map((item, itemIndex) => itemIndex === index ? { ...item, condition: event.target.value } : item) } }))} />
                    <Button type="button" variant="ghost" size="icon" onClick={() => setDraft((current) => ({ ...current, definition: { ...current.definition, edges: current.definition.edges.filter((_, itemIndex) => itemIndex !== index) } }))}>
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </SurfaceCard>

        {showRightPanel ? (
          <SurfaceCard className={`overflow-hidden ${isDesignerFullscreen ? "h-full min-h-0" : ""}`}>
            <ScrollArea className={isDesignerFullscreen ? "h-[calc(100dvh-11rem)] pr-4" : "h-[760px] max-h-[calc(100vh-16rem)] pr-4"}>
              <Tabs defaultValue="workflow" className="space-y-5">
                <TabsList className="grid w-full grid-cols-2">
                  <TabsTrigger value="workflow">工作流</TabsTrigger>
                  <TabsTrigger value="node">节点</TabsTrigger>
                </TabsList>
                <TabsContent value="workflow" className="space-y-4">
                  <div className="space-y-2"><Label>名称</Label><Input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} /></div>
                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="space-y-2"><Label>分类</Label><Input value={draft.category} onChange={(event) => setDraft((current) => ({ ...current, category: event.target.value }))} /></div>
                    <div className="space-y-2">
                      <Label>状态</Label>
                      <Select value={draft.status} onValueChange={(value) => setDraft((current) => ({ ...current, status: value }))}>
                        <SelectTrigger><SelectValue placeholder="状态" /></SelectTrigger>
                        <SelectContent>{WORKFLOW_STATUS_OPTIONS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
                      </Select>
                    </div>
                  </div>
                  <div className="space-y-2"><Label>说明</Label><Textarea className="min-h-[100px]" value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} /></div>
                  <div className="space-y-2"><Label>触发配置 JSON</Label><Textarea className="min-h-[140px] font-mono text-xs leading-6" value={draft.triggerConfigText} onChange={(event) => setDraft((current) => ({ ...current, triggerConfigText: event.target.value }))} /></div>
                  <div className="space-y-2"><Label>界面配置 JSON</Label><Textarea className="min-h-[120px] font-mono text-xs leading-6" value={draft.uiConfigText} onChange={(event) => setDraft((current) => ({ ...current, uiConfigText: event.target.value }))} /></div>
                  <div className="space-y-2"><Label>权限配置 JSON</Label><Textarea className="min-h-[120px] font-mono text-xs leading-6" value={draft.permissionsText} onChange={(event) => setDraft((current) => ({ ...current, permissionsText: event.target.value }))} /></div>
                </TabsContent>
                <TabsContent value="node" className="space-y-4">
                  {!selectedNode ? (
                    <EmptyState title="未选择节点" description="点击中间画布中的节点后，可在这里编辑节点属性。" />
                  ) : (
                    <>
                      <div className="flex items-center justify-between">
                        <div>
                          <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">当前节点</div>
                          <h3 className="mt-2 text-base font-semibold text-foreground">{selectedNode.name}</h3>
                        </div>
                        <Badge variant="outline">{selectedNode.type}</Badge>
                      </div>
                      <div className="space-y-2"><Label>节点 ID</Label><Input value={selectedNode.id} onChange={(event) => updateSelectedNode({ id: event.target.value })} /></div>
                      <div className="space-y-2"><Label>节点名称</Label><Input value={selectedNode.name} onChange={(event) => updateSelectedNode({ name: event.target.value })} /></div>
                      <div className="space-y-2">
                        <Label>节点类型</Label>
                        <Select value={selectedNode.type} onValueChange={(value) => updateSelectedNode({ type: value })}>
                          <SelectTrigger><SelectValue placeholder="节点类型" /></SelectTrigger>
                          <SelectContent>{nodeTypes.map((item) => <SelectItem key={item.type} value={item.type}>{item.label}</SelectItem>)}</SelectContent>
                        </Select>
                      </div>
                      <div className="space-y-2"><Label>节点配置 JSON</Label><Textarea className="min-h-[220px] font-mono text-xs leading-6" value={nodeConfigText} onChange={(event) => setNodeConfigText(event.target.value)} /></div>
                      <Button variant="outline" onClick={applyNodeConfig}>
                        <Save className="size-4" />
                        应用节点配置
                      </Button>
                    </>
                  )}
                </TabsContent>
              </Tabs>
            </ScrollArea>
          </SurfaceCard>
        ) : null}
      </section>
      </div>

      <SurfaceCard className="overflow-hidden">
        <Tabs defaultValue="instances" className="space-y-5">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
            <div>
              <div className="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">运行台</div>
              <h2 className="mt-2 text-lg font-semibold text-foreground">实例、任务、日志与引擎</h2>
            </div>
            <TabsList>
              <TabsTrigger value="instances">实例</TabsTrigger>
              <TabsTrigger value="tasks">任务</TabsTrigger>
              <TabsTrigger value="logs">日志</TabsTrigger>
              <TabsTrigger value="engine">引擎</TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="instances" className="space-y-4">
            <div className="flex flex-wrap gap-2">
              <Select value={instanceStatus} onValueChange={setInstanceStatus}>
                <SelectTrigger className="w-[220px]"><SelectValue placeholder="实例状态" /></SelectTrigger>
                <SelectContent>{INSTANCE_STATUS_FILTERS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
              </Select>
              <Button variant="outline" onClick={async () => { if (!selectedInstanceId) return; try { await pauseInstanceMutation.mutateAsync({ appid: activeAppId, instance_id: selectedInstanceId }); toast.success("实例已暂停"); } catch (error) { toast.error(getErrorMessage(error)); } }} disabled={!selectedInstanceId}><CirclePause className="size-4" />暂停</Button>
              <Button variant="outline" onClick={async () => { if (!selectedInstanceId) return; try { await resumeInstanceMutation.mutateAsync({ appid: activeAppId, instance_id: selectedInstanceId }); toast.success("实例已恢复"); } catch (error) { toast.error(getErrorMessage(error)); } }} disabled={!selectedInstanceId}><CirclePlay className="size-4" />恢复</Button>
              <Button variant="destructive" onClick={async () => { if (!selectedInstanceId) return; try { await cancelInstanceMutation.mutateAsync({ appid: activeAppId, instance_id: selectedInstanceId }); toast.success("实例已取消"); } catch (error) { toast.error(getErrorMessage(error)); } }} disabled={!selectedInstanceId}><Trash2 className="size-4" />取消</Button>
            </div>
            <div className="table-shell">
              <Table>
                <TableHeader><TableRow><TableHead>实例</TableHead><TableHead>状态</TableHead><TableHead>优先级</TableHead><TableHead>开始时间</TableHead></TableRow></TableHeader>
                <TableBody>
                  {instances.length === 0 ? <TableRow><TableCell colSpan={4} className="py-10 text-center text-muted-foreground">当前没有实例记录。</TableCell></TableRow> : instances.map((item) => {
                    const record = item as Record<string, unknown>;
                    return <TableRow key={item.id} className="cursor-pointer" data-state={item.id === selectedInstanceId ? "selected" : undefined} onClick={() => setSelectedInstanceId(item.id)}><TableCell><div className="font-semibold text-foreground">{toText(readString(record, "instanceName", "instance_name", "workflowName"), `实例 ${item.id}`)}</div></TableCell><TableCell><Badge variant={statusBadgeVariant(readString(record, "status"))}>{toText(readString(record, "status"), "unknown")}</Badge></TableCell><TableCell>{String(record.priority ?? "-")}</TableCell><TableCell className="text-muted-foreground">{formatDate(readString(record, "startedAt", "started_at", "createdAt"))}</TableCell></TableRow>;
                  })}
                </TableBody>
              </Table>
            </div>
            {selectedInstance ? <div className="detail-grid"><div className="kv-pair"><div className="kv-pair-label">当前节点</div><div className="kv-pair-value">{toText(readString(selectedInstance as Record<string, unknown>, "currentNode", "current_node_id", "currentStep"), "无")}</div></div><div className="kv-pair"><div className="kv-pair-label">启动时间</div><div className="kv-pair-value">{formatDate(readString(selectedInstance as Record<string, unknown>, "startedAt", "started_at", "createdAt"))}</div></div><div className="kv-pair"><div className="kv-pair-label">输入数据</div><div className="kv-pair-value">{toText(stringifyJson((selectedInstance as Record<string, unknown>).input_data ?? (selectedInstance as Record<string, unknown>).inputData), "{}")}</div></div></div> : null}
          </TabsContent>

          <TabsContent value="tasks" className="space-y-4">
            <Select value={taskStatus} onValueChange={setTaskStatus}>
              <SelectTrigger className="w-[220px]"><SelectValue placeholder="任务状态" /></SelectTrigger>
              <SelectContent>{TASK_STATUS_FILTERS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
            </Select>
            <div className="table-shell">
              <Table>
                <TableHeader><TableRow><TableHead>任务</TableHead><TableHead>状态</TableHead><TableHead>优先级</TableHead><TableHead>更新时间</TableHead></TableRow></TableHeader>
                <TableBody>
                  {tasks.length === 0 ? <TableRow><TableCell colSpan={4} className="py-10 text-center text-muted-foreground">当前没有任务记录。</TableCell></TableRow> : tasks.map((item) => {
                    const record = item as Record<string, unknown>;
                    return <TableRow key={item.id} className="cursor-pointer" data-state={item.id === selectedTaskId ? "selected" : undefined} onClick={() => setSelectedTaskId(item.id)}><TableCell><div className="font-semibold text-foreground">{toText(readString(record, "name"), `任务 ${item.id}`)}</div></TableCell><TableCell><Badge variant={statusBadgeVariant(readString(record, "status"))}>{toText(readString(record, "status"), "unknown")}</Badge></TableCell><TableCell>{String(record.priority ?? "-")}</TableCell><TableCell className="text-muted-foreground">{formatDate(readString(record, "updatedAt", "completed_at", "createdAt"))}</TableCell></TableRow>;
                  })}
                </TableBody>
              </Table>
            </div>
            {selectedTask ? <div className="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]"><div className="space-y-3 rounded-2xl border border-border/80 bg-background/70 p-4"><div className="font-semibold text-foreground">{toText(readString(selectedTask as Record<string, unknown>, "name"), `任务 ${selectedTask.id}`)}</div><Input value={assignForm.assignedTo} onChange={(event) => setAssignForm((current) => ({ ...current, assignedTo: event.target.value }))} placeholder="分配用户 ID" /><Textarea className="min-h-[80px]" value={assignForm.comment} onChange={(event) => setAssignForm((current) => ({ ...current, comment: event.target.value }))} placeholder="分配备注，可选" /><Button variant="outline" onClick={assignTask}><UserCog className="size-4" />分配任务</Button></div><div className="space-y-3 rounded-2xl border border-border/80 bg-background/70 p-4"><Textarea className="min-h-[160px] font-mono text-xs leading-6" value={completeForm.outputText} onChange={(event) => setCompleteForm((current) => ({ ...current, outputText: event.target.value }))} /><Textarea className="min-h-[90px]" value={completeForm.comment} onChange={(event) => setCompleteForm((current) => ({ ...current, comment: event.target.value }))} placeholder="完成备注，可选" /><Button onClick={completeTask}><ShieldCheck className="size-4" />完成任务</Button></div></div> : null}
            {taskHistoryQuery.data?.length ? <div className="space-y-2">{taskHistoryQuery.data.map((entry: WorkflowLogEntry, index: number) => { const record = entry as Record<string, unknown>; return <div key={`${record.id ?? index}`} className="rounded-2xl border border-border/80 bg-background/80 p-3 dark:bg-background/56"><div className="flex items-center justify-between gap-3"><div className="text-sm font-medium text-foreground">{toText(readString(record, "message"), "日志记录")}</div><Badge variant={statusBadgeVariant(readString(record, "level"))}>{toText(readString(record, "level"), "info")}</Badge></div><div className="mt-2 text-xs text-muted-foreground">{formatDate(readString(record, "createdAt"))}</div></div>; })}</div> : null}
          </TabsContent>

          <TabsContent value="logs" className="table-shell">
            <Table>
              <TableHeader><TableRow><TableHead>消息</TableHead><TableHead>事件</TableHead><TableHead>级别</TableHead><TableHead>时间</TableHead></TableRow></TableHeader>
              <TableBody>{logs.length === 0 ? <TableRow><TableCell colSpan={4} className="py-10 text-center text-muted-foreground">当前没有日志记录。</TableCell></TableRow> : logs.map((entry, index) => { const record = entry as Record<string, unknown>; return <TableRow key={`${record.id ?? index}`}><TableCell className="font-medium text-foreground">{toText(readString(record, "message"), `日志 ${index + 1}`)}</TableCell><TableCell>{toText(readString(record, "event"), "-")}</TableCell><TableCell><Badge variant={statusBadgeVariant(readString(record, "level"))}>{toText(readString(record, "level"), "info")}</Badge></TableCell><TableCell className="text-muted-foreground">{formatDate(readString(record, "createdAt", "timestamp"))}</TableCell></TableRow>; })}</TableBody>
            </Table>
          </TabsContent>

          <TabsContent value="engine" className="space-y-4">
            <div className="detail-grid"><div className="kv-pair"><div className="kv-pair-label">引擎</div><div className="kv-pair-value">{toText((engineStatus as Record<string, unknown>).engine, "temporal")}</div></div><div className="kv-pair"><div className="kv-pair-label">命名空间</div><div className="kv-pair-value">{toText((engineStatus as Record<string, unknown>).namespace, "default")}</div></div><div className="kv-pair"><div className="kv-pair-label">任务队列</div><div className="kv-pair-value">{toText((engineStatus as Record<string, unknown>).taskQueue, "aegis-workflow")}</div></div><div className="kv-pair"><div className="kv-pair-label">调度工作流</div><div className="kv-pair-value">{String((engineStatus as Record<string, unknown>).scheduledWorkflows ?? 0)}</div></div></div>
            <Textarea className="min-h-[280px] font-mono text-xs leading-6" readOnly value={stringifyJson(engineStatus)} />
          </TabsContent>
        </Tabs>
      </SurfaceCard>

      <WorkflowStartDialog open={startDialogOpen} loading={startWorkflowMutation.isPending} form={startForm} onOpenChange={setStartDialogOpen} onChange={setStartForm} onSubmit={startWorkflow} />
      <WorkflowSaveTemplateDialog open={templateDialogOpen} loading={saveAsTemplateMutation.isPending} form={templateForm} onOpenChange={setTemplateDialogOpen} onChange={setTemplateForm} onSubmit={saveAsTemplate} />
      <WorkflowFromTemplateDialog open={fromTemplateDialogOpen} loading={createFromTemplateMutation.isPending} templates={templates as WorkflowTemplate[]} form={fromTemplateForm} onOpenChange={setFromTemplateDialogOpen} onChange={setFromTemplateForm} onSubmit={createFromTemplate} />
    </div>
  );
}
