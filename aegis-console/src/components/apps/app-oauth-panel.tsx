"use client";

import { useCallback, useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Link2,
  Loader2,
  Plus,
  RotateCcw,
  Save,
  Search,
  Stethoscope,
  Trash2,
  Unlink,
  Wand2,
  X
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type {
  AppOAuthProvider,
  AppOAuthProviderPayload,
  OAuthTemplate,
  OAuthTestResult,
  TokenAuthStyle,
  UserInfoAuthStyle
} from "@/lib/api/app-oauth";
import {
  useAppOAuthBindingsQuery,
  useAppOAuthProvidersQuery,
  useDeleteAppOAuthBindingMutation,
  useDeleteAppOAuthProviderMutation,
  useOAuthTemplatesQuery,
  useReorderAppOAuthProvidersMutation,
  useSaveAppOAuthProviderMutation,
  useTestAppOAuthProviderMutation,
  useToggleAppOAuthProviderMutation
} from "@/lib/admin-hooks";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { BrandIcon, getBrandColor } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

type Props = { appKey?: string | null };

/** 编辑表单草稿：与后端 payload 一一对应，scopes / 附加参数在 UI 中用更好编辑的形态承载 */
type ProviderDraft = {
  provider: string;
  kind: AppOAuthProvider["kind"];
  displayName: string;
  icon: string;
  color: string;
  enabled: boolean;
  clientId: string;
  clientSecret: string;
  clearClientSecret: boolean;
  redirectUrl: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  scopes: string[];
  tokenAuthStyle: TokenAuthStyle;
  userInfoAuthStyle: UserInfoAuthStyle;
  profileMapping: Record<string, string>;
  extraAuthParams: Array<{ key: string; value: string }>;
  allowLogin: boolean;
  allowRegister: boolean;
  allowBind: boolean;
  remark: string;
  /** 已有渠道的密钥状态，用于提示"留空即保持不变" */
  clientSecretSet: boolean;
};

const mappingFields: Array<{ key: string; label: string; placeholder: string }> = [
  { key: "id", label: "唯一标识", placeholder: "sub / id / data.user_id" },
  { key: "nickname", label: "昵称", placeholder: "name / data.nickname" },
  { key: "avatar", label: "头像", placeholder: "avatar_url / data.avatar" },
  { key: "email", label: "邮箱", placeholder: "email" },
  { key: "unionId", label: "UnionID", placeholder: "unionid" }
];

const tokenAuthStyleOptions: Array<{ value: TokenAuthStyle; label: string; hint: string }> = [
  { value: "auto", label: "自动（推荐）", hint: "先用表单参数，被拒绝时自动改用 Basic 重试" },
  { value: "params", label: "表单参数", hint: "client_id / client_secret 放在请求体" },
  { value: "basic", label: "HTTP Basic", hint: "凭据放在 Authorization 头" }
];

const userInfoAuthStyleOptions: Array<{ value: UserInfoAuthStyle; label: string; hint: string }> = [
  { value: "header", label: "Bearer 头（推荐）", hint: "Authorization: Bearer {access_token}" },
  { value: "query", label: "查询参数", hint: "?access_token={access_token}" }
];

function emptyDraft(): ProviderDraft {
  return {
    provider: "",
    kind: "generic",
    displayName: "",
    icon: "",
    color: "",
    enabled: false,
    clientId: "",
    clientSecret: "",
    clearClientSecret: false,
    redirectUrl: "",
    authUrl: "",
    tokenUrl: "",
    userInfoUrl: "",
    scopes: [],
    tokenAuthStyle: "auto",
    userInfoAuthStyle: "header",
    profileMapping: {},
    extraAuthParams: [],
    allowLogin: true,
    allowRegister: true,
    allowBind: true,
    remark: "",
    clientSecretSet: false
  };
}

function draftFromProvider(item: AppOAuthProvider): ProviderDraft {
  return {
    provider: item.provider,
    kind: item.kind,
    displayName: item.displayName,
    icon: item.icon || "",
    color: item.color || "",
    enabled: item.enabled,
    clientId: item.clientId,
    clientSecret: "",
    clearClientSecret: false,
    redirectUrl: item.redirectUrl,
    authUrl: item.authUrl,
    tokenUrl: item.tokenUrl,
    userInfoUrl: item.userInfoUrl,
    scopes: [...(item.scopes || [])],
    tokenAuthStyle: item.tokenAuthStyle || "auto",
    userInfoAuthStyle: item.userInfoAuthStyle || "header",
    profileMapping: { ...(item.profileMapping || {}) },
    extraAuthParams: Object.entries(item.extraAuthParams || {}).map(([key, value]) => ({ key, value })),
    allowLogin: item.allowLogin,
    allowRegister: item.allowRegister,
    allowBind: item.allowBind,
    remark: item.source === "platform" ? "" : item.remark || "",
    clientSecretSet: item.clientSecretSet
  };
}

function draftFromTemplate(template: OAuthTemplate, callbackPrefix: string): ProviderDraft {
  return {
    ...emptyDraft(),
    provider: template.key,
    kind: template.kind,
    displayName: template.name,
    icon: template.icon,
    color: template.color,
    authUrl: template.authUrl,
    tokenUrl: template.tokenUrl,
    userInfoUrl: template.userInfoUrl,
    scopes: [...(template.scopes || [])],
    tokenAuthStyle: template.tokenAuthStyle || "auto",
    userInfoAuthStyle: template.userInfoAuthStyle || "header",
    redirectUrl: callbackPrefix ? `${callbackPrefix}${template.key}` : ""
  };
}

function toPayload(draft: ProviderDraft): AppOAuthProviderPayload {
  const extraAuthParams: Record<string, string> = {};
  draft.extraAuthParams.forEach(({ key, value }) => {
    const name = key.trim();
    if (name) extraAuthParams[name] = value.trim();
  });
  const profileMapping: Record<string, string> = {};
  Object.entries(draft.profileMapping).forEach(([key, value]) => {
    if (value.trim()) profileMapping[key] = value.trim();
  });
  return {
    provider: draft.provider.trim().toLowerCase(),
    kind: draft.kind,
    displayName: draft.displayName.trim(),
    icon: draft.icon.trim(),
    color: draft.color.trim(),
    enabled: draft.enabled,
    clientId: draft.clientId.trim(),
    clientSecret: draft.clientSecret.trim(),
    clearClientSecret: draft.clearClientSecret,
    redirectUrl: draft.redirectUrl.trim(),
    authUrl: draft.authUrl.trim(),
    tokenUrl: draft.tokenUrl.trim(),
    userInfoUrl: draft.userInfoUrl.trim(),
    scopes: draft.scopes,
    tokenAuthStyle: draft.tokenAuthStyle,
    userInfoAuthStyle: draft.userInfoAuthStyle,
    profileMapping,
    extraAuthParams,
    allowLogin: draft.allowLogin,
    allowRegister: draft.allowRegister,
    allowBind: draft.allowBind,
    remark: draft.remark.trim()
  };
}

async function copyText(value: string, message: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(message);
  } catch {
    toast.error("复制失败，请手动选择复制");
  }
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof ApiError ? err.message : fallback;
}

function formatDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit"
      });
}

export function AppOAuthPanel({ appKey }: Props) {
  const providersQuery = useAppOAuthProvidersQuery(appKey);
  const templatesQuery = useOAuthTemplatesQuery();
  const saveMutation = useSaveAppOAuthProviderMutation(appKey);
  const toggleMutation = useToggleAppOAuthProviderMutation(appKey);
  const deleteMutation = useDeleteAppOAuthProviderMutation(appKey);
  const reorderMutation = useReorderAppOAuthProvidersMutation(appKey);
  const testMutation = useTestAppOAuthProviderMutation(appKey);

  const providers = useMemo(() => providersQuery.data?.items ?? [], [providersQuery.data]);
  const callbackPrefix = providersQuery.data?.callbackUrlPrefix ?? templatesQuery.data?.callbackUrlPrefix ?? "";

  const [editorOpen, setEditorOpen] = useState(false);
  // 每次打开编辑抽屉自增，作为 ProviderEditor 的 key —— 让抽屉内的本地状态随开启重置，
  // 同时不影响关闭时的退出动画（关闭不改 key）
  const [editorSession, setEditorSession] = useState(0);
  const [editorMode, setEditorMode] = useState<"create" | "update">("create");
  const [draft, setDraft] = useState<ProviderDraft>(emptyDraft);
  const [marketOpen, setMarketOpen] = useState(false);
  const [testResult, setTestResult] = useState<OAuthTestResult | null>(null);
  const [pendingDelete, setPendingDelete] = useState<AppOAuthProvider | null>(null);

  const configuredKeys = useMemo(() => new Set(providers.map((item) => item.provider)), [providers]);

  const openCreate = useCallback(
    (template: OAuthTemplate) => {
      setDraft(draftFromTemplate(template, callbackPrefix));
      setEditorMode("create");
      setEditorSession((prev) => prev + 1);
      setMarketOpen(false);
      setEditorOpen(true);
    },
    [callbackPrefix]
  );

  const openEdit = useCallback((item: AppOAuthProvider) => {
    setDraft(draftFromProvider(item));
    // 平台级兜底渠道首次编辑即"接管"为应用级配置，因此按新建提交
    setEditorMode(item.source === "platform" ? "create" : "update");
    setEditorSession((prev) => prev + 1);
    setEditorOpen(true);
  }, []);

  const handleSave = useCallback(async () => {
    const payload = toPayload(draft);
    if (!payload.provider) {
      toast.error("请填写渠道标识");
      return;
    }
    try {
      await saveMutation.mutateAsync({ mode: editorMode, payload });
      toast.success(editorMode === "create" ? "渠道已创建" : "渠道已更新");
      setEditorOpen(false);
    } catch (err) {
      toast.error(errorMessage(err, "保存失败"));
    }
  }, [draft, editorMode, saveMutation]);

  const handleToggle = useCallback(
    async (item: AppOAuthProvider, enabled: boolean) => {
      try {
        await toggleMutation.mutateAsync({ provider: item.provider, enabled });
        toast.success(enabled ? `${item.displayName} 已启用` : `${item.displayName} 已停用`);
      } catch (err) {
        toast.error(errorMessage(err, "操作失败"));
      }
    },
    [toggleMutation]
  );

  const handleMove = useCallback(
    async (index: number, delta: number) => {
      const next = [...providers];
      const target = index + delta;
      if (target < 0 || target >= next.length) return;
      [next[index], next[target]] = [next[target], next[index]];
      try {
        await reorderMutation.mutateAsync(next.map((item) => item.provider));
      } catch (err) {
        toast.error(errorMessage(err, "排序失败"));
      }
    },
    [providers, reorderMutation]
  );

  const handleTest = useCallback(
    async (provider: string) => {
      try {
        const result = await testMutation.mutateAsync(provider);
        setTestResult(result);
      } catch (err) {
        toast.error(errorMessage(err, "自检失败"));
      }
    },
    [testMutation]
  );

  const handleDelete = useCallback(async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.provider);
      toast.success(`${pendingDelete.displayName} 已删除`);
      setPendingDelete(null);
    } catch (err) {
      toast.error(errorMessage(err, "删除失败"));
    }
  }, [deleteMutation, pendingDelete]);

  if (!appKey) {
    return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;
  }

  return (
    <Tabs defaultValue="providers" className="space-y-4">
      <TabsList className="w-fit">
        <TabsTrigger value="providers">
          <Link2 className="size-4" />
          登录渠道
        </TabsTrigger>
        <TabsTrigger value="bindings">
          <Unlink className="size-4" />
          绑定记录
        </TabsTrigger>
      </TabsList>

      <TabsContent value="providers" className="space-y-4">
        <CallbackHint prefix={callbackPrefix} />

        <div className="flex items-center justify-between gap-2">
          <div className="text-xs text-muted-foreground">
            已配置 {providers.length} 个渠道 · 启用 {providers.filter((item) => item.enabled).length} 个
          </div>
          <Button size="sm" className="h-8 gap-1 text-xs" onClick={() => setMarketOpen(true)}>
            <Plus className="size-3" />
            添加渠道
          </Button>
        </div>

        {providersQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-20 w-full" />
          </div>
        ) : providers.length === 0 ? (
          <EmptyProviders onAdd={() => setMarketOpen(true)} />
        ) : (
          <div className="space-y-2">
            {providers.map((item, index) => (
              <ProviderCard
                key={item.provider}
                item={item}
                index={index}
                total={providers.length}
                busy={toggleMutation.isPending || reorderMutation.isPending}
                testing={testMutation.isPending}
                onToggle={handleToggle}
                onEdit={openEdit}
                onMove={handleMove}
                onTest={handleTest}
                onDelete={setPendingDelete}
                callbackPrefix={callbackPrefix}
              />
            ))}
          </div>
        )}
      </TabsContent>

      <TabsContent value="bindings">
        <BindingsSection appKey={appKey} providers={providers} />
      </TabsContent>

      <ProviderMarket
        open={marketOpen}
        onOpenChange={setMarketOpen}
        templates={templatesQuery.data?.items ?? []}
        loading={templatesQuery.isLoading}
        configuredKeys={configuredKeys}
        onPick={openCreate}
      />

      <ProviderEditor
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        mode={editorMode}
        draft={draft}
        setDraft={setDraft}
        callbackPrefix={callbackPrefix}
        saving={saveMutation.isPending}
        onSave={handleSave}
      />

      <TestResultDialog result={testResult} onOpenChange={() => setTestResult(null)} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除渠道「{pendingDelete?.displayName}」？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该渠道立即从登录页消失。已产生的 {pendingDelete?.bindings ?? 0} 条用户绑定会保留，
              重新配置同名渠道后可继续使用。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? "删除中..." : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Tabs>
  );
}

/** 回调地址提示条：管理员最容易填错的一项，直接给出可复制的完整前缀 */
function CallbackHint({ prefix }: { prefix: string }) {
  if (!prefix) return null;
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-sky-200 bg-sky-50/60 px-3 py-2 text-xs dark:border-sky-900/60 dark:bg-sky-950/30">
      <span className="font-medium text-sky-700 dark:text-sky-300">回调地址</span>
      <code className="truncate rounded bg-background/70 px-1.5 py-0.5 font-mono text-[11px]">
        {prefix}
        <span className="text-muted-foreground">{"{渠道标识}"}</span>
      </code>
      <span className="text-muted-foreground">需原样登记到服务商开发者后台</span>
      <Button
        size="sm"
        variant="ghost"
        className="ml-auto h-6 gap-1 px-2 text-[11px]"
        onClick={() => void copyText(prefix, "回调地址前缀已复制")}
      >
        <Copy className="size-3" />
        复制前缀
      </Button>
    </div>
  );
}

function EmptyProviders({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="rounded-lg border border-dashed py-12 text-center">
      <Link2 className="mx-auto size-8 text-muted-foreground/60" />
      <div className="mt-3 text-sm font-medium">还没有配置第三方登录</div>
      <p className="mx-auto mt-1 max-w-md text-xs text-muted-foreground">
        从渠道市场挑一个模板，端点与 scope 会自动填好，只需再补 ClientID 与 ClientSecret 即可上线。
      </p>
      <Button size="sm" className="mt-4 h-8 gap-1 text-xs" onClick={onAdd}>
        <Plus className="size-3" />
        添加渠道
      </Button>
    </div>
  );
}

function ProviderCard({
  item,
  index,
  total,
  busy,
  testing,
  onToggle,
  onEdit,
  onMove,
  onTest,
  onDelete,
  callbackPrefix
}: {
  item: AppOAuthProvider;
  index: number;
  total: number;
  busy: boolean;
  testing: boolean;
  onToggle: (item: AppOAuthProvider, enabled: boolean) => void;
  onEdit: (item: AppOAuthProvider) => void;
  onMove: (index: number, delta: number) => void;
  onTest: (provider: string) => void;
  onDelete: (item: AppOAuthProvider) => void;
  callbackPrefix: string;
}) {
  const isPlatform = item.source === "platform";
  // 管理员自定义色优先，其次用 Simple Icons 的官方品牌色，最后中性灰
  const brandColor = item.color || getBrandColor(item.icon) || "#64748B";
  return (
    <div className="rounded-lg border bg-card p-3 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-3">
        <div
          className="flex size-9 shrink-0 items-center justify-center rounded-full"
          style={{ backgroundColor: `${brandColor}1A`, color: brandColor }}
        >
          <BrandIcon slug={item.icon} className="size-[18px]" title={item.displayName} />
        </div>

        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-sm font-medium">{item.displayName}</span>
            <code className="rounded bg-muted px-1 py-px font-mono text-[10px] text-muted-foreground">
              {item.provider}
            </code>
            {item.ready ? (
              <Badge variant="success" size="sm">
                已就绪
              </Badge>
            ) : (
              <Badge variant="warning" size="sm">
                待完善
              </Badge>
            )}
            {isPlatform && (
              <Badge variant="info" size="sm">
                平台默认
              </Badge>
            )}
            {item.bindings > 0 && (
              <Badge variant="outline" size="sm">
                {item.bindings} 人已绑定
              </Badge>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-1">
            <CapabilityChip active={item.allowLogin} label="登录" />
            <CapabilityChip active={item.allowRegister} label="自动注册" />
            <CapabilityChip active={item.allowBind} label="账号绑定" />
          </div>

          {item.issues && item.issues.length > 0 && (
            <div className="flex items-start gap-1 text-[11px] text-amber-600 dark:text-amber-400">
              <AlertTriangle className="mt-px size-3 shrink-0" />
              <span>{item.issues.join("；")}</span>
            </div>
          )}
          {item.warnings && item.warnings.length > 0 && (
            <div className="text-[11px] text-muted-foreground">{item.warnings.join("；")}</div>
          )}
          {isPlatform && (
            <div className="text-[11px] text-muted-foreground">
              当前沿用平台级 .env 配置，编辑保存后即成为该应用独立的配置
            </div>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-0.5">
          <Button
            size="sm"
            variant="ghost"
            className="size-7 p-0"
            title="上移"
            disabled={index === 0 || busy}
            onClick={() => onMove(index, -1)}
          >
            <ArrowUp className="size-3" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="size-7 p-0"
            title="下移"
            disabled={index === total - 1 || busy}
            onClick={() => onMove(index, 1)}
          >
            <ArrowDown className="size-3" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="size-7 p-0"
            title="复制该渠道的回调地址"
            onClick={() => void copyText(item.redirectUrl || `${callbackPrefix}${item.provider}`, "回调地址已复制")}
          >
            <Copy className="size-3" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="size-7 p-0"
            title="连通性自检"
            disabled={testing}
            onClick={() => onTest(item.provider)}
          >
            {testing ? <Loader2 className="size-3 animate-spin" /> : <Stethoscope className="size-3" />}
          </Button>
          <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => onEdit(item)}>
            配置
          </Button>
          {!isPlatform && (
            <Button
              size="sm"
              variant="ghost"
              className="size-7 p-0 text-destructive hover:text-destructive"
              title="删除渠道"
              onClick={() => onDelete(item)}
            >
              <Trash2 className="size-3" />
            </Button>
          )}
          <Switch
            checked={item.enabled}
            disabled={busy}
            onCheckedChange={(value) => onToggle(item, value)}
            aria-label={`${item.displayName} 启用开关`}
          />
        </div>
      </div>
    </div>
  );
}

function CapabilityChip({ active, label }: { active: boolean; label: string }) {
  return (
    <span
      className={`rounded px-1.5 py-px text-[10px] ${
        active ? "bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300" : "bg-muted text-muted-foreground line-through"
      }`}
    >
      {label}
    </span>
  );
}

/** 渠道市场：按分类展示内置模板，点选即预填端点与 scope */
function ProviderMarket({
  open,
  onOpenChange,
  templates,
  loading,
  configuredKeys,
  onPick
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  templates: OAuthTemplate[];
  loading: boolean;
  configuredKeys: Set<string>;
  onPick: (template: OAuthTemplate) => void;
}) {
  const grouped = useMemo(() => {
    const map = new Map<string, OAuthTemplate[]>();
    templates.forEach((item) => {
      const list = map.get(item.category) || [];
      list.push(item);
      map.set(item.category, list);
    });
    return Array.from(map.entries());
  }, [templates]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>添加第三方登录渠道</DialogTitle>
          <DialogDescription>
            选择模板后端点、scope、品牌信息会自动填好；只需补上服务商给的 ClientID 与 ClientSecret。
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[60vh] pr-3">
          {loading ? (
            <div className="grid gap-2 sm:grid-cols-2">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : (
            <div className="space-y-4">
              {grouped.map(([category, items]) => (
                <div key={category} className="space-y-2">
                  <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{category}</div>
                  <div className="grid gap-2 sm:grid-cols-2">
                    {items.map((template) => {
                      const configured = configuredKeys.has(template.key);
                      return (
                        <button
                          key={template.key}
                          type="button"
                          onClick={() => onPick(template)}
                          className="group flex items-start gap-2.5 rounded-lg border p-2.5 text-left transition-colors hover:border-foreground/30 hover:bg-accent/40"
                        >
                          <span
                            className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full"
                            style={{
                              backgroundColor: `${template.color || getBrandColor(template.icon) || "#64748B"}1A`,
                              color: template.color || getBrandColor(template.icon) || "#64748B"
                            }}
                          >
                            <BrandIcon slug={template.icon} className="size-4" title={template.name} />
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-1.5">
                              <span className="text-sm font-medium">{template.name}</span>
                              {configured && (
                                <Badge variant="secondary" size="sm">
                                  已配置
                                </Badge>
                              )}
                              {template.requiresEndpoints && (
                                <Badge variant="outline" size="sm">
                                  需填端点
                                </Badge>
                              )}
                            </span>
                            <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                              {template.description}
                            </span>
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

/** 渠道配置抽屉：基础信息与凭据常驻，端点与高级项收进折叠区 */
function ProviderEditor({
  open,
  onOpenChange,
  mode,
  draft,
  setDraft,
  callbackPrefix,
  saving,
  onSave
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mode: "create" | "update";
  draft: ProviderDraft;
  setDraft: (updater: (prev: ProviderDraft) => ProviderDraft) => void;
  callbackPrefix: string;
  saving: boolean;
  onSave: () => void;
}) {
  // 每次打开抽屉由父级换 key 重挂载，这两项本地状态天然回到初值
  const [showSecret, setShowSecret] = useState(false);
  const [scopeInput, setScopeInput] = useState("");

  const set = <K extends keyof ProviderDraft>(key: K, value: ProviderDraft[K]) =>
    setDraft((prev) => ({ ...prev, [key]: value }));

  const suggestedRedirect = callbackPrefix ? `${callbackPrefix}${draft.provider.trim().toLowerCase()}` : "";
  const previewColor = draft.color || getBrandColor(draft.icon) || "#64748B";

  const addScope = () => {
    const value = scopeInput.trim();
    if (!value) return;
    if (draft.scopes.includes(value)) {
      setScopeInput("");
      return;
    }
    set("scopes", [...draft.scopes, value]);
    setScopeInput("");
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle>{mode === "create" ? "添加渠道" : `配置「${draft.displayName || draft.provider}」`}</SheetTitle>
          <SheetDescription>
            凭据加密存储，保存后不再回显；留空即保持原值不变。
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="space-y-5 px-5 py-4">
            {/* 基础信息 */}
            <section className="space-y-3">
              <SectionTitle title="基础信息" />
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="展示名称" hint="登录页按钮上显示的文字">
                  <Input
                    className="h-8 text-sm"
                    value={draft.displayName}
                    placeholder="微信登录"
                    onChange={(event) => set("displayName", event.target.value)}
                  />
                </Field>
                <Field label="渠道标识" hint={mode === "update" ? "创建后不可修改" : "小写字母/数字/-/_，2-32 位"}>
                  <Input
                    className="h-8 font-mono text-sm"
                    value={draft.provider}
                    disabled={mode === "update"}
                    placeholder="wechat"
                    onChange={(event) => set("provider", event.target.value.toLowerCase())}
                  />
                </Field>
                <Field label="图标" hint="Simple Icons 的 slug，如 github / wechat / openid">
                  <div className="flex items-center gap-2">
                    <span
                      className="flex size-8 shrink-0 items-center justify-center rounded-full"
                      style={{
                        backgroundColor: `${previewColor}1A`,
                        color: previewColor
                      }}
                    >
                      <BrandIcon slug={draft.icon} className="size-4" />
                    </span>
                    <Input
                      className="h-8 flex-1 font-mono text-sm"
                      value={draft.icon}
                      placeholder="github"
                      onChange={(event) => set("icon", event.target.value.trim().toLowerCase())}
                    />
                  </div>
                </Field>
                <Field label="品牌色" hint="留空则使用 Simple Icons 的官方品牌色">
                  <div className="flex items-center gap-2">
                    <Input
                      type="color"
                      className="h-8 w-12 cursor-pointer p-1"
                      value={previewColor}
                      onChange={(event) => set("color", event.target.value)}
                    />
                    <Input
                      className="h-8 flex-1 font-mono text-sm"
                      value={draft.color}
                      placeholder="#07C160"
                      onChange={(event) => set("color", event.target.value)}
                    />
                  </div>
                </Field>
                <Field label="协议适配器" hint="决定 token 与用户信息的解析方式">
                  <Select value={draft.kind} onValueChange={(value) => set("kind", value as ProviderDraft["kind"])}>
                    <SelectTrigger className="h-8 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="generic">标准 OAuth2 / OIDC</SelectItem>
                      <SelectItem value="wechat">微信</SelectItem>
                      <SelectItem value="qq">QQ 互联</SelectItem>
                      <SelectItem value="weibo">微博</SelectItem>
                      <SelectItem value="github">GitHub</SelectItem>
                      <SelectItem value="microsoft">Microsoft</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              </div>
            </section>

            <Separator />

            {/* 凭据 */}
            <section className="space-y-3">
              <SectionTitle title="应用凭据" description="在服务商开发者后台创建应用后获得" />
              <Field label="ClientID / AppID">
                <Input
                  className="h-8 font-mono text-sm"
                  value={draft.clientId}
                  placeholder="wx0000000000000000"
                  onChange={(event) => set("clientId", event.target.value)}
                />
              </Field>
              <Field
                label="ClientSecret / AppSecret"
                hint={draft.clientSecretSet ? "已配置，留空即保持不变" : "必填，加密存储"}
              >
                <div className="flex items-center gap-1.5">
                  <Input
                    className="h-8 flex-1 font-mono text-sm"
                    type={showSecret ? "text" : "password"}
                    value={draft.clientSecret}
                    placeholder={draft.clientSecretSet ? "••••••••（留空保持不变）" : "粘贴 Secret"}
                    onChange={(event) => {
                      set("clientSecret", event.target.value);
                      if (event.target.value) set("clearClientSecret", false);
                    }}
                  />
                  <Button
                    size="sm"
                    variant="ghost"
                    className="size-8 shrink-0 p-0"
                    onClick={() => setShowSecret((prev) => !prev)}
                    title={showSecret ? "隐藏" : "显示"}
                  >
                    {showSecret ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  </Button>
                  {draft.clientSecretSet && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-8 shrink-0 p-0 text-destructive hover:text-destructive"
                      title="清空已保存的密钥"
                      onClick={() => {
                        set("clientSecret", "");
                        set("clearClientSecret", true);
                      }}
                    >
                      <X className="size-3.5" />
                    </Button>
                  )}
                </div>
                {draft.clearClientSecret && (
                  <p className="text-[11px] text-destructive">保存后将清空已存储的密钥，该渠道会自动变为待完善状态</p>
                )}
              </Field>
              <Field label="回调地址" hint="必须与服务商后台登记的地址完全一致">
                <div className="flex items-center gap-1.5">
                  <Input
                    className="h-8 flex-1 font-mono text-sm"
                    value={draft.redirectUrl}
                    placeholder={suggestedRedirect || "https://example.com/api/auth/oauth2/callback?provider=wechat"}
                    onChange={(event) => set("redirectUrl", event.target.value)}
                  />
                  {suggestedRedirect && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-8 shrink-0 gap-1 px-2 text-[11px]"
                      title="填入本站默认回调地址"
                      onClick={() => set("redirectUrl", suggestedRedirect)}
                    >
                      <Wand2 className="size-3" />
                      自动填入
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    className="size-8 shrink-0 p-0"
                    title="复制"
                    disabled={!draft.redirectUrl}
                    onClick={() => void copyText(draft.redirectUrl, "回调地址已复制")}
                  >
                    <Copy className="size-3.5" />
                  </Button>
                </div>
              </Field>
            </section>

            <Separator />

            {/* 行为策略 */}
            <section className="space-y-2">
              <SectionTitle title="行为策略" />
              <ToggleRow
                label="允许直接登录"
                description="关闭后该渠道只出现在账号绑定入口，不在登录页展示"
                checked={draft.allowLogin}
                onChange={(value) => set("allowLogin", value)}
              />
              <ToggleRow
                label="允许自动注册"
                description="首次使用该渠道且未绑定过的用户，自动创建账号；关闭则必须先用已有账号绑定"
                checked={draft.allowRegister}
                onChange={(value) => set("allowRegister", value)}
              />
              <ToggleRow
                label="允许账号绑定"
                description="已登录用户可在个人中心把该渠道绑定到当前账号"
                checked={draft.allowBind}
                onChange={(value) => set("allowBind", value)}
              />
              <ToggleRow
                label="启用该渠道"
                description="配置完整时才能启用；缺项会在保存时明确提示"
                checked={draft.enabled}
                onChange={(value) => set("enabled", value)}
              />
            </section>

            <Separator />

            <Accordion type="multiple" className="w-full">
              <AccordionItem value="endpoints">
                <AccordionTrigger className="text-xs">协议端点与授权范围</AccordionTrigger>
                <AccordionContent className="space-y-3 pt-1">
                  <Field label="授权端点 authorize">
                    <Input
                      className="h-8 font-mono text-sm"
                      value={draft.authUrl}
                      onChange={(event) => set("authUrl", event.target.value)}
                    />
                  </Field>
                  <Field label="令牌端点 token">
                    <Input
                      className="h-8 font-mono text-sm"
                      value={draft.tokenUrl}
                      onChange={(event) => set("tokenUrl", event.target.value)}
                    />
                  </Field>
                  <Field label="用户信息端点 userinfo">
                    <Input
                      className="h-8 font-mono text-sm"
                      value={draft.userInfoUrl}
                      onChange={(event) => set("userInfoUrl", event.target.value)}
                    />
                  </Field>
                  <Field label="授权范围 scope" hint="回车添加，点击标签删除">
                    <div className="space-y-1.5">
                      <Input
                        className="h-8 font-mono text-sm"
                        value={scopeInput}
                        placeholder="openid"
                        onChange={(event) => setScopeInput(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.preventDefault();
                            addScope();
                          }
                        }}
                      />
                      {draft.scopes.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                          {draft.scopes.map((scope) => (
                            <button
                              key={scope}
                              type="button"
                              className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] hover:bg-destructive/10 hover:text-destructive"
                              onClick={() => set("scopes", draft.scopes.filter((item) => item !== scope))}
                            >
                              {scope}
                              <X className="size-2.5" />
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  </Field>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field
                      label="令牌端点凭据方式"
                      hint={tokenAuthStyleOptions.find((item) => item.value === draft.tokenAuthStyle)?.hint}
                    >
                      <Select
                        value={draft.tokenAuthStyle}
                        onValueChange={(value) => set("tokenAuthStyle", value as TokenAuthStyle)}
                      >
                        <SelectTrigger className="h-8 text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {tokenAuthStyleOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                    <Field
                      label="用户信息凭据方式"
                      hint={userInfoAuthStyleOptions.find((item) => item.value === draft.userInfoAuthStyle)?.hint}
                    >
                      <Select
                        value={draft.userInfoAuthStyle}
                        onValueChange={(value) => set("userInfoAuthStyle", value as UserInfoAuthStyle)}
                      >
                        <SelectTrigger className="h-8 text-sm">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {userInfoAuthStyleOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                  </div>
                </AccordionContent>
              </AccordionItem>

              <AccordionItem value="mapping">
                <AccordionTrigger className="text-xs">用户字段映射</AccordionTrigger>
                <AccordionContent className="space-y-3 pt-1">
                  <p className="text-[11px] text-muted-foreground">
                    留空时使用内置识别规则；返回结构非标准时可填 JSON 路径（支持 data.user.id 这样的点号写法）。
                  </p>
                  {mappingFields.map((field) => (
                    <Field key={field.key} label={field.label}>
                      <Input
                        className="h-8 font-mono text-sm"
                        value={draft.profileMapping[field.key] || ""}
                        placeholder={field.placeholder}
                        onChange={(event) =>
                          set("profileMapping", { ...draft.profileMapping, [field.key]: event.target.value })
                        }
                      />
                    </Field>
                  ))}
                </AccordionContent>
              </AccordionItem>

              <AccordionItem value="advanced">
                <AccordionTrigger className="text-xs">附加授权参数与备注</AccordionTrigger>
                <AccordionContent className="space-y-3 pt-1">
                  <p className="text-[11px] text-muted-foreground">
                    追加到授权链接的额外参数，例如 Google 的 access_type=offline、prompt=consent。
                  </p>
                  <div className="space-y-1.5">
                    {draft.extraAuthParams.map((pair, index) => (
                      <div key={index} className="flex items-center gap-1.5">
                        <Input
                          className="h-8 flex-1 font-mono text-sm"
                          placeholder="参数名"
                          value={pair.key}
                          onChange={(event) => {
                            const next = [...draft.extraAuthParams];
                            next[index] = { ...next[index], key: event.target.value };
                            set("extraAuthParams", next);
                          }}
                        />
                        <Input
                          className="h-8 flex-1 font-mono text-sm"
                          placeholder="参数值"
                          value={pair.value}
                          onChange={(event) => {
                            const next = [...draft.extraAuthParams];
                            next[index] = { ...next[index], value: event.target.value };
                            set("extraAuthParams", next);
                          }}
                        />
                        <Button
                          size="sm"
                          variant="ghost"
                          className="size-8 shrink-0 p-0 text-destructive hover:text-destructive"
                          onClick={() =>
                            set("extraAuthParams", draft.extraAuthParams.filter((_, position) => position !== index))
                          }
                        >
                          <Trash2 className="size-3" />
                        </Button>
                      </div>
                    ))}
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 gap-1 text-[11px]"
                      onClick={() => set("extraAuthParams", [...draft.extraAuthParams, { key: "", value: "" }])}
                    >
                      <Plus className="size-3" />
                      添加参数
                    </Button>
                  </div>
                  <Field label="备注">
                    <Input
                      className="h-8 text-sm"
                      value={draft.remark}
                      placeholder="例如：由张三在服务商后台申请，账号 xxx"
                      onChange={(event) => set("remark", event.target.value)}
                    />
                  </Field>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </div>
        </ScrollArea>

        <div className="flex items-center justify-end gap-2 border-t px-5 py-3">
          <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" className="h-8 gap-1 text-xs" disabled={saving} onClick={onSave}>
            {saving ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
            {saving ? "保存中..." : "保存配置"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function TestResultDialog({
  result,
  onOpenChange
}: {
  result: OAuthTestResult | null;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={Boolean(result)} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            渠道自检
            {result?.ready ? (
              <Badge variant="success" size="sm">
                配置完整
              </Badge>
            ) : (
              <Badge variant="warning" size="sm">
                待完善
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription>检查配置完整性与端点可达性，不会真正发起授权。</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {result?.issues && result.issues.length > 0 && (
            <div className="rounded-md border border-amber-200 bg-amber-50/60 p-2 text-[11px] text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
              {result.issues.map((issue) => (
                <div key={issue}>· {issue}</div>
              ))}
            </div>
          )}
          {result?.warnings && result.warnings.length > 0 && (
            <div className="rounded-md border bg-muted/40 p-2 text-[11px] text-muted-foreground">
              {result.warnings.map((warning) => (
                <div key={warning}>· {warning}</div>
              ))}
            </div>
          )}

          <div className="space-y-1.5">
            {result?.endpoints.map((endpoint) => (
              <div key={endpoint.name} className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs">
                {endpoint.reachable ? (
                  <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
                ) : (
                  <AlertTriangle className="size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
                )}
                <span className="shrink-0 font-medium">{endpoint.name}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-foreground">
                  {endpoint.url}
                </span>
                <span className="shrink-0 text-[10px] text-muted-foreground">
                  {endpoint.reachable ? `HTTP ${endpoint.status} · ${endpoint.latencyMs}ms` : endpoint.message}
                </span>
              </div>
            ))}
          </div>

          {result?.authorizeUrl && (
            <div className="space-y-1">
              <div className="text-[10px] uppercase tracking-wider text-muted-foreground">示例授权链接</div>
              <div className="flex items-center gap-1.5">
                <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-[10px]">
                  {result.authorizeUrl}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  className="size-7 shrink-0 p-0"
                  title="复制"
                  onClick={() => void copyText(result.authorizeUrl as string, "授权链接已复制")}
                >
                  <Copy className="size-3" />
                </Button>
                <Button size="sm" variant="ghost" className="size-7 shrink-0 p-0" title="在新标签页打开" asChild>
                  <a href={result.authorizeUrl} target="_blank" rel="noreferrer">
                    <ExternalLink className="size-3" />
                  </a>
                </Button>
              </div>
              <p className="text-[10px] text-muted-foreground">
                点开后若能正常显示服务商授权页，说明 ClientID 与回调地址登记无误。
              </p>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** 绑定记录：查看谁绑了哪个渠道，并支持管理员强制解绑 */
function BindingsSection({ appKey, providers }: { appKey: string; providers: AppOAuthProvider[] }) {
  const [keyword, setKeyword] = useState("");
  const [searchTerm, setSearchTerm] = useState("");
  const [provider, setProvider] = useState("all");
  const [page, setPage] = useState(1);
  const [pendingUnbind, setPendingUnbind] = useState<{ userId: number; provider: string; account?: string } | null>(null);

  const query = useAppOAuthBindingsQuery(appKey, {
    provider: provider === "all" ? undefined : provider,
    keyword: searchTerm || undefined,
    page,
    pageSize: 20
  });
  const unbindMutation = useDeleteAppOAuthBindingMutation(appKey);

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));

  const handleUnbind = useCallback(async () => {
    if (!pendingUnbind) return;
    try {
      await unbindMutation.mutateAsync({
        userId: pendingUnbind.userId,
        provider: pendingUnbind.provider,
        force: true
      });
      toast.success("已解绑");
      setPendingUnbind(null);
    } catch (err) {
      toast.error(errorMessage(err, "解绑失败"));
    }
  }, [pendingUnbind, unbindMutation]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 sm:max-w-xs">
          <Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-7 text-sm"
            placeholder="搜索账号 / 昵称 / 第三方 ID"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                setSearchTerm(keyword.trim());
                setPage(1);
              }
            }}
          />
        </div>
        <Select
          value={provider}
          onValueChange={(value) => {
            setProvider(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-40 text-xs">
            <SelectValue placeholder="全部渠道" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部渠道</SelectItem>
            {providers.map((item) => (
              <SelectItem key={item.provider} value={item.provider}>
                {item.displayName}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          variant="outline"
          className="h-8 gap-1 text-xs"
          onClick={() => {
            setSearchTerm(keyword.trim());
            setPage(1);
          }}
        >
          <Search className="size-3" />
          查询
        </Button>
        <Button
          size="sm"
          variant="ghost"
          className="h-8 gap-1 text-xs"
          onClick={() => {
            setKeyword("");
            setSearchTerm("");
            setProvider("all");
            setPage(1);
          }}
        >
          <RotateCcw className="size-3" />
          重置
        </Button>
        <span className="ml-auto text-xs text-muted-foreground">共 {total} 条</span>
      </div>

      {query.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-dashed py-10 text-center text-xs text-muted-foreground">
          暂无绑定记录
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-xs">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left font-medium">用户</th>
                <th className="px-3 py-2 text-left font-medium">渠道</th>
                <th className="px-3 py-2 text-left font-medium">第三方账号</th>
                <th className="px-3 py-2 text-left font-medium">绑定时间</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="border-t">
                  <td className="px-3 py-2">
                    <div className="font-medium">{item.account || `#${item.userId}`}</div>
                    <div className="font-mono text-[10px] text-muted-foreground">ID {item.userId}</div>
                  </td>
                  <td className="px-3 py-2">
                    <span className="flex items-center gap-1.5">
                      <BrandIcon
                        slug={item.icon}
                        className="size-3.5 shrink-0"
                        title={item.displayName || item.provider}
                      />
                      {item.displayName || item.provider}
                    </span>
                  </td>
                  <td className="px-3 py-2">
                    <div>{item.nickname || "—"}</div>
                    <div className="max-w-[220px] truncate font-mono text-[10px] text-muted-foreground">
                      {item.providerUserId}
                    </div>
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">{formatDate(item.createdAt)}</td>
                  <td className="px-3 py-2 text-right">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-6 gap-1 px-2 text-[11px] text-destructive hover:text-destructive"
                      onClick={() =>
                        setPendingUnbind({ userId: item.userId, provider: item.provider, account: item.account })
                      }
                    >
                      <Unlink className="size-3" />
                      解绑
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-end gap-2 text-xs">
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-[11px]"
            disabled={page <= 1}
            onClick={() => setPage((prev) => Math.max(1, prev - 1))}
          >
            上一页
          </Button>
          <span className="text-muted-foreground">
            {page} / {totalPages}
          </span>
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-[11px]"
            disabled={page >= totalPages}
            onClick={() => setPage((prev) => prev + 1)}
          >
            下一页
          </Button>
        </div>
      )}

      <AlertDialog open={Boolean(pendingUnbind)} onOpenChange={(open) => !open && setPendingUnbind(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>解绑「{pendingUnbind?.account || pendingUnbind?.userId}」的第三方账号？</AlertDialogTitle>
            <AlertDialogDescription>
              解绑后该用户将无法再用此第三方账号登录。若该账号没有设置密码且这是唯一登录方式，解绑会导致其无法登录。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleUnbind} disabled={unbindMutation.isPending}>
              {unbindMutation.isPending ? "解绑中..." : "确认解绑"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SectionTitle({ title, description }: { title: string; description?: string }) {
  return (
    <div>
      <div className="text-xs font-semibold">{title}</div>
      {description && <p className="mt-0.5 text-[11px] text-muted-foreground">{description}</p>}
    </div>
  );
}

function Field({
  label,
  hint,
  children
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{label}</Label>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
    </div>
  );
}

function ToggleRow({
  label,
  description,
  checked,
  onChange
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-md border px-3 py-2">
      <div className="min-w-0">
        <div className="text-xs font-medium">{label}</div>
        <p className="mt-0.5 text-[11px] leading-snug text-muted-foreground">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} aria-label={label} />
    </div>
  );
}
