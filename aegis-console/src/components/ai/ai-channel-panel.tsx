"use client";

import { FormEvent, useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  BookOpenText,
  Plus,
  Save,
  Server,
  Share2,
  Sparkles,
  Trash2,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { AIConfig, AIMCPServer, AIProviderMeta, AIResolution, AISkill } from "@/lib/api/ai";
import {
  useAIChannelQuery,
  useAIConfigsQuery,
  useAIMCPServersQuery,
  useAIProviderCatalogQuery,
  useAISkillsQuery,
  useDeleteAIConfigMutation,
  useDeleteAIMCPServerMutation,
  useDeleteAISkillMutation,
  useSaveAIConfigMutation,
  useSaveAIMCPServerMutation,
  useSaveAISkillMutation,
  useTestAIConfigMutation,
  useTestAIMCPServerMutation,
  type AIScope
} from "@/lib/ai-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { AIBrandBadge } from "./ai-brand-icon";
import { AI_CAPABILITY_LABELS, AIProviderFields, buildDefaultAISettings } from "./ai-provider-fields";

/** 连通性测试的结论。失败原因可能来自上游，防御性截断，别让一段长文撑爆弹窗。 */
type TestOutcome = { ok: boolean; text: string };

function clipText(text: string, limit = 500) {
  return text.length > limit ? `${text.slice(0, limit)}…` : text;
}

function TestOutcomeNote({ outcome }: { outcome: TestOutcome }) {
  return (
    <p
      className={cn(
        "max-h-40 overflow-y-auto whitespace-pre-wrap break-all rounded-lg border px-3 py-2 text-xs leading-relaxed",
        outcome.ok ? "bg-muted/40" : "border-destructive/40 bg-destructive/5 text-destructive"
      )}
    >
      {outcome.text}
    </p>
  );
}

/**
 * AI 通道面板。平台级与应用级**共用同一个组件**，只差一个 scope ——
 * 两边的后端也是同一批方法，各写一份 UI 迟早漂移成两套不一样的能力。
 *
 * 三个页签：通道配置（供应商 + 密钥 + 型号）、技能（提示词包）、MCP 服务器。
 */
export function AIChannelPanel({ scope }: { scope: AIScope }) {
  const catalogQuery = useAIProviderCatalogQuery(scope);
  const configsQuery = useAIConfigsQuery(scope);
  const channelQuery = useAIChannelQuery(scope);

  const providers = catalogQuery.data?.providers ?? [];
  const configs = configsQuery.data ?? [];
  const isPlatform = scope.kind === "platform";

  if (scope.kind === "app" && !scope.appKey) {
    return <EmptyState title="请先选择应用" description="选择应用后可管理它的 AI 通道。" />;
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">{isPlatform ? "平台 AI 通道" : "AI 通道"}</h2>
          <p className="text-sm text-muted-foreground">
            {isPlatform
              ? "平台自身的 AI 出口；打开「共享」后还能作为应用的兜底通道。"
              : "该应用的 AI 助手、aegis.ai 脚本调用与 OpenAI/Anthropic 兼容网关都从这里出。"}
          </p>
        </div>
      </div>

      <ChannelChainBand
        isPlatform={isPlatform}
        chain={channelQuery.data ?? []}
        loading={channelQuery.isLoading}
        providers={providers}
      />

      <Tabs defaultValue="configs">
        <TabsList>
          <TabsTrigger value="configs">
            <Zap className="size-3.5" /> 通道配置
          </TabsTrigger>
          <TabsTrigger value="skills">
            <BookOpenText className="size-3.5" /> 技能
          </TabsTrigger>
          <TabsTrigger value="mcp">
            <Server className="size-3.5" /> MCP 服务器
          </TabsTrigger>
        </TabsList>

        <TabsContent value="configs" className="mt-4">
          <ConfigsSection scope={scope} providers={providers} configs={configs} />
        </TabsContent>
        <TabsContent value="skills" className="mt-4">
          <SkillsSection scope={scope} />
        </TabsContent>
        <TabsContent value="mcp" className="mt-4">
          <MCPSection scope={scope} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

/**
 * 通道链路带。「这次调用会先试哪条、失败了落到谁」是这一页最该一眼看到的结论，
 * 应用借用平台共享通道时必须把「花的是平台的钱」这件事说出来。
 */
function ChannelChainBand({
  isPlatform,
  chain,
  loading,
  providers
}: {
  isPlatform: boolean;
  chain: AIResolution[];
  loading: boolean;
  providers: AIProviderMeta[];
}) {
  if (loading) {
    return <div className="h-16 animate-pulse rounded-xl border bg-muted/30" />;
  }
  if (!chain.length) {
    return (
      <div className="flex items-start gap-2.5 rounded-xl border border-amber-500/40 bg-amber-500/5 px-4 py-3">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />
        <div className="min-w-0 text-sm">
          <p className="font-medium">当前没有可用的 AI 通道</p>
          <p className="text-xs text-muted-foreground">
            {isPlatform
              ? "新建一条通道后，平台与共享兜底能力才可用。"
              : "AI 助手、aegis.ai 脚本与兼容网关现在都调不通。新建一条通道，或请平台管理员共享平台通道。"}
          </p>
        </div>
      </div>
    );
  }

  const inheritedOnly = chain.every((item) => item.inherited);
  return (
    <div className="space-y-2 rounded-xl border px-4 py-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-xs font-medium text-muted-foreground">调用链路</span>
        {chain.map((item, index) => {
          const meta = providers.find((p) => p.provider === item.provider);
          return (
            <span key={item.configId} className="flex items-center gap-2">
              {index > 0 && <ArrowRight className="size-3.5 text-muted-foreground/60" />}
              <span className="inline-flex items-center gap-1.5 rounded-lg border px-2 py-1">
                <AIBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="sm" className="size-5! rounded-md!" />
                <span className="text-xs font-medium">{item.configName}</span>
                {item.model && <span className="font-mono text-[10px] text-muted-foreground">{item.model}</span>}
                {item.inherited && (
                  <Badge variant="info" size="sm">
                    平台共享
                  </Badge>
                )}
              </span>
            </span>
          );
        })}
      </div>
      {!isPlatform && inheritedOnly && (
        <p className="flex items-center gap-1.5 text-[11px] text-sky-600 dark:text-sky-400">
          <Share2 className="size-3.5 shrink-0" />
          本应用没有自己的通道，正在借用平台共享通道 —— 调用计费落在平台头上。
        </p>
      )}
    </div>
  );
}

// ═══════════════════════ 通道配置 ═══════════════════════

type ConfigDraft = {
  configId?: number;
  provider: string;
  name: string;
  description: string;
  enabled: boolean;
  isDefault: boolean;
  shared: boolean;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
  testModel: string;
};

function ConfigsSection({
  scope,
  providers,
  configs
}: {
  scope: AIScope;
  providers: AIProviderMeta[];
  configs: AIConfig[];
}) {
  const saveMutation = useSaveAIConfigMutation(scope);
  const deleteMutation = useDeleteAIConfigMutation(scope);
  const testMutation = useTestAIConfigMutation(scope);
  const isPlatform = scope.kind === "platform";

  const [editing, setEditing] = useState<ConfigDraft | null>(null);
  const [testOutcome, setTestOutcome] = useState<TestOutcome | null>(null);

  function openCreate() {
    const first = providers[0];
    setEditing({
      provider: first?.provider ?? "openai",
      name: configs.length ? "" : "default",
      description: "",
      enabled: true,
      isDefault: configs.length === 0,
      shared: false,
      settings: buildDefaultAISettings(first),
      secrets: {},
      secretSet: {},
      testModel: ""
    });
    setTestOutcome(null);
  }

  function openEdit(config: AIConfig) {
    setEditing({
      configId: config.id,
      provider: config.provider,
      name: config.name ?? "",
      description: config.description ?? "",
      enabled: config.enabled !== false,
      isDefault: Boolean(config.isDefault),
      shared: Boolean(config.shared),
      settings: { ...(config.settings ?? {}) },
      secrets: {},
      secretSet: { ...(config.secretSet ?? {}) },
      testModel: ""
    });
    setTestOutcome(null);
  }

  async function handleSave(event: FormEvent) {
    event.preventDefault();
    if (!editing) return;
    try {
      await saveMutation.mutateAsync({
        configId: editing.configId,
        payload: {
          name: editing.name,
          provider: editing.provider,
          description: editing.description,
          enabled: editing.enabled,
          isDefault: editing.isDefault,
          ...(isPlatform ? { shared: editing.shared } : {}),
          settings: editing.settings,
          // 密钥留空的键在这里就不发出去，后端也会再挡一道 ——
          // 这条约定一旦失效，代价是用户的凭据被静默清空。
          secrets: Object.fromEntries(Object.entries(editing.secrets).filter(([, v]) => v.trim() !== "")),
          replaceSettings: true
        }
      });
      toast.success(editing.configId ? "通道已更新" : "通道已创建");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!editing?.configId) return;
    try {
      await deleteMutation.mutateAsync(editing.configId);
      toast.success("已删除");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  async function handleTest() {
    if (!editing?.configId) return;
    setTestOutcome(null);
    try {
      const result = await testMutation.mutateAsync({
        configId: editing.configId,
        model: editing.testModel.trim() || undefined
      });
      if (result.ok) {
        setTestOutcome({
          ok: true,
          text: `连通正常（${result.elapsedMs}ms · ${result.model}）${result.reply ? `：${result.reply}` : ""}`
        });
        toast.success("测试通过");
      } else {
        setTestOutcome({ ok: false, text: `测试未通过：${clipText(result.error ?? "未知错误")}` });
        toast.error("测试未通过，详情见下方");
      }
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  }

  const activeMeta = providers.find((p) => p.provider === editing?.provider);

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button size="sm" onClick={openCreate} disabled={!providers.length}>
          <Plus className="size-3.5" /> 新建通道
        </Button>
      </div>

      {configs.length === 0 ? (
        <EmptyState
          title="还没有 AI 通道"
          description={
            isPlatform
              ? "新建一条通道并打开「共享」，应用即可零配置借用。"
              : "新建一条通道；也可以让平台管理员把平台通道设为共享，本应用即可直接借用。"
          }
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {configs.map((config) => (
            <ConfigCard
              key={config.id}
              config={config}
              meta={providers.find((p) => p.provider === config.provider)}
              onClick={() => openEdit(config)}
            />
          ))}
        </div>
      )}

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-w-2xl p-0! gap-0!">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle>{editing?.configId ? "编辑 AI 通道" : "新建 AI 通道"}</DialogTitle>
          </DialogHeader>

          {editing && (
            <form onSubmit={handleSave} className="max-h-[70vh] space-y-4 overflow-y-auto px-6 py-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs">
                    通道名称<span className="text-destructive"> *</span>
                  </Label>
                  <Input
                    className="h-8 text-sm"
                    placeholder="default"
                    value={editing.name}
                    onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                    required
                  />
                  <p className="text-[11px] text-muted-foreground">同一作用域内唯一；链路按优先级 + 默认位排序。</p>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">供应商</Label>
                  <ProviderPicker
                    providers={providers}
                    value={editing.provider}
                    onChange={(provider) => {
                      const meta = providers.find((p) => p.provider === provider);
                      // 换供应商时把字段重置成新供应商的默认值，避免上一家的残留值提交上去
                      setEditing({ ...editing, provider, settings: buildDefaultAISettings(meta), secrets: {} });
                    }}
                  />
                </div>
              </div>

              <Separator />

              <AIProviderFields
                meta={activeMeta}
                settings={editing.settings}
                secrets={editing.secrets}
                secretSet={editing.secretSet}
                onSetting={(key, value) => setEditing({ ...editing, settings: { ...editing.settings, [key]: value } })}
                onSecret={(key, value) => setEditing({ ...editing, secrets: { ...editing.secrets, [key]: value } })}
              />

              <Separator />

              <div className="space-y-1.5">
                <Label className="text-xs">说明</Label>
                <Textarea
                  className="text-sm"
                  rows={2}
                  placeholder="可选备注"
                  value={editing.description}
                  onChange={(e) => setEditing({ ...editing, description: e.target.value })}
                />
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <SwitchRow
                  label="启用"
                  help="关掉之后这条通道不会出现在任何链路里"
                  checked={editing.enabled}
                  onCheckedChange={(v) => setEditing({ ...editing, enabled: v })}
                />
                <SwitchRow
                  label="设为默认"
                  help="没有指名通道的调用都先走它"
                  checked={editing.isDefault}
                  onCheckedChange={(v) => setEditing({ ...editing, isDefault: v })}
                />
              </div>

              {isPlatform && (
                <SwitchRow
                  label="共享给应用作为兜底"
                  help="应用自己一条通道都没有时，它的 AI 调用会走这条通道 —— 计费落在平台头上。默认关闭。"
                  checked={editing.shared}
                  onCheckedChange={(v) => setEditing({ ...editing, shared: v })}
                />
              )}

              {editing.configId && (
                <>
                  <Separator />
                  <div className="flex items-end gap-2">
                    <div className="flex-1 space-y-1.5">
                      <Label className="text-xs">测试型号（留空用默认型号）</Label>
                      <Input
                        className="h-8 font-mono text-xs"
                        placeholder={activeMeta?.suggestedModels?.[0] ?? ""}
                        value={editing.testModel}
                        onChange={(e) => setEditing({ ...editing, testModel: e.target.value })}
                      />
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-8"
                      onClick={() => void handleTest()}
                      disabled={testMutation.isPending}
                    >
                      <Sparkles className="size-3.5" /> {testMutation.isPending ? "测试中…" : "连通性测试"}
                    </Button>
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    测试使用已保存的配置，将产生一次真实调用计费；修改配置后请先保存再测试。
                  </p>
                  {testOutcome ? <TestOutcomeNote outcome={testOutcome} /> : null}
                </>
              )}
            </form>
          )}

          <DialogFooter className="border-t px-6 py-4">
            <div className="flex w-full items-center justify-between">
              <div>
                {editing?.configId && (
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    onClick={() => void handleDelete()}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="size-3.5" /> 删除
                  </Button>
                )}
              </div>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setEditing(null)}>
                  取消
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={(e) => void handleSave(e as unknown as FormEvent)}
                  disabled={saveMutation.isPending}
                >
                  <Save className="size-3.5" /> {saveMutation.isPending ? "保存中…" : "保存"}
                </Button>
              </div>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ConfigCard({
  config,
  meta,
  onClick
}: {
  config: AIConfig;
  meta?: AIProviderMeta;
  onClick: () => void;
}) {
  const capabilities = AI_CAPABILITY_LABELS.filter((c) => meta?.capabilities?.[c.key]);
  const defaultModel = config.settings?.defaultModel || config.settings?.models?.split(/[\n,]/)[0]?.trim();
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex flex-col gap-2 rounded-xl border p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30"
    >
      <div className="flex w-full items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <AIBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="sm" />
          <span className="truncate text-sm font-semibold">{config.name || `配置 ${config.id}`}</span>
        </div>
        <div className="flex shrink-0 gap-1.5">
          {config.isDefault && (
            <Badge variant="outline" size="sm">
              默认
            </Badge>
          )}
          {config.shared && (
            <Badge variant="info" size="sm">
              共享
            </Badge>
          )}
          <Badge variant={config.enabled === false ? "warning" : "success"} size="sm">
            {config.enabled === false ? "停用" : "启用"}
          </Badge>
        </div>
      </div>

      <span className="text-xs text-muted-foreground">{meta?.name ?? config.provider}</span>
      <span className="truncate font-mono text-xs text-muted-foreground">{defaultModel || "未填写型号"}</span>

      {capabilities.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {capabilities.map((c) => (
            <Badge key={c.key} variant="secondary" size="sm" className="font-normal">
              {c.label}
            </Badge>
          ))}
        </div>
      )}
    </button>
  );
}

function ProviderPicker({
  providers,
  value,
  onChange
}: {
  providers: AIProviderMeta[];
  value: string;
  onChange: (provider: string) => void;
}) {
  // 按目录下发的分类分组；后端加一个分类时这里自动多一组。
  const groups = useMemo(() => {
    const map = new Map<string, AIProviderMeta[]>();
    for (const provider of providers) {
      const key = provider.categoryName || "其他";
      const list = map.get(key) ?? [];
      list.push(provider);
      map.set(key, list);
    }
    return [...map.entries()];
  }, [providers]);

  return (
    <div className="space-y-2">
      {groups.map(([category, items]) => (
        <div key={category} className="space-y-1">
          <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{category}</span>
          <div className="flex flex-wrap gap-1.5">
            {items.map((provider) => (
              <button
                key={provider.provider}
                type="button"
                onClick={() => onChange(provider.provider)}
                title={provider.description}
                className={`inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 text-xs transition-colors ${
                  value === provider.provider
                    ? "border-primary bg-primary/10 text-foreground"
                    : "hover:border-primary/30 hover:bg-muted/40"
                }`}
              >
                <AIBrandBadge
                  slug={provider.icon}
                  brandColor={provider.brandColor}
                  name={provider.name}
                  size="sm"
                  className="size-5! rounded-md!"
                />
                {provider.name}
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function SwitchRow({
  label,
  help,
  checked,
  onCheckedChange
}: {
  label: string;
  help?: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-start justify-between gap-3 rounded-xl border px-4 py-3">
      <span className="min-w-0 space-y-0.5">
        <span className="block text-sm">{label}</span>
        {help && <span className="block text-[11px] leading-relaxed text-muted-foreground">{help}</span>}
      </span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} className="mt-0.5 shrink-0" />
    </label>
  );
}

// ═══════════════════════ 技能 ═══════════════════════

type SkillDraft = {
  skillId?: number;
  key: string;
  name: string;
  description: string;
  content: string;
  enabled: boolean;
  builtin: boolean;
};

function SkillsSection({ scope }: { scope: AIScope }) {
  const skillsQuery = useAISkillsQuery(scope);
  const saveMutation = useSaveAISkillMutation(scope);
  const deleteMutation = useDeleteAISkillMutation(scope);
  const [editing, setEditing] = useState<SkillDraft | null>(null);

  const skills = skillsQuery.data ?? [];

  async function handleSave(event: FormEvent) {
    event.preventDefault();
    if (!editing) return;
    try {
      await saveMutation.mutateAsync({
        skillId: editing.skillId,
        payload: editing.builtin
          ? // 内置技能只有开关可改；键名/正文由 Go 侧目录提供。
            { enabled: editing.enabled }
          : {
              key: editing.key,
              name: editing.name,
              description: editing.description,
              content: editing.content,
              enabled: editing.enabled
            }
      });
      toast.success(editing.skillId ? "技能已更新" : "技能已创建");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!editing?.skillId || editing.builtin) return;
    try {
      await deleteMutation.mutateAsync(editing.skillId);
      toast.success("已删除");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          技能是可复用的提示词包：领域约定、代码风格、排错清单。启用的技能会注入 Agent 的系统提示词。
        </p>
        <Button
          size="sm"
          onClick={() =>
            setEditing({ key: "", name: "", description: "", content: "", enabled: true, builtin: false })
          }
        >
          <Plus className="size-3.5" /> 新建技能
        </Button>
      </div>

      {skills.length === 0 && !skillsQuery.isLoading ? (
        <EmptyState title="还没有技能" description="内置技能由平台提供；也可以把团队约定写成自定义技能。" />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {skills.map((skill) => (
            <SkillCard
              key={`${skill.builtin ? "b" : "c"}-${skill.id}-${skill.key}`}
              skill={skill}
              onClick={() =>
                setEditing({
                  skillId: skill.id,
                  key: skill.key,
                  name: skill.name,
                  description: skill.description,
                  content: skill.content,
                  enabled: skill.enabled,
                  builtin: skill.builtin
                })
              }
            />
          ))}
        </div>
      )}

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-w-2xl p-0! gap-0!">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle>
              {editing?.builtin ? "内置技能" : editing?.skillId ? "编辑技能" : "新建技能"}
            </DialogTitle>
          </DialogHeader>

          {editing && (
            <form onSubmit={handleSave} className="max-h-[70vh] space-y-4 overflow-y-auto px-6 py-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs">
                    键名<span className="text-destructive"> *</span>
                  </Label>
                  <Input
                    className="h-8 font-mono text-xs"
                    placeholder="team-style"
                    value={editing.key}
                    onChange={(e) => setEditing({ ...editing, key: e.target.value })}
                    disabled={editing.builtin}
                    required
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">
                    名称<span className="text-destructive"> *</span>
                  </Label>
                  <Input
                    className="h-8 text-sm"
                    placeholder="团队代码风格"
                    value={editing.name}
                    onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                    disabled={editing.builtin}
                    required
                  />
                </div>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">一句话说明</Label>
                <Input
                  className="h-8 text-sm"
                  value={editing.description}
                  onChange={(e) => setEditing({ ...editing, description: e.target.value })}
                  disabled={editing.builtin}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">
                  内容（Markdown）{!editing.builtin && <span className="text-destructive"> *</span>}
                </Label>
                <Textarea
                  className="font-mono text-xs"
                  rows={editing.builtin ? 14 : 10}
                  value={editing.content}
                  onChange={(e) => setEditing({ ...editing, content: e.target.value })}
                  readOnly={editing.builtin}
                  required={!editing.builtin}
                />
                {editing.builtin && (
                  <p className="text-[11px] text-muted-foreground">内置技能的正文随平台版本更新，这里只读。</p>
                )}
              </div>
              <SwitchRow
                label="启用"
                help="启用的技能会注入到 Agent 的系统提示词里"
                checked={editing.enabled}
                onCheckedChange={(v) => setEditing({ ...editing, enabled: v })}
              />
            </form>
          )}

          <DialogFooter className="border-t px-6 py-4">
            <div className="flex w-full items-center justify-between">
              <div>
                {editing?.skillId && !editing.builtin ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    onClick={() => void handleDelete()}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="size-3.5" /> 删除
                  </Button>
                ) : null}
              </div>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setEditing(null)}>
                  取消
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={(e) => void handleSave(e as unknown as FormEvent)}
                  disabled={saveMutation.isPending}
                >
                  <Save className="size-3.5" /> {saveMutation.isPending ? "保存中…" : "保存"}
                </Button>
              </div>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function SkillCard({ skill, onClick }: { skill: AISkill; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex flex-col gap-1.5 rounded-xl border p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30"
    >
      <div className="flex w-full items-start justify-between gap-2">
        <span className="truncate text-sm font-semibold">{skill.name}</span>
        <div className="flex shrink-0 gap-1.5">
          {skill.builtin && (
            <Badge variant="outline" size="sm">
              内置
            </Badge>
          )}
          <Badge variant={skill.enabled ? "success" : "warning"} size="sm">
            {skill.enabled ? "启用" : "停用"}
          </Badge>
        </div>
      </div>
      <span className="font-mono text-[11px] text-muted-foreground">{skill.key}</span>
      {skill.description && <span className="line-clamp-2 text-xs text-muted-foreground">{skill.description}</span>}
    </button>
  );
}

// ═══════════════════════ MCP 服务器 ═══════════════════════

type MCPDraft = {
  serverId?: number;
  name: string;
  url: string;
  description: string;
  enabled: boolean;
  headersText: string;
  headersSet: boolean;
  clearHeaders: boolean;
};

function MCPSection({ scope }: { scope: AIScope }) {
  const serversQuery = useAIMCPServersQuery(scope);
  const saveMutation = useSaveAIMCPServerMutation(scope);
  const deleteMutation = useDeleteAIMCPServerMutation(scope);
  const testMutation = useTestAIMCPServerMutation(scope);

  const [editing, setEditing] = useState<MCPDraft | null>(null);
  const [testOutcome, setTestOutcome] = useState<TestOutcome | null>(null);

  const servers = serversQuery.data ?? [];

  function parseHeaders(text: string): Record<string, string> {
    const map: Record<string, string> = {};
    for (const line of text.split("\n")) {
      const index = line.indexOf("=");
      if (index <= 0) continue;
      const key = line.slice(0, index).trim();
      if (!key) continue;
      map[key] = line.slice(index + 1).trim();
    }
    return map;
  }

  async function handleSave(event: FormEvent) {
    event.preventDefault();
    if (!editing) return;
    const headers = parseHeaders(editing.headersText);
    try {
      await saveMutation.mutateAsync({
        serverId: editing.serverId,
        payload: {
          name: editing.name,
          url: editing.url,
          description: editing.description,
          enabled: editing.enabled,
          // 没填新头且没点清除时不发 headers，保持已有配置不动
          ...(Object.keys(headers).length ? { headers } : {}),
          ...(editing.clearHeaders ? { clearHeaders: true } : {})
        }
      });
      toast.success(editing.serverId ? "MCP 服务器已更新" : "MCP 服务器已添加");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!editing?.serverId) return;
    try {
      await deleteMutation.mutateAsync(editing.serverId);
      toast.success("已删除");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  async function handleTest() {
    if (!editing?.serverId) return;
    setTestOutcome(null);
    try {
      const result = await testMutation.mutateAsync(editing.serverId);
      if (result.ok) {
        setTestOutcome({
          ok: true,
          text: `连通正常（${result.elapsedMs}ms），${result.count ?? result.tools?.length ?? 0} 个工具：${(result.tools ?? []).join("、")}`
        });
        toast.success("测试通过");
      } else {
        setTestOutcome({ ok: false, text: `测试未通过：${clipText(result.error ?? "未知错误")}` });
        toast.error("测试未通过，详情见下方");
      }
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          外接的 MCP 工具服务器（Streamable HTTP）。启用后其工具自动出现在 Agent 的工具清单里。
        </p>
        <Button
          size="sm"
          onClick={() => {
            setEditing({
              name: "",
              url: "",
              description: "",
              enabled: true,
              headersText: "",
              headersSet: false,
              clearHeaders: false
            });
            setTestOutcome(null);
          }}
        >
          <Plus className="size-3.5" /> 添加服务器
        </Button>
      </div>

      {servers.length === 0 && !serversQuery.isLoading ? (
        <EmptyState title="还没有 MCP 服务器" description="接入内部工具平台或第三方 MCP 服务，扩展 Agent 的能力。" />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {servers.map((server) => (
            <button
              key={server.id}
              type="button"
              onClick={() => {
                setEditing({
                  serverId: server.id,
                  name: server.name,
                  url: server.url,
                  description: server.description ?? "",
                  enabled: server.enabled,
                  headersText: "",
                  headersSet: server.headersSet,
                  clearHeaders: false
                });
                setTestOutcome(null);
              }}
              className="flex flex-col gap-1.5 rounded-xl border p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30"
            >
              <div className="flex w-full items-start justify-between gap-2">
                <span className="flex min-w-0 items-center gap-1.5 text-sm font-semibold">
                  <Server className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{server.name}</span>
                </span>
                <Badge variant={server.enabled ? "success" : "warning"} size="sm">
                  {server.enabled ? "启用" : "停用"}
                </Badge>
              </div>
              <span className="truncate font-mono text-[11px] text-muted-foreground">{server.url}</span>
              {server.description && (
                <span className="line-clamp-2 text-xs text-muted-foreground">{server.description}</span>
              )}
            </button>
          ))}
        </div>
      )}

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-w-xl p-0! gap-0!">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle>{editing?.serverId ? "编辑 MCP 服务器" : "添加 MCP 服务器"}</DialogTitle>
          </DialogHeader>

          {editing && (
            <form onSubmit={handleSave} className="max-h-[70vh] space-y-4 overflow-y-auto px-6 py-5">
              <div className="space-y-1.5">
                <Label className="text-xs">
                  名称<span className="text-destructive"> *</span>
                </Label>
                <Input
                  className="h-8 text-sm"
                  placeholder="内部工具平台"
                  value={editing.name}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">
                  服务地址<span className="text-destructive"> *</span>
                </Label>
                <Input
                  className="h-8 font-mono text-xs"
                  placeholder="https://mcp.example.com/mcp"
                  value={editing.url}
                  onChange={(e) => setEditing({ ...editing, url: e.target.value })}
                  required
                />
                <p className="text-[11px] text-muted-foreground">Streamable HTTP 端点；出站走平台的统一出网通道。</p>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">鉴权请求头</Label>
                <Textarea
                  className="font-mono text-xs"
                  rows={3}
                  placeholder={editing.headersSet ? "已配置，留空即不修改" : "Authorization=Bearer sk-…\nX-API-Key=…"}
                  value={editing.headersText}
                  onChange={(e) => setEditing({ ...editing, headersText: e.target.value })}
                />
                <p className="text-[11px] text-muted-foreground">
                  每行一条「键=值」。整体加密存放、永不回显{editing.headersSet ? "；当前已配置" : ""}。
                </p>
                {editing.headersSet && (
                  <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
                    <Switch
                      checked={editing.clearHeaders}
                      onCheckedChange={(v) => setEditing({ ...editing, clearHeaders: v })}
                    />
                    清空已配置的请求头
                  </label>
                )}
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">说明</Label>
                <Input
                  className="h-8 text-sm"
                  value={editing.description}
                  onChange={(e) => setEditing({ ...editing, description: e.target.value })}
                />
              </div>
              <SwitchRow
                label="启用"
                help="启用后其工具清单会并入 Agent 可调用的工具"
                checked={editing.enabled}
                onCheckedChange={(v) => setEditing({ ...editing, enabled: v })}
              />

              {editing.serverId && (
                <>
                  <Separator />
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-[11px] text-muted-foreground">测试将连接已保存的地址并拉取工具列表。</p>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-8 shrink-0"
                      onClick={() => void handleTest()}
                      disabled={testMutation.isPending}
                    >
                      <Sparkles className="size-3.5" /> {testMutation.isPending ? "测试中…" : "连通性测试"}
                    </Button>
                  </div>
                  {testOutcome ? <TestOutcomeNote outcome={testOutcome} /> : null}
                </>
              )}
            </form>
          )}

          <DialogFooter className="border-t px-6 py-4">
            <div className="flex w-full items-center justify-between">
              <div>
                {editing?.serverId && (
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    onClick={() => void handleDelete()}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="size-3.5" /> 删除
                  </Button>
                )}
              </div>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setEditing(null)}>
                  取消
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={(e) => void handleSave(e as unknown as FormEvent)}
                  disabled={saveMutation.isPending}
                >
                  <Save className="size-3.5" /> {saveMutation.isPending ? "保存中…" : "保存"}
                </Button>
              </div>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
