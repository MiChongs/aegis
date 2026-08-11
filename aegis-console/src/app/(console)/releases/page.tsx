"use client";

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  BarChart3,
  ChevronDown,
  CirclePlus,
  Code2,
  Copy,
  Filter,
  GitBranch,
  Hash,
  MoreHorizontal,
  Package,
  Palette,
  Pencil,
  Plus,
  RefreshCw,
  Rocket,
  RotateCcw,
  Smartphone,
  Trash2,
  Undo2,
  X
} from "lucide-react";
import type { ChannelRule, VersionChannel, VersionItem } from "@/lib/api-client";
import { ApiError } from "@/lib/api-client";
import {
  useAdminAppsQuery,
  useAdminVersionChannelsQuery,
  useAdminVersionStatsQuery,
  useAdminVersionsQuery,
  useCreateAdminVersionChannelMutation,
  useCreateAdminVersionMutation,
  useDeleteAdminVersionChannelMutation,
  useDeleteAdminVersionMutation,
  usePublishAdminVersionMutation,
  useRevokeAdminVersionMutation,
  useUpdateAdminVersionChannelMutation,
  useUpdateAdminVersionMutation
} from "@/lib/admin-hooks";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  常量 & 工具                                                         */
/* ------------------------------------------------------------------ */

const DATA_FONT = "font-mono tabular-nums";

const platforms = [
  { value: "android", label: "Android" },
  { value: "ios", label: "iOS" },
  { value: "windows", label: "Windows" },
  { value: "macos", label: "macOS" },
  { value: "linux", label: "Linux" },
  { value: "all", label: "全平台" },
];

const statusOptions = [
  { value: "draft", label: "草稿" },
  { value: "published", label: "已发布" },
  { value: "revoked", label: "已撤回" },
];

function statusBadge(s?: string): "success" | "warning" | "secondary" | "danger" {
  if (s === "published") return "success";
  if (s === "revoked") return "danger";
  return "secondary";
}

function fmtDate(v?: string | null) {
  if (!v) return "--";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "--";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(d);
}

function fmtSize(bytes?: number | null) {
  if (!bytes || bytes <= 0) return "--";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1048576).toFixed(2)} MB`;
}

function safeJson(v: string) {
  try {
    const p = JSON.parse(v);
    if (typeof p === "object" && p && !Array.isArray(p)) return p as Record<string, unknown>;
  } catch { /* noop */ }
  return {};
}

/* ------------------------------------------------------------------ */
/*  版本编辑弹窗                                                        */
/* ------------------------------------------------------------------ */

type VersionFormData = {
  version: string;
  version_code: string;
  channel_id: string;
  platform: string;
  status: string;
  update_type: string;
  force_update: boolean;
  download_url: string;
  min_os_version: string;
  file_size: string;
  file_hash: string;
  description: string;
  release_notes: string;
  metadata: string;
};

const emptyVersionForm: VersionFormData = {
  version: "", version_code: "", channel_id: "", platform: "android",
  status: "draft", update_type: "optional", force_update: false,
  download_url: "", min_os_version: "", file_size: "0", file_hash: "",
  description: "", release_notes: "", metadata: "{}",
};

function versionToForm(item: VersionItem): VersionFormData {
  return {
    version: item.version || "",
    version_code: String(item.version_code || ""),
    channel_id: item.channel_id ? String(item.channel_id) : "",
    platform: item.platform || "android",
    status: item.status || "draft",
    update_type: item.update_type || "optional",
    force_update: item.force_update === true,
    download_url: item.download_url || "",
    min_os_version: item.min_os_version || "",
    file_size: String(item.file_size || 0),
    file_hash: item.file_hash || "",
    description: item.description || "",
    release_notes: item.release_notes || "",
    metadata: JSON.stringify(item.metadata || {}, null, 2),
  };
}

function VersionDialog({
  mode,
  item,
  appKey,
  channels,
  open,
  onOpenChange,
}: {
  mode: "create" | "edit";
  item?: VersionItem | null;
  appKey: string;
  channels: VersionChannel[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState<VersionFormData>(item ? versionToForm(item) : emptyVersionForm);
  const createMutation = useCreateAdminVersionMutation();
  const updateMutation = useUpdateAdminVersionMutation();
  const busy = createMutation.isPending || updateMutation.isPending;

  const set = useCallback(<K extends keyof VersionFormData>(k: K, v: VersionFormData[K]) => {
    setForm((s) => ({ ...s, [k]: v }));
  }, []);

  async function handleSubmit() {
    const payload: Record<string, unknown> = {
      version: form.version,
      version_code: Number(form.version_code || 0),
      channel_id: form.channel_id ? Number(form.channel_id) : undefined,
      platform: form.platform,
      status: form.status,
      update_type: form.update_type,
      force_update: form.force_update,
      download_url: form.download_url,
      min_os_version: form.min_os_version,
      file_size: Number(form.file_size || 0),
      file_hash: form.file_hash,
      description: form.description,
      release_notes: form.release_notes,
      metadata: safeJson(form.metadata),
    };
    try {
      if (mode === "edit" && item) {
        await updateMutation.mutateAsync({ appKey, versionId: item.id, payload });
        toast.success("版本已更新");
      } else {
        await createMutation.mutateAsync({ appKey, payload });
        toast.success("版本已创建");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{mode === "edit" ? "编辑版本" : "创建版本"}</DialogTitle>
          <DialogDescription>
            {mode === "edit" ? `版本 ${item?.version || item?.id}` : "填写版本信息，创建后可随时发布。"}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-1.5">
            <Label>版本号</Label>
            <Input placeholder="1.2.0" value={form.version} onChange={(e) => set("version", e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>版本码</Label>
            <Input placeholder="120" value={form.version_code} onChange={(e) => set("version_code", e.target.value.replace(/\D/g, ""))} />
          </div>
          <div className="space-y-1.5">
            <Label>平台</Label>
            <Select value={form.platform} onValueChange={(v) => set("platform", v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {platforms.map((p) => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>渠道</Label>
            <Select value={form.channel_id || "__none__"} onValueChange={(v) => set("channel_id", v === "__none__" ? "" : v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">默认渠道</SelectItem>
                {channels.map((ch) => <SelectItem key={ch.id} value={String(ch.id)}>{ch.name || ch.code}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>状态</Label>
            <Select value={form.status} onValueChange={(v) => set("status", v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {statusOptions.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>更新类型</Label>
            <Select value={form.update_type} onValueChange={(v) => set("update_type", v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="optional">可选更新</SelectItem>
                <SelectItem value="mandatory">强制更新</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <Label>下载地址</Label>
            <Input placeholder="https://..." value={form.download_url} onChange={(e) => set("download_url", e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>最小系统版本</Label>
            <Input placeholder="Android 8.0" value={form.min_os_version} onChange={(e) => set("min_os_version", e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label>文件大小（字节）</Label>
            <Input placeholder="0" value={form.file_size} onChange={(e) => set("file_size", e.target.value.replace(/\D/g, ""))} />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <Label>文件哈希</Label>
            <Input placeholder="sha256:..." value={form.file_hash} onChange={(e) => set("file_hash", e.target.value)} />
          </div>
          <div className="flex items-center gap-3 md:col-span-2">
            <Switch checked={form.force_update} onCheckedChange={(v) => set("force_update", v)} />
            <Label>强制更新</Label>
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <Label>描述</Label>
            <Textarea rows={2} value={form.description} onChange={(e) => set("description", e.target.value)} />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <Label>发布说明</Label>
            <Textarea rows={4} placeholder="本次更新内容..." value={form.release_notes} onChange={(e) => set("release_notes", e.target.value)} />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <Label>Metadata JSON</Label>
            <Textarea rows={4} className="font-mono text-xs" value={form.metadata} onChange={(e) => set("metadata", e.target.value)} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={handleSubmit} disabled={busy || !form.version}>
            {busy ? "保存中..." : mode === "edit" ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------------------------------------------ */
/*  渠道编辑弹窗                                                        */
/* ------------------------------------------------------------------ */

type AudienceCondition = {
  key: string;   // 属性键：vip_level / age_min / age_max / register_after / region / tag ...
  op: string;    // 操作符
  value: string; // 值
};

type ChannelFormData = {
  name: string;
  code: string;
  description: string;
  is_default: boolean;
  status: boolean;
  priority: number;
  color: string;
  level: string;
  rollout_pct: number;
  platforms: string[];
  min_version_code: string;
  max_version_code: string;
  rules: ChannelRule[];
  audience: AudienceCondition[];
  audienceJsonMode: boolean;
  audienceJson: string;
};

const emptyChannelForm: ChannelFormData = {
  name: "", code: "", description: "", is_default: false, status: true,
  priority: 0, color: "", level: "stable", rollout_pct: 100,
  platforms: [], min_version_code: "0", max_version_code: "0",
  rules: [], audience: [], audienceJsonMode: false, audienceJson: "{}",
};

const audienceKeys = [
  { value: "vip_level", label: "VIP 等级" },
  { value: "level", label: "用户等级" },
  { value: "age_min", label: "最小年龄" },
  { value: "age_max", label: "最大年龄" },
  { value: "gender", label: "性别" },
  { value: "register_after", label: "注册日期(起)" },
  { value: "register_before", label: "注册日期(止)" },
  { value: "region", label: "地区" },
  { value: "language", label: "语言" },
  { value: "tag", label: "用户标签" },
  { value: "points_min", label: "最低积分" },
  { value: "points_max", label: "最高积分" },
  { value: "signin_days", label: "签到天数" },
  { value: "custom", label: "自定义键" },
];

const audienceOps = [
  { value: "eq", label: "等于" },
  { value: "neq", label: "不等于" },
  { value: "gt", label: "大于" },
  { value: "lt", label: "小于" },
  { value: "gte", label: "大于等于" },
  { value: "lte", label: "小于等于" },
  { value: "in", label: "包含于" },
  { value: "not_in", label: "不包含于" },
  { value: "contains", label: "包含" },
];

const channelLevels = [
  { value: "stable", label: "Stable", color: "bg-emerald-500" },
  { value: "beta", label: "Beta", color: "bg-blue-500" },
  { value: "alpha", label: "Alpha", color: "bg-amber-500" },
  { value: "canary", label: "Canary", color: "bg-orange-500" },
  { value: "nightly", label: "Nightly", color: "bg-purple-500" },
];

const ruleFields = [
  { value: "platform", label: "平台" },
  { value: "os_version", label: "系统版本" },
  { value: "app_version", label: "应用版本" },
  { value: "user_id", label: "用户 ID" },
  { value: "region", label: "地区" },
  { value: "language", label: "语言" },
  { value: "tag", label: "标签" },
  { value: "device", label: "设备型号" },
];

const ruleOps = [
  { value: "eq", label: "等于" },
  { value: "neq", label: "不等于" },
  { value: "in", label: "包含于" },
  { value: "not_in", label: "不包含于" },
  { value: "gt", label: "大于" },
  { value: "lt", label: "小于" },
  { value: "gte", label: "大于等于" },
  { value: "lte", label: "小于等于" },
  { value: "regex", label: "正则匹配" },
  { value: "contains", label: "包含" },
];

function audienceToConditions(ta?: Record<string, unknown>): AudienceCondition[] {
  if (!ta || typeof ta !== "object") return [];
  const out: AudienceCondition[] = [];
  for (const [key, val] of Object.entries(ta)) {
    if (val === null || val === undefined) continue;
    if (typeof val === "object" && !Array.isArray(val)) {
      // 结构化条件 { op: "gte", value: "5" }
      const obj = val as Record<string, unknown>;
      out.push({ key, op: String(obj.op || "eq"), value: String(obj.value ?? "") });
    } else {
      out.push({ key, op: "eq", value: Array.isArray(val) ? (val as string[]).join(", ") : String(val) });
    }
  }
  return out;
}

function conditionsToAudience(conds: AudienceCondition[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const c of conds) {
    if (!c.key) continue;
    if (c.op === "eq") {
      out[c.key] = c.value;
    } else {
      out[c.key] = { op: c.op, value: c.value };
    }
  }
  return out;
}

function channelToForm(ch: VersionChannel): ChannelFormData {
  const audience = audienceToConditions(ch.targetAudience);
  return {
    name: ch.name || "",
    code: ch.code || "",
    description: ch.description || "",
    is_default: ch.is_default === true,
    status: ch.status !== false,
    priority: ch.priority ?? 0,
    color: ch.color || "",
    level: ch.level || "stable",
    rollout_pct: ch.rollout_pct ?? 100,
    platforms: ch.platforms || [],
    min_version_code: String(ch.min_version_code || 0),
    max_version_code: String(ch.max_version_code || 0),
    rules: ch.rules || [],
    audience,
    audienceJsonMode: false,
    audienceJson: JSON.stringify(ch.targetAudience || {}, null, 2),
  };
}

/* 目标人群条件行 */
function AudienceRow({
  cond,
  index,
  onChange,
  onRemove,
}: {
  cond: AudienceCondition;
  index: number;
  onChange: (index: number, cond: AudienceCondition) => void;
  onRemove: (index: number) => void;
}) {
  const isCustom = !audienceKeys.some((k) => k.value === cond.key) && cond.key !== "custom";
  const [customKey, setCustomKey] = useState(isCustom ? cond.key : "");

  return (
    <div className="flex items-center gap-2">
      <Select
        value={isCustom ? "custom" : (cond.key || "")}
        onValueChange={(v) => {
          if (v === "custom") {
            onChange(index, { ...cond, key: customKey || "" });
          } else {
            onChange(index, { ...cond, key: v });
          }
        }}
      >
        <SelectTrigger className="w-28 h-8 text-xs"><SelectValue placeholder="属性" /></SelectTrigger>
        <SelectContent>
          {audienceKeys.map((k) => <SelectItem key={k.value} value={k.value}>{k.label}</SelectItem>)}
        </SelectContent>
      </Select>
      {(cond.key === "custom" || isCustom) && (
        <Input
          className="w-20 h-8 text-xs"
          placeholder="键名"
          value={customKey}
          onChange={(e) => {
            setCustomKey(e.target.value);
            onChange(index, { ...cond, key: e.target.value });
          }}
        />
      )}
      <Select value={cond.op || "eq"} onValueChange={(v) => onChange(index, { ...cond, op: v })}>
        <SelectTrigger className="w-24 h-8 text-xs"><SelectValue /></SelectTrigger>
        <SelectContent>
          {audienceOps.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <Input
        className="flex-1 h-8 text-xs"
        placeholder="值"
        value={cond.value}
        onChange={(e) => onChange(index, { ...cond, value: e.target.value })}
      />
      <Button variant="ghost" size="icon" className="size-8 shrink-0 text-muted-foreground hover:text-red-500" onClick={() => onRemove(index)}>
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

/* 可视化规则构建器行 */
function RuleRow({
  rule,
  index,
  onChange,
  onRemove,
}: {
  rule: ChannelRule;
  index: number;
  onChange: (index: number, rule: ChannelRule) => void;
  onRemove: (index: number) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Select value={rule.field || ""} onValueChange={(v) => onChange(index, { ...rule, field: v })}>
        <SelectTrigger className="w-28 h-8 text-xs"><SelectValue placeholder="字段" /></SelectTrigger>
        <SelectContent>
          {ruleFields.map((f) => <SelectItem key={f.value} value={f.value}>{f.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <Select value={rule.op || "eq"} onValueChange={(v) => onChange(index, { ...rule, op: v })}>
        <SelectTrigger className="w-24 h-8 text-xs"><SelectValue /></SelectTrigger>
        <SelectContent>
          {ruleOps.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <Input
        className="flex-1 h-8 text-xs"
        placeholder="值（逗号分隔多个）"
        value={typeof rule.value === "string" ? rule.value : Array.isArray(rule.value) ? (rule.value as string[]).join(", ") : String(rule.value ?? "")}
        onChange={(e) => {
          const raw = e.target.value;
          const val = (rule.op === "in" || rule.op === "not_in") ? raw.split(",").map((s) => s.trim()).filter(Boolean) : raw;
          onChange(index, { ...rule, value: val });
        }}
      />
      <Button variant="ghost" size="icon" className="size-8 shrink-0 text-muted-foreground hover:text-red-500" onClick={() => onRemove(index)}>
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

function ChannelDialog({
  mode,
  item,
  appKey,
  open,
  onOpenChange,
}: {
  mode: "create" | "edit";
  item?: VersionChannel | null;
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState<ChannelFormData>(item ? channelToForm(item) : emptyChannelForm);
  const createMutation = useCreateAdminVersionChannelMutation();
  const updateMutation = useUpdateAdminVersionChannelMutation();
  const busy = createMutation.isPending || updateMutation.isPending;

  const set = useCallback(<K extends keyof ChannelFormData>(k: K, v: ChannelFormData[K]) => {
    setForm((s) => ({ ...s, [k]: v }));
  }, []);

  const togglePlatform = useCallback((p: string) => {
    setForm((s) => ({
      ...s,
      platforms: s.platforms.includes(p) ? s.platforms.filter((x) => x !== p) : [...s.platforms, p],
    }));
  }, []);

  const updateRule = useCallback((i: number, r: ChannelRule) => {
    setForm((s) => ({ ...s, rules: s.rules.map((old, idx) => idx === i ? r : old) }));
  }, []);

  const removeRule = useCallback((i: number) => {
    setForm((s) => ({ ...s, rules: s.rules.filter((_, idx) => idx !== i) }));
  }, []);

  const addRule = useCallback(() => {
    setForm((s) => ({ ...s, rules: [...s.rules, { field: "platform", op: "eq", value: "" }] }));
  }, []);

  const updateAudience = useCallback((i: number, c: AudienceCondition) => {
    setForm((s) => ({ ...s, audience: s.audience.map((old, idx) => idx === i ? c : old) }));
  }, []);

  const removeAudience = useCallback((i: number) => {
    setForm((s) => ({ ...s, audience: s.audience.filter((_, idx) => idx !== i) }));
  }, []);

  const addAudience = useCallback(() => {
    setForm((s) => ({ ...s, audience: [...s.audience, { key: "vip_level", op: "gte", value: "" }] }));
  }, []);

  const toggleAudienceMode = useCallback(() => {
    setForm((s) => {
      if (s.audienceJsonMode) {
        // JSON -> 可视化：解析 JSON 为条件
        const parsed = safeJson(s.audienceJson);
        return { ...s, audienceJsonMode: false, audience: audienceToConditions(parsed) };
      }
      // 可视化 -> JSON：条件序列化为 JSON
      return { ...s, audienceJsonMode: true, audienceJson: JSON.stringify(conditionsToAudience(s.audience), null, 2) };
    });
  }, []);

  async function handleSubmit() {
    const targetAudience = form.audienceJsonMode
      ? safeJson(form.audienceJson)
      : conditionsToAudience(form.audience);
    const payload: Record<string, unknown> = {
      name: form.name,
      code: form.code,
      description: form.description,
      is_default: form.is_default,
      status: form.status,
      priority: form.priority,
      color: form.color,
      level: form.level,
      rollout_pct: form.rollout_pct,
      platforms: form.platforms,
      min_version_code: Number(form.min_version_code || 0),
      max_version_code: Number(form.max_version_code || 0),
      rules: form.rules,
      targetAudience,
    };
    try {
      if (mode === "edit" && item) {
        await updateMutation.mutateAsync({ appKey, channelId: item.id, payload });
        toast.success("渠道已更新");
      } else {
        await createMutation.mutateAsync({ appKey, payload });
        toast.success("渠道已创建");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  const levelInfo = channelLevels.find((l) => l.value === form.level) || channelLevels[0];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{mode === "edit" ? "编辑渠道" : "创建渠道"}</DialogTitle>
          <DialogDescription>
            {mode === "edit" ? `渠道 ${item?.name || item?.code}` : "多维度配置发布渠道，支持灰度分发规则。"}
          </DialogDescription>
        </DialogHeader>

        <Accordion type="multiple" defaultValue={["basic", "distribution", "rules"]} className="space-y-2">
          {/* 基本信息 */}
          <AccordionItem value="basic" className="rounded-lg border overflow-hidden border-b-0">
            <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">基本信息</AccordionTrigger>
            <AccordionContent className="px-4 pb-4 space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-1.5">
                  <Label>名称</Label>
                  <Input placeholder="正式渠道" value={form.name} onChange={(e) => set("name", e.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <Label>代码</Label>
                  <Input placeholder="stable" value={form.code} onChange={(e) => set("code", e.target.value)} />
                </div>
              </div>
              <div className="space-y-1.5">
                <Label>说明</Label>
                <Textarea rows={2} value={form.description} onChange={(e) => set("description", e.target.value)} />
              </div>
              <div className="grid gap-4 md:grid-cols-3">
                <div className="space-y-1.5">
                  <Label>级别</Label>
                  <Select value={form.level} onValueChange={(v) => set("level", v)}>
                    <SelectTrigger>
                      <div className="flex items-center gap-2">
                        <span className={cn("size-2 rounded-full", levelInfo.color)} />
                        <SelectValue />
                      </div>
                    </SelectTrigger>
                    <SelectContent>
                      {channelLevels.map((l) => (
                        <SelectItem key={l.value} value={l.value}>
                          <div className="flex items-center gap-2">
                            <span className={cn("size-2 rounded-full", l.color)} />
                            {l.label}
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label>优先级</Label>
                  <Input type="number" value={form.priority} onChange={(e) => set("priority", Number(e.target.value))} />
                </div>
                <div className="space-y-1.5">
                  <Label className="flex items-center gap-1.5"><Palette className="size-3" />标签颜色</Label>
                  <div className="flex items-center gap-2">
                    <Input
                      placeholder="#3B82F6"
                      value={form.color}
                      onChange={(e) => set("color", e.target.value)}
                      className="flex-1"
                    />
                    {form.color && (
                      <span className="size-7 rounded-md border shrink-0" style={{ backgroundColor: form.color }} />
                    )}
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-6">
                <div className="flex items-center gap-2">
                  <Switch checked={form.status} onCheckedChange={(v) => set("status", v)} />
                  <Label>启用</Label>
                </div>
                <div className="flex items-center gap-2">
                  <Switch checked={form.is_default} onCheckedChange={(v) => set("is_default", v)} />
                  <Label>默认渠道</Label>
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>

          {/* 分发策略 */}
          <AccordionItem value="distribution" className="rounded-lg border overflow-hidden border-b-0">
            <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">分发策略</AccordionTrigger>
            <AccordionContent className="px-4 pb-4 space-y-4">
              <div className="space-y-1.5">
                <Label>灰度放量 {form.rollout_pct}%</Label>
                <div className="flex items-center gap-3">
                  <input
                    type="range" min={0} max={100} value={form.rollout_pct}
                    onChange={(e) => set("rollout_pct", Number(e.target.value))}
                    title="灰度放量百分比"
                    className="flex-1 h-2 accent-primary cursor-pointer"
                  />
                  <span className={cn("text-sm font-semibold w-12 text-right", DATA_FONT)}>{form.rollout_pct}%</span>
                </div>
                <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                  <div
                    className="h-full rounded-full bg-primary transition-all"
                    style={{ width: `${form.rollout_pct}%` }}
                  />
                </div>
              </div>
              <div className="space-y-1.5">
                <Label>限定平台</Label>
                <div className="flex flex-wrap gap-2">
                  {platforms.map((p) => (
                    <button
                      key={p.value}
                      type="button"
                      className={cn(
                        "rounded-md border px-3 py-1.5 text-xs font-medium transition-colors",
                        form.platforms.includes(p.value)
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-border text-muted-foreground hover:bg-muted/50"
                      )}
                      onClick={() => togglePlatform(p.value)}
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
                <p className="text-[10px] text-muted-foreground">不选则不限制平台</p>
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-1.5">
                  <Label>最低版本码</Label>
                  <Input placeholder="0 = 不限" value={form.min_version_code} onChange={(e) => set("min_version_code", e.target.value.replace(/\D/g, ""))} />
                </div>
                <div className="space-y-1.5">
                  <Label>最高版本码</Label>
                  <Input placeholder="0 = 不限" value={form.max_version_code} onChange={(e) => set("max_version_code", e.target.value.replace(/\D/g, ""))} />
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>

          {/* 灰度规则 */}
          <AccordionItem value="rules" className="rounded-lg border overflow-hidden border-b-0">
            <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">
              <div className="flex items-center gap-2">
                <Filter className="size-3.5" />
                灰度规则
                {form.rules.length > 0 && <Badge variant="secondary" className="text-[10px]">{form.rules.length}</Badge>}
              </div>
            </AccordionTrigger>
            <AccordionContent className="px-4 pb-4 space-y-3">
              {form.rules.length === 0 && (
                <p className="text-xs text-muted-foreground py-2">暂无规则，所有用户均可匹配此渠道。</p>
              )}
              {form.rules.map((rule, i) => (
                <RuleRow key={i} rule={rule} index={i} onChange={updateRule} onRemove={removeRule} />
              ))}
              <Button variant="outline" size="sm" onClick={addRule} className="w-full">
                <CirclePlus className="size-3.5 mr-1.5" />添加规则
              </Button>
              <p className="text-[10px] text-muted-foreground">多条规则之间为 AND 关系，全部满足才匹配。</p>
            </AccordionContent>
          </AccordionItem>

          {/* 目标人群 */}
          <AccordionItem value="audience" className="rounded-lg border overflow-hidden border-b-0">
            <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">
              <div className="flex items-center gap-2">
                目标人群
                {form.audience.length > 0 && !form.audienceJsonMode && (
                  <Badge variant="secondary" className="text-[10px]">{form.audience.length} 条件</Badge>
                )}
              </div>
            </AccordionTrigger>
            <AccordionContent className="px-4 pb-4 space-y-3">
              {/* 模式切换 */}
              <div className="flex items-center justify-between">
                <p className="text-xs text-muted-foreground">
                  {form.audienceJsonMode ? "JSON 编辑模式" : "可视化配置模式"}
                </p>
                <Button variant="ghost" size="sm" className="h-7 text-xs gap-1" onClick={toggleAudienceMode}>
                  <Code2 className="size-3" />
                  {form.audienceJsonMode ? "切换可视化" : "切换 JSON"}
                </Button>
              </div>

              {form.audienceJsonMode ? (
                <Textarea
                  rows={8}
                  className="font-mono text-xs"
                  value={form.audienceJson}
                  onChange={(e) => set("audienceJson", e.target.value)}
                />
              ) : (
                <>
                  {form.audience.length === 0 && (
                    <p className="text-xs text-muted-foreground py-2">暂无条件，不限制目标人群。</p>
                  )}
                  {form.audience.map((cond, i) => (
                    <AudienceRow key={i} cond={cond} index={i} onChange={updateAudience} onRemove={removeAudience} />
                  ))}
                  <Button variant="outline" size="sm" onClick={addAudience} className="w-full">
                    <CirclePlus className="size-3.5 mr-1.5" />添加条件
                  </Button>
                </>
              )}
              <p className="text-[10px] text-muted-foreground">多个条件之间为 AND 关系。可视化模式与 JSON 模式可互相切换。</p>
            </AccordionContent>
          </AccordionItem>
        </Accordion>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={handleSubmit} disabled={busy || !form.name || !form.code}>
            {busy ? "保存中..." : mode === "edit" ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------------------------------------------ */
/*  版本卡片                                                            */
/* ------------------------------------------------------------------ */

function VersionCard({
  item,
  appKey,
  channels,
  onEdit,
}: {
  item: VersionItem;
  appKey: string;
  channels: VersionChannel[];
  onEdit: () => void;
}) {
  const publishMutation = usePublishAdminVersionMutation();
  const revokeMutation = useRevokeAdminVersionMutation();
  const deleteMutation = useDeleteAdminVersionMutation();

  const channelName = useMemo(() => {
    if (!item.channel_id) return "默认";
    return channels.find((c) => c.id === item.channel_id)?.name || `渠道 ${item.channel_id}`;
  }, [item.channel_id, channels]);

  const handlePublish = async () => {
    try {
      await publishMutation.mutateAsync({ appKey, versionId: item.id });
      toast.success("版本已发布");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "发布失败");
    }
  };

  const handleRevoke = async () => {
    try {
      await revokeMutation.mutateAsync({ appKey, versionId: item.id });
      toast.success("版本已撤回");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤回失败");
    }
  };

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync({ appKey, versionId: item.id });
      toast.success("版本已删除");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  };

  return (
    <div className="rounded-xl border bg-card overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
            <Package className="size-4 text-muted-foreground" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className={cn("text-sm font-semibold", DATA_FONT)}>{item.version || `v${item.id}`}</span>
              <Badge variant={statusBadge(item.status)} className="text-[10px]">{item.status === "published" ? "已发布" : item.status === "revoked" ? "已撤回" : "草稿"}</Badge>
              {item.force_update && <Badge variant="danger" className="text-[10px]">强制</Badge>}
            </div>
            <div className="flex items-center gap-2 text-[11px] text-muted-foreground mt-0.5">
              <span className="flex items-center gap-0.5"><Smartphone className="size-3" />{item.platform || "android"}</span>
              <span className="flex items-center gap-0.5"><GitBranch className="size-3" />{channelName}</span>
              <span className="flex items-center gap-0.5"><Hash className="size-3" />{item.version_code || 0}</span>
            </div>
          </div>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 shrink-0">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>
              <Pencil className="size-3.5 mr-2" />编辑
            </DropdownMenuItem>
            {item.status !== "published" && (
              <DropdownMenuItem onClick={handlePublish}>
                <Rocket className="size-3.5 mr-2" />发布
              </DropdownMenuItem>
            )}
            {item.status === "published" && (
              <DropdownMenuItem onClick={handleRevoke}>
                <Undo2 className="size-3.5 mr-2" />撤回
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleDelete} className="text-red-600 dark:text-red-400">
              <Trash2 className="size-3.5 mr-2" />删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* 指标行 */}
      <div className="grid grid-cols-4 gap-px bg-border">
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">更新类型</div>
          <div className="text-xs font-medium">{item.update_type === "mandatory" ? "强制" : "可选"}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">文件大小</div>
          <div className={cn("text-xs font-medium", DATA_FONT)}>{fmtSize(item.file_size)}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">下载量</div>
          <div className={cn("text-xs font-medium", DATA_FONT)}>{item.download_count ?? 0}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">更新时间</div>
          <div className="text-xs font-medium">{fmtDate(item.updatedAt)}</div>
        </div>
      </div>

      {/* 发布说明（折叠） */}
      {item.release_notes ? (
        <Accordion type="single" collapsible>
          <AccordionItem value="notes" className="border-b-0">
            <AccordionTrigger className="px-4 py-2 text-xs text-muted-foreground hover:no-underline">
              发布说明
            </AccordionTrigger>
            <AccordionContent className="px-4 pb-3">
              <p className="whitespace-pre-wrap text-xs text-muted-foreground leading-relaxed">{item.release_notes}</p>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      ) : null}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  渠道卡片                                                            */
/* ------------------------------------------------------------------ */

function ChannelCard({
  item,
  appKey,
  onEdit,
}: {
  item: VersionChannel;
  appKey: string;
  onEdit: () => void;
}) {
  const deleteMutation = useDeleteAdminVersionChannelMutation();
  const levelInfo = channelLevels.find((l) => l.value === item.level) || channelLevels[0];
  const rulesCount = item.rules?.length ?? 0;

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync({ appKey, channelId: item.id });
      toast.success("渠道已删除");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  };

  return (
    <div className="rounded-xl border bg-card overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
            {item.color ? (
              <span className="size-4 rounded" style={{ backgroundColor: item.color }} />
            ) : (
              <GitBranch className="size-4 text-muted-foreground" />
            )}
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-sm font-semibold">{item.name || `渠道 ${item.id}`}</span>
              <Badge variant="outline" className="text-[10px]">{item.code || "--"}</Badge>
              <Badge variant="secondary" className="text-[10px] gap-1">
                <span className={cn("size-1.5 rounded-full", levelInfo.color)} />
                {levelInfo.label}
              </Badge>
              {item.is_default && <Badge variant="secondary" className="text-[10px]">默认</Badge>}
              <Badge variant={item.status === false ? "warning" : "success"} className="text-[10px]">
                {item.status === false ? "停用" : "启用"}
              </Badge>
            </div>
            <div className="flex items-center gap-3 text-[11px] text-muted-foreground mt-0.5">
              {item.description && <span className="truncate max-w-48">{item.description}</span>}
            </div>
          </div>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 shrink-0">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>
              <Pencil className="size-3.5 mr-2" />编辑
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleDelete} className="text-red-600 dark:text-red-400" disabled={item.is_default}>
              <Trash2 className="size-3.5 mr-2" />删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* 指标行 */}
      <div className="grid grid-cols-4 gap-px bg-border">
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">放量</div>
          <div className={cn("text-xs font-semibold", DATA_FONT)}>{item.rollout_pct ?? 100}%</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">优先级</div>
          <div className={cn("text-xs font-semibold", DATA_FONT)}>{item.priority ?? 0}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">用户</div>
          <div className={cn("text-xs font-semibold", DATA_FONT)}>{item.userCount ?? 0}</div>
        </div>
        <div className="bg-card px-3 py-1.5">
          <div className="text-[9px] font-medium uppercase text-muted-foreground">规则</div>
          <div className={cn("text-xs font-semibold", DATA_FONT)}>{rulesCount} 条</div>
        </div>
      </div>

      {/* 底部标签行 */}
      {((item.platforms && item.platforms.length > 0) || rulesCount > 0) && (
        <div className="flex flex-wrap items-center gap-1.5 px-4 py-2">
          {(item.platforms || []).map((p) => (
            <Badge key={p} variant="outline" className="text-[9px] px-1.5">{p}</Badge>
          ))}
          {rulesCount > 0 && (
            <Badge variant="outline" className="text-[9px] px-1.5 gap-0.5">
              <Filter className="size-2.5" />{rulesCount} 条规则
            </Badge>
          )}
        </div>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  主页面                                                              */
/* ------------------------------------------------------------------ */

export default function ReleasesPage() {
  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data || [];
  const [selectedAppKey, setSelectedAppKey] = useState<string | null>(null);
  const resolvedApp = useMemo(() => {
    if (selectedAppKey) return apps.find((a) => a.appKey === selectedAppKey) || apps[0] || null;
    return apps[0] || null;
  }, [apps, selectedAppKey]);
  const appKey = resolvedApp?.appKey || null;

  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [platformFilter, setPlatformFilter] = useState<string>("all");

  const versionsQuery = useAdminVersionsQuery(appKey, {
    page, limit: 20,
    status: statusFilter !== "all" ? statusFilter : undefined,
    platform: platformFilter !== "all" ? platformFilter : undefined,
  });
  const channelsQuery = useAdminVersionChannelsQuery(appKey);
  const statsQuery = useAdminVersionStatsQuery(appKey);
  const channels = channelsQuery.data || [];
  const stats = statsQuery.data;
  const versions = versionsQuery.data?.items || [];
  const totalPages = versionsQuery.data?.totalPages || 1;

  // Dialog 状态
  const [versionDialog, setVersionDialog] = useState<{ open: boolean; mode: "create" | "edit"; item?: VersionItem | null }>({ open: false, mode: "create" });
  const [channelDialog, setChannelDialog] = useState<{ open: boolean; mode: "create" | "edit"; item?: VersionChannel | null }>({ open: false, mode: "create" });

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载发布模块" description="正在读取版本与渠道信息。" />;
  }
  if (!resolvedApp) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Releases" title="发布" description="当前没有可管理的应用。" />
        <EmptyState title="暂无应用" description="请先创建应用。" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Releases"
        title="发布"
        description="版本与渠道管理，支持快捷发布与撤回。"
        action={
          <Select value={resolvedApp.appKey} onValueChange={setSelectedAppKey}>
            <SelectTrigger className="w-56"><SelectValue placeholder="选择应用" /></SelectTrigger>
            <SelectContent>
              {apps.map((a) => <SelectItem key={a.appKey} value={a.appKey}>{a.name}</SelectItem>)}
            </SelectContent>
          </Select>
        }
      />

      {/* 统计卡片 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        {[
          { label: "版本总数", value: stats?.totalVersions ?? 0, icon: Package },
          { label: "已发布", value: stats?.publishedCount ?? 0, icon: Rocket },
          { label: "渠道数", value: stats?.channelCount ?? 0, icon: GitBranch },
          { label: "平台数", value: Object.keys(stats?.platformCounts || {}).length, icon: Smartphone },
        ].map((s) => (
          <div key={s.label} className="rounded-xl border bg-card px-4 py-3">
            <div className="flex items-center gap-2 mb-1">
              <s.icon className="size-3.5 text-muted-foreground" />
              <span className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{s.label}</span>
            </div>
            <div className={cn("text-xl font-semibold", DATA_FONT)}>{s.value}</div>
          </div>
        ))}
      </div>

      {/* 主区域 Tabs */}
      <Tabs defaultValue="versions" className="space-y-4">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <TabsList>
            <TabsTrigger value="versions">版本列表</TabsTrigger>
            <TabsTrigger value="channels">渠道管理</TabsTrigger>
          </TabsList>

          <div className="flex items-center gap-2 flex-wrap">
            <Select value={statusFilter} onValueChange={(v) => { setStatusFilter(v); setPage(1); }}>
              <SelectTrigger className="w-28 h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                {statusOptions.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
              </SelectContent>
            </Select>
            <Select value={platformFilter} onValueChange={(v) => { setPlatformFilter(v); setPage(1); }}>
              <SelectTrigger className="w-28 h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部平台</SelectItem>
                {platforms.map((p) => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" onClick={() => { void versionsQuery.refetch(); void channelsQuery.refetch(); void statsQuery.refetch(); }}>
              <RefreshCw className="size-3.5" />
            </Button>
          </div>
        </div>

        {/* ── 版本列表 ── */}
        <TabsContent value="versions" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              共 {versionsQuery.data?.total ?? 0} 个版本
            </p>
            <Button size="sm" onClick={() => setVersionDialog({ open: true, mode: "create" })}>
              <Plus className="size-3.5 mr-1.5" />创建版本
            </Button>
          </div>

          {versionsQuery.isLoading ? (
            <LoadingState title="加载版本" description="" />
          ) : !versions.length ? (
            <EmptyState title="暂无版本" description="点击「创建版本」添加第一个版本。" />
          ) : (
            <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
              {versions.map((v) => (
                <VersionCard
                  key={v.id}
                  item={v}
                  appKey={resolvedApp.appKey}
                  channels={channels}
                  onEdit={() => setVersionDialog({ open: true, mode: "edit", item: v })}
                />
              ))}
            </div>
          )}

          {/* 分页 */}
          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
              <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{page} / {totalPages}</span>
              <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
            </div>
          )}
        </TabsContent>

        {/* ── 渠道管理 ── */}
        <TabsContent value="channels" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              共 {channels.length} 个渠道
            </p>
            <Button size="sm" onClick={() => setChannelDialog({ open: true, mode: "create" })}>
              <Plus className="size-3.5 mr-1.5" />创建渠道
            </Button>
          </div>

          {!channels.length ? (
            <EmptyState title="暂无渠道" description="系统会自动创建默认渠道。" />
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {channels.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  item={ch}
                  appKey={resolvedApp.appKey}
                  onEdit={() => setChannelDialog({ open: true, mode: "edit", item: ch })}
                />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>

      {/* Dialog 弹窗 */}
      {versionDialog.open && appKey && (
        <VersionDialog
          mode={versionDialog.mode}
          item={versionDialog.item}
          appKey={appKey}
          channels={channels}
          open={versionDialog.open}
          onOpenChange={(open) => setVersionDialog((s) => ({ ...s, open }))}
        />
      )}
      {channelDialog.open && appKey && (
        <ChannelDialog
          mode={channelDialog.mode}
          item={channelDialog.item}
          appKey={appKey}
          open={channelDialog.open}
          onOpenChange={(open) => setChannelDialog((s) => ({ ...s, open }))}
        />
      )}
    </div>
  );
}
