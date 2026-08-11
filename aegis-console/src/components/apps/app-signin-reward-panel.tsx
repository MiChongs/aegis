"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";
import { Gift, Loader2, Plus, RotateCcw, Save, Sparkles, TestTubeDiagonal, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  ApiError,
  type AppSignInRecordItem,
  type SignInRewardMilestone,
  type SignInRewardPolicy,
  type SignInRewardPreview,
  type SignInRewardPreviewInput,
  type SignInRewardRule
} from "@/lib/api-client";
import {
  useAdminAppSignInRecordsQuery,
  useAdminAppSignInStatsQuery,
  useAdminAppSignInRewardQuery,
  useResetAdminAppSignInRewardMutation,
  useSignInRewardTemplatesQuery,
  useTestAdminAppSignInRewardMutation,
  useUpdateAdminAppSignInRewardMutation
} from "@/lib/admin-hooks";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

type Props = { appKey?: string | null };

type PreviewForm = {
  occurredAt: string;
  consecutiveDays: number;
  totalSignIns: number;
  userExperience: number;
};

const templateOptions = [
  { key: "balanced", label: "balanced" },
  { key: "growth", label: "growth" },
  { key: "retention", label: "retention" }
] as const;

const defaultPreviewForm = (): PreviewForm => ({
  occurredAt: "",
  consecutiveDays: 1,
  totalSignIns: 1,
  userExperience: 0
});

const defaultRecordFilters = () => ({
  keyword: "",
  source: "all",
  dateFrom: "",
  dateTo: "",
  page: 1,
  limit: 20
});

const signInTrendChartConfig: ChartConfig = {
  count: { label: "签到次数", color: "#16a34a" }
};

const newRule = (): SignInRewardRule => ({
  key: `rule_${Date.now()}`,
  name: "新规则",
  description: "",
  enabled: true,
  priority: 100,
  group: "",
  expression: "consecutiveDays >= 1",
  bonusType: "",
  bonusDescription: "",
  integralMultiplierDelta: 0,
  integralBonus: 0,
  experienceMultiplierDelta: 0,
  experienceBonus: 0
});

const newMilestone = (): SignInRewardMilestone => ({
  consecutiveDays: 7,
  integralBonus: 0,
  experienceBonus: 0,
  bonusType: "",
  description: ""
});

function clonePolicy(policy: SignInRewardPolicy): SignInRewardPolicy {
  return JSON.parse(JSON.stringify(policy)) as SignInRewardPolicy;
}

function toDateTimeLocalInput(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${year}-${month}-${day}T${hours}:${minutes}`;
}

function validatePolicy(policy: SignInRewardPolicy) {
  if (!policy.timezone.trim()) return "时区不能为空";
  for (const rule of policy.rules) {
    if (!rule.key.trim()) return "规则 key 不能为空";
    if (!rule.name.trim()) return "规则名称不能为空";
    if (!rule.expression.trim()) return "规则表达式不能为空";
  }
  for (const milestone of policy.milestones) {
    if (milestone.consecutiveDays <= 0) return "里程碑天数必须大于 0";
  }
  return null;
}

function formatShortDate(value?: string) {
  if (!value) return "-";
  try {
    return new Date(value).toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
  } catch {
    return value;
  }
}

function formatDateTime(value?: string) {
  if (!value) return "-";
  try {
    return new Date(value).toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit"
    });
  } catch {
    return value;
  }
}

export function AppSignInRewardPanel({ appKey }: Props) {
  const rewardQuery = useAdminAppSignInRewardQuery(appKey);
  const [statsDays, setStatsDays] = useState("14");
  const statsQuery = useAdminAppSignInStatsQuery(appKey, Number(statsDays) || 14);
  const templatesQuery = useSignInRewardTemplatesQuery();
  const saveMutation = useUpdateAdminAppSignInRewardMutation(appKey);
  const testMutation = useTestAdminAppSignInRewardMutation(appKey);
  const resetMutation = useResetAdminAppSignInRewardMutation(appKey);
  const [recordDraftFilters, setRecordDraftFilters] = useState(defaultRecordFilters);
  const [recordFilters, setRecordFilters] = useState(defaultRecordFilters);
  const recordsQuery = useAdminAppSignInRecordsQuery(appKey, {
    ...recordFilters,
    source: recordFilters.source === "all" ? undefined : recordFilters.source
  });

  const [draft, setDraft] = useState<SignInRewardPolicy | null>(null);
  const [previewForm, setPreviewForm] = useState<PreviewForm>(defaultPreviewForm);
  const [previewResult, setPreviewResult] = useState<SignInRewardPreview | null>(null);
  const [selectedTemplate, setSelectedTemplate] = useState<string>("balanced");
  const [resetOpen, setResetOpen] = useState(false);

  useEffect(() => {
    if (!rewardQuery.data?.policy) return;
    const next = clonePolicy(rewardQuery.data.policy);
    setDraft(next);
    setPreviewForm((current) => ({
      ...current,
      consecutiveDays: Math.max(current.consecutiveDays, 1),
      totalSignIns: Math.max(current.totalSignIns, 1)
    }));
    setPreviewResult(null);
  }, [rewardQuery.data, appKey]);

  useEffect(() => {
    setRecordDraftFilters(defaultRecordFilters());
    setRecordFilters(defaultRecordFilters());
    setStatsDays("14");
  }, [appKey]);

  const sortedRules = useMemo(
    () =>
      (draft?.rules ?? [])
        .map((rule, index) => ({ rule, index }))
        .sort((a, b) => a.rule.priority - b.rule.priority || a.index - b.index),
    [draft]
  );

  const sortedMilestones = useMemo(
    () =>
      (draft?.milestones ?? [])
        .map((milestone, index) => ({ milestone, index }))
        .sort((a, b) => a.milestone.consecutiveDays - b.milestone.consecutiveDays || a.index - b.index),
    [draft]
  );

  const signInTrendData = useMemo(
    () =>
      (statsQuery.data?.trend ?? []).map((item) => ({
        date: formatShortDate(item.date),
        count: item.count
      })),
    [statsQuery.data?.trend]
  );

  const selectedTemplatePolicy = templatesQuery.data?.templates?.[selectedTemplate];

  function patchDraft<K extends keyof SignInRewardPolicy>(key: K, value: SignInRewardPolicy[K]) {
    setDraft((current) => (current ? { ...current, [key]: value } : current));
  }

  function patchRule(index: number, patch: Partial<SignInRewardRule>) {
    setDraft((current) => {
      if (!current) return current;
      const rules = current.rules.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item));
      return { ...current, rules };
    });
  }

  function patchMilestone(index: number, patch: Partial<SignInRewardMilestone>) {
    setDraft((current) => {
      if (!current) return current;
      const milestones = current.milestones.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item
      );
      return { ...current, milestones };
    });
  }

  function addRule() {
    setDraft((current) => (current ? { ...current, rules: [...current.rules, newRule()] } : current));
  }

  function removeRule(index: number) {
    setDraft((current) => (current ? { ...current, rules: current.rules.filter((_, itemIndex) => itemIndex !== index) } : current));
  }

  function addMilestone() {
    setDraft((current) =>
      current ? { ...current, milestones: [...current.milestones, newMilestone()] } : current
    );
  }

  function removeMilestone(index: number) {
    setDraft((current) =>
      current ? { ...current, milestones: current.milestones.filter((_, itemIndex) => itemIndex !== index) } : current
    );
  }

  async function applyTemplate() {
    if (!selectedTemplatePolicy) {
      if (templatesQuery.isError) {
        toast.error(templatesQuery.error instanceof ApiError ? templatesQuery.error.message : "模板加载失败");
        return;
      }
      toast.error("模板尚未加载完成");
      return;
    }
    try {
      const result = await saveMutation.mutateAsync({ policy: clonePolicy(selectedTemplatePolicy) });
      setDraft(clonePolicy(result.policy));
      setPreviewResult(null);
      toast.success(`已应用模板 ${selectedTemplate}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "应用模板失败");
    }
  }

  async function handleSave() {
    if (!draft) return;
    const errorMessage = validatePolicy(draft);
    if (errorMessage) {
      toast.error(errorMessage);
      return;
    }
    try {
      await saveMutation.mutateAsync({ policy: draft });
      toast.success("签到奖励已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleTest() {
    if (!draft) return;
    const errorMessage = validatePolicy(draft);
    if (errorMessage) {
      toast.error(errorMessage);
      return;
    }
    const payload: SignInRewardPreviewInput = {
      consecutiveDays: previewForm.consecutiveDays,
      totalSignIns: previewForm.totalSignIns,
      userExperience: previewForm.userExperience
    };
    if (previewForm.occurredAt) {
      const date = new Date(previewForm.occurredAt);
      if (Number.isNaN(date.getTime())) {
        toast.error("测试时间格式无效");
        return;
      }
      payload.occurredAt = date.toISOString();
    }
    try {
      const result = await testMutation.mutateAsync(payload);
      setPreviewResult(result);
      toast.success("测试完成");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  }

  async function handleReset() {
    try {
      const result = await resetMutation.mutateAsync();
      setDraft(clonePolicy(result.policy));
      setPreviewResult(null);
      setResetOpen(false);
      toast.success("已恢复默认策略");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重置失败");
    }
  }

  function applyRecordFilters() {
    setRecordFilters({
      ...recordDraftFilters,
      page: 1
    });
  }

  function resetRecordFilters() {
    const next = defaultRecordFilters();
    setRecordDraftFilters(next);
    setRecordFilters(next);
  }

  if (!appKey) {
    return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;
  }

  if (rewardQuery.isLoading || !draft) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-56 w-full" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <Section
        title="签到统计"
        action={
          <Field label="窗口">
            <Select value={statsDays} onValueChange={setStatsDays}>
              <SelectTrigger className="h-8 w-28 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="7">7 天</SelectItem>
                <SelectItem value="14">14 天</SelectItem>
                <SelectItem value="30">30 天</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
          <MetricCard label="今日签到" value={statsQuery.data?.todaySignCount} loading={statsQuery.isLoading} />
          <MetricCard label="累计记录" value={statsQuery.data?.totalSignRecords} loading={statsQuery.isLoading} />
          <MetricCard label="签到用户" value={statsQuery.data?.uniqueSignedUsers} loading={statsQuery.isLoading} />
          <MetricCard label="总积分奖励" value={statsQuery.data?.totalIntegralReward} loading={statsQuery.isLoading} />
          <MetricCard label="总经验奖励" value={statsQuery.data?.totalExperienceReward} loading={statsQuery.isLoading} />
          <MetricCard
            label="平均连签"
            value={statsQuery.data?.avgConsecutiveDays}
            loading={statsQuery.isLoading}
            fractionDigits={1}
          />
        </div>
        <div className="grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
          <div className="rounded-xl border p-4">
            <div className="mb-3 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              近 {statsQuery.data?.days ?? (Number(statsDays) || 14)} 天趋势
            </div>
            {statsQuery.isLoading ? (
              <Skeleton className="h-56 w-full rounded-lg" />
            ) : signInTrendData.length > 0 ? (
              <ChartContainer config={signInTrendChartConfig} className="h-56 w-full">
                <AreaChart data={signInTrendData} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
                  <defs>
                    <linearGradient id="signinTrendGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#16a34a" stopOpacity={0.22} />
                      <stop offset="100%" stopColor="#16a34a" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: "var(--muted-foreground)" }} axisLine={false} tickLine={false} />
                  <YAxis
                    tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                    axisLine={false}
                    tickLine={false}
                    allowDecimals={false}
                    width={36}
                  />
                  <Tooltip
                    cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
                    content={({ active, payload, label }) => {
                      if (!active || !payload?.length) return null;
                      return (
                        <div className="rounded-lg border bg-popover px-3 py-2 shadow-md">
                          <div className="text-[11px] text-muted-foreground">{label}</div>
                          <div className="mt-0.5 text-sm font-semibold tabular-nums">
                            {Number(payload[0].value).toLocaleString()} 次签到
                          </div>
                        </div>
                      );
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="count"
                    stroke="#16a34a"
                    fill="url(#signinTrendGrad)"
                    strokeWidth={2}
                    dot={false}
                    activeDot={{ r: 4, fill: "#16a34a", stroke: "var(--background)", strokeWidth: 2 }}
                  />
                </AreaChart>
              </ChartContainer>
            ) : (
              <div className="flex h-56 items-center justify-center text-xs text-muted-foreground">暂无趋势数据</div>
            )}
          </div>

          <div className="rounded-xl border p-4">
            <div className="mb-3 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">签到来源</div>
            {statsQuery.isLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-10 w-full rounded-lg" />
                <Skeleton className="h-10 w-full rounded-lg" />
                <Skeleton className="h-10 w-full rounded-lg" />
              </div>
            ) : (statsQuery.data?.sources?.length ?? 0) > 0 ? (
              <div className="space-y-2">
                {statsQuery.data?.sources.map((item) => (
                  <div key={item.source} className="flex items-center justify-between rounded-lg border px-3 py-2">
                    <div className="text-sm">{item.source || "manual"}</div>
                    <div className="text-sm font-semibold tabular-nums">{item.count.toLocaleString()}</div>
                  </div>
                ))}
                <div className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
                  最高连签：{(statsQuery.data?.maxConsecutiveDays ?? 0).toLocaleString()} 天
                </div>
              </div>
            ) : (
              <EmptyBlock>暂无来源数据</EmptyBlock>
            )}
          </div>
        </div>
      </Section>

      <Separator />

      <Section title="每日签到明细">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-6">
          <Field label="关键词">
            <Input
              className="h-8 text-sm"
              placeholder="账号 / 昵称 / 邮箱 / 手机 / IP / 用户ID"
              value={recordDraftFilters.keyword}
              onChange={(event) =>
                setRecordDraftFilters((current) => ({ ...current, keyword: event.target.value }))
              }
            />
          </Field>
          <Field label="来源">
            <Select
              value={recordDraftFilters.source}
              onValueChange={(value) => setRecordDraftFilters((current) => ({ ...current, source: value }))}
            >
              <SelectTrigger className="h-8 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="manual">manual</SelectItem>
                <SelectItem value="auto">auto</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="开始日期">
            <Input
              type="date"
              className="h-8 text-sm"
              value={recordDraftFilters.dateFrom}
              onChange={(event) =>
                setRecordDraftFilters((current) => ({ ...current, dateFrom: event.target.value }))
              }
            />
          </Field>
          <Field label="结束日期">
            <Input
              type="date"
              className="h-8 text-sm"
              value={recordDraftFilters.dateTo}
              onChange={(event) =>
                setRecordDraftFilters((current) => ({ ...current, dateTo: event.target.value }))
              }
            />
          </Field>
          <Field label="每页">
            <Select
              value={String(recordDraftFilters.limit)}
              onValueChange={(value) =>
                setRecordDraftFilters((current) => ({ ...current, limit: Number(value) || 20 }))
              }
            >
              <SelectTrigger className="h-8 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="20">20</SelectItem>
                <SelectItem value="50">50</SelectItem>
                <SelectItem value="100">100</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <div className="flex items-end gap-2">
            <Button className="h-8 text-xs" onClick={applyRecordFilters}>
              查询
            </Button>
            <Button variant="outline" className="h-8 text-xs" onClick={resetRecordFilters}>
              重置
            </Button>
          </div>
        </div>

        <div className="rounded-xl border">
          <ScrollArea className="w-full">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>签到时间</TableHead>
                  <TableHead>奖励</TableHead>
                  <TableHead>连签</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>网络</TableHead>
                  <TableHead>说明</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recordsQuery.isLoading ? (
                  Array.from({ length: 5 }).map((_, index) => (
                    <TableRow key={`signin-record-skeleton-${index}`}>
                      <TableCell colSpan={7}>
                        <Skeleton className="h-8 w-full rounded-lg" />
                      </TableCell>
                    </TableRow>
                  ))
                ) : (recordsQuery.data?.items?.length ?? 0) > 0 ? (
                  recordsQuery.data?.items.map((item) => <SignInRecordRow key={item.id} item={item} />)
                ) : (
                  <TableRow>
                    <TableCell colSpan={7} className="py-8 text-center text-sm text-muted-foreground">
                      暂无签到记录
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </ScrollArea>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
          <div>
            共 {(recordsQuery.data?.total ?? 0).toLocaleString()} 条，第 {recordsQuery.data?.page ?? recordFilters.page} /
            {" "}
            {recordsQuery.data?.totalPages ?? 1} 页
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              className="h-8 text-xs"
              disabled={(recordsQuery.data?.page ?? recordFilters.page) <= 1 || recordsQuery.isFetching}
              onClick={() =>
                setRecordFilters((current) => ({ ...current, page: Math.max(current.page - 1, 1) }))
              }
            >
              上一页
            </Button>
            <Button
              variant="outline"
              className="h-8 text-xs"
              disabled={
                (recordsQuery.data?.page ?? recordFilters.page) >= (recordsQuery.data?.totalPages ?? 1) ||
                recordsQuery.isFetching
              }
              onClick={() =>
                setRecordFilters((current) => ({
                  ...current,
                  page: Math.min(current.page + 1, recordsQuery.data?.totalPages ?? current.page + 1)
                }))
              }
            >
              下一页
            </Button>
          </div>
        </div>
      </Section>

      <Separator />

      <Section title="基础配置">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <Toggle
            label="启用签到奖励"
            checked={draft.enabled}
            onChange={(value) => patchDraft("enabled", value)}
          />
          <Toggle
            label="应用等级经验倍率"
            checked={draft.applyLevelExperienceMultiplier}
            onChange={(value) => patchDraft("applyLevelExperienceMultiplier", value)}
          />
          <Field label="时区">
            <Input
              className="h-8 text-sm"
              value={draft.timezone}
              onChange={(event) => patchDraft("timezone", event.target.value)}
            />
          </Field>
          <NumberField label="基础积分" value={draft.baseIntegral} onChange={(value) => patchDraft("baseIntegral", value)} />
          <NumberField
            label="基础经验"
            value={draft.baseExperience}
            onChange={(value) => patchDraft("baseExperience", value)}
          />
          <NumberField
            label="首签经验加成"
            value={draft.firstSignInExperienceBonus}
            onChange={(value) => patchDraft("firstSignInExperienceBonus", value)}
          />
          <NumberField
            label="连签经验步进"
            value={draft.consecutiveExperienceStep}
            onChange={(value) => patchDraft("consecutiveExperienceStep", value)}
          />
          <NumberField
            label="连签经验上限"
            value={draft.consecutiveExperienceStepCap}
            onChange={(value) => patchDraft("consecutiveExperienceStepCap", value)}
          />
          <NumberField
            label="积分奖励上限"
            value={draft.maxIntegralReward}
            onChange={(value) => patchDraft("maxIntegralReward", value)}
          />
          <NumberField
            label="经验奖励上限"
            value={draft.maxExperienceReward}
            onChange={(value) => patchDraft("maxExperienceReward", value)}
          />
        </div>
      </Section>

      <Separator />

      <Section title="模板">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <Field label="模板">
            <Select value={selectedTemplate} onValueChange={setSelectedTemplate}>
              <SelectTrigger className="h-8 w-full min-w-44 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {templateOptions.map((item) => (
                  <SelectItem key={item.key} value={item.key}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Button
            variant="outline"
            className="h-8 gap-1 text-xs"
            disabled={templatesQuery.isLoading || saveMutation.isPending || !selectedTemplatePolicy}
            onClick={() => void applyTemplate()}
          >
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Sparkles className="size-3.5" />}
            {saveMutation.isPending ? "应用中..." : "应用模板"}
          </Button>
          {templatesQuery.isError ? (
            <Button
              variant="ghost"
              className="h-8 px-2 text-xs"
              onClick={() => void templatesQuery.refetch()}
            >
              重试加载
            </Button>
          ) : null}
        </div>
        {templatesQuery.isError ? (
          <div className="text-xs text-destructive">
            模板加载失败：{templatesQuery.error instanceof ApiError ? templatesQuery.error.message : "请求失败"}
          </div>
        ) : null}
      </Section>

      <Separator />

      <Section
        title="规则"
        action={
          <Button variant="outline" className="h-8 gap-1 text-xs" onClick={addRule}>
            <Plus className="size-3.5" />
            新增规则
          </Button>
        }
      >
        <ScrollArea className="max-h-[32rem] rounded-xl border">
          <div className="space-y-3 p-3">
            {sortedRules.length === 0 ? (
              <EmptyBlock>暂无规则</EmptyBlock>
            ) : (
              sortedRules.map(({ rule, index }) => (
                <div key={`${rule.key}-${index}`} className="space-y-3 rounded-xl border p-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-3">
                      <Toggle
                        label="启用"
                        checked={rule.enabled}
                        onChange={(value) => patchRule(index, { enabled: value })}
                        compact
                      />
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">{rule.name || rule.key}</div>
                        <div className="font-mono text-[11px] text-muted-foreground">{rule.key}</div>
                      </div>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 gap-1 text-xs text-destructive hover:text-destructive"
                      onClick={() => removeRule(index)}
                    >
                      <Trash2 className="size-3.5" />
                      删除
                    </Button>
                  </div>
                  <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                    <Field label="Key">
                      <Input
                        className="h-8 text-sm"
                        value={rule.key}
                        onChange={(event) => patchRule(index, { key: event.target.value })}
                      />
                    </Field>
                    <Field label="名称">
                      <Input
                        className="h-8 text-sm"
                        value={rule.name}
                        onChange={(event) => patchRule(index, { name: event.target.value })}
                      />
                    </Field>
                    <Field label="分组">
                      <Input
                        className="h-8 text-sm"
                        value={rule.group ?? ""}
                        onChange={(event) => patchRule(index, { group: event.target.value })}
                      />
                    </Field>
                    <NumberField
                      label="优先级"
                      value={rule.priority}
                      onChange={(value) => patchRule(index, { priority: value })}
                    />
                    <Field label="奖励类型">
                      <Input
                        className="h-8 text-sm"
                        value={rule.bonusType ?? ""}
                        onChange={(event) => patchRule(index, { bonusType: event.target.value })}
                      />
                    </Field>
                    <Field label="奖励说明">
                      <Input
                        className="h-8 text-sm"
                        value={rule.bonusDescription ?? ""}
                        onChange={(event) => patchRule(index, { bonusDescription: event.target.value })}
                      />
                    </Field>
                    <NumberField
                      label="积分倍率增量"
                      value={rule.integralMultiplierDelta}
                      step="0.1"
                      onChange={(value) => patchRule(index, { integralMultiplierDelta: value })}
                    />
                    <NumberField
                      label="积分加成"
                      value={rule.integralBonus}
                      onChange={(value) => patchRule(index, { integralBonus: value })}
                    />
                    <NumberField
                      label="经验倍率增量"
                      value={rule.experienceMultiplierDelta}
                      step="0.1"
                      onChange={(value) => patchRule(index, { experienceMultiplierDelta: value })}
                    />
                    <NumberField
                      label="经验加成"
                      value={rule.experienceBonus}
                      onChange={(value) => patchRule(index, { experienceBonus: value })}
                    />
                  </div>
                  <Field label="表达式">
                    <Textarea
                      className="min-h-24 font-mono text-xs"
                      value={rule.expression}
                      onChange={(event) => patchRule(index, { expression: event.target.value })}
                    />
                  </Field>
                  <Field label="描述">
                    <Textarea
                      className="min-h-20 text-sm"
                      value={rule.description ?? ""}
                      onChange={(event) => patchRule(index, { description: event.target.value })}
                    />
                  </Field>
                </div>
              ))
            )}
          </div>
        </ScrollArea>
      </Section>

      <Separator />

      <Section
        title="里程碑"
        action={
          <Button variant="outline" className="h-8 gap-1 text-xs" onClick={addMilestone}>
            <Plus className="size-3.5" />
            新增里程碑
          </Button>
        }
      >
        <div className="space-y-3">
          {sortedMilestones.length === 0 ? (
            <EmptyBlock>暂无里程碑</EmptyBlock>
          ) : (
            sortedMilestones.map(({ milestone, index }) => (
              <div key={`${milestone.consecutiveDays}-${index}`} className="grid gap-3 rounded-xl border p-4 md:grid-cols-2 xl:grid-cols-5">
                <NumberField
                  label="连续天数"
                  value={milestone.consecutiveDays}
                  onChange={(value) => patchMilestone(index, { consecutiveDays: value })}
                />
                <NumberField
                  label="积分加成"
                  value={milestone.integralBonus}
                  onChange={(value) => patchMilestone(index, { integralBonus: value })}
                />
                <NumberField
                  label="经验加成"
                  value={milestone.experienceBonus}
                  onChange={(value) => patchMilestone(index, { experienceBonus: value })}
                />
                <Field label="奖励类型">
                  <Input
                    className="h-8 text-sm"
                    value={milestone.bonusType ?? ""}
                    onChange={(event) => patchMilestone(index, { bonusType: event.target.value })}
                  />
                </Field>
                <div className="flex items-end">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 gap-1 text-xs text-destructive hover:text-destructive"
                    onClick={() => removeMilestone(index)}
                  >
                    <Trash2 className="size-3.5" />
                    删除
                  </Button>
                </div>
                <div className="md:col-span-2 xl:col-span-5">
                  <Field label="说明">
                    <Input
                      className="h-8 text-sm"
                      value={milestone.description ?? ""}
                      onChange={(event) => patchMilestone(index, { description: event.target.value })}
                    />
                  </Field>
                </div>
              </div>
            ))
          )}
        </div>
      </Section>

      <Separator />

      <Section title="测试">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <Field label="时间">
            <Input
              type="datetime-local"
              className="h-8 text-sm"
              value={previewForm.occurredAt}
              onChange={(event) => setPreviewForm((current) => ({ ...current, occurredAt: event.target.value }))}
            />
          </Field>
          <NumberField
            label="连续天数"
            value={previewForm.consecutiveDays}
            onChange={(value) => setPreviewForm((current) => ({ ...current, consecutiveDays: value }))}
          />
          <NumberField
            label="累计签到"
            value={previewForm.totalSignIns}
            onChange={(value) => setPreviewForm((current) => ({ ...current, totalSignIns: value }))}
          />
          <NumberField
            label="用户经验"
            value={previewForm.userExperience}
            onChange={(value) => setPreviewForm((current) => ({ ...current, userExperience: value }))}
          />
        </div>
        <Button className="h-8 gap-1 text-xs" disabled={testMutation.isPending} onClick={handleTest}>
          {testMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <TestTubeDiagonal className="size-3.5" />}
          {testMutation.isPending ? "测试中..." : "测试"}
        </Button>
        {previewResult ? (
          <div className="space-y-4 rounded-xl border p-4">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <ReadOnlyField label="积分奖励" value={String(previewResult.reward.integralReward)} />
              <ReadOnlyField label="经验奖励" value={String(previewResult.reward.experienceReward)} />
              <ReadOnlyField label="倍率" value={String(previewResult.reward.rewardMultiplier)} />
              <ReadOnlyField label="奖励类型" value={previewResult.reward.bonusType || "-"} />
            </div>
            <div className="space-y-2">
              <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">命中规则</Label>
              {previewResult.appliedRules.length === 0 ? (
                <EmptyBlock>未命中附加规则</EmptyBlock>
              ) : (
                <div className="space-y-2">
                  {previewResult.appliedRules.map((item, index) => (
                    <div key={`${item.key}-${index}`} className="rounded-lg border px-3 py-2">
                      <div className="text-sm font-medium">{item.name}</div>
                      <div className="text-xs text-muted-foreground">
                        {item.key}
                        {item.description ? ` · ${item.description}` : ""}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div className="space-y-2">
              <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">环境变量</Label>
              <pre className="overflow-x-auto rounded-xl border bg-muted/30 p-3 text-xs leading-6">
                {JSON.stringify(previewResult.environment, null, 2)}
              </pre>
            </div>
          </div>
        ) : null}
      </Section>

      <Separator />

      <div className="flex flex-wrap gap-2">
        <Button
          className="h-8 gap-1 text-xs"
          disabled={saveMutation.isPending || resetMutation.isPending}
          onClick={handleSave}
        >
          {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          {saveMutation.isPending ? "保存中..." : "保存"}
        </Button>
        <Button
          variant="outline"
          className="h-8 gap-1 text-xs"
          disabled={saveMutation.isPending || resetMutation.isPending}
          onClick={() => setResetOpen(true)}
        >
          <RotateCcw className="size-3.5" />
          重置为默认
        </Button>
      </div>

      <Dialog open={resetOpen} onOpenChange={setResetOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>重置签到奖励</DialogTitle>
            <DialogDescription>会覆盖当前未保存的本地修改，并恢复服务端默认模板。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setResetOpen(false)}>
              取消
            </Button>
            <Button className="gap-1" disabled={resetMutation.isPending} onClick={handleReset}>
              {resetMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <RotateCcw className="size-4" />}
              {resetMutation.isPending ? "重置中..." : "确认重置"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Section({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{title}</h3>
        {action}
      </div>
      {children}
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function ReadOnlyField({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm font-medium">{value}</div>
    </div>
  );
}

function MetricCard({
  label,
  value,
  loading,
  fractionDigits
}: {
  label: string;
  value?: number | null;
  loading?: boolean;
  fractionDigits?: number;
}) {
  const displayValue =
    typeof value === "number"
      ? value.toLocaleString("zh-CN", {
          minimumFractionDigits: fractionDigits ?? 0,
          maximumFractionDigits: fractionDigits ?? 0
        })
      : "0";
  return (
    <div className="rounded-xl border px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-16 rounded" />
      ) : (
        <div className="mt-0.5 text-lg font-semibold tabular-nums">{displayValue}</div>
      )}
    </div>
  );
}

function SignInRecordRow({ item }: { item: AppSignInRecordItem }) {
  return (
    <TableRow>
      <TableCell>
        <div className="space-y-0.5">
          <div className="font-medium">{item.nickname || item.account}</div>
          <div className="text-xs text-muted-foreground">{item.account}</div>
          {item.email ? <div className="text-xs text-muted-foreground">{item.email}</div> : null}
        </div>
      </TableCell>
      <TableCell>
        <div className="space-y-0.5">
          <div>{item.signDate}</div>
          <div className="text-xs text-muted-foreground">{formatDateTime(item.signedAt)}</div>
        </div>
      </TableCell>
      <TableCell>
        <div className="space-y-0.5">
          <div className="tabular-nums">积分 +{item.integralReward.toLocaleString()}</div>
          <div className="text-xs text-muted-foreground tabular-nums">
            经验 +{item.experienceReward.toLocaleString()} / 倍率 {item.rewardMultiplier}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <div className="tabular-nums">{item.consecutiveDays.toLocaleString()} 天</div>
      </TableCell>
      <TableCell>
        <div className="space-y-0.5">
          <div>{item.signInSource || "manual"}</div>
          {item.deviceInfo ? <div className="line-clamp-1 max-w-48 text-xs text-muted-foreground">{item.deviceInfo}</div> : null}
        </div>
      </TableCell>
      <TableCell>
        <div className="space-y-0.5">
          <div>{item.ipAddress || "-"}</div>
          <div className="line-clamp-1 max-w-48 text-xs text-muted-foreground">{item.location || "-"}</div>
        </div>
      </TableCell>
      <TableCell>
        <div className="space-y-0.5">
          <div>{item.bonusType || "-"}</div>
          <div className="line-clamp-2 max-w-56 text-xs text-muted-foreground">{item.bonusDescription || "-"}</div>
        </div>
      </TableCell>
    </TableRow>
  );
}

function Toggle({
  label,
  checked,
  onChange,
  compact
}: {
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  compact?: boolean;
}) {
  return (
    <label
      className={compact ? "flex cursor-pointer items-center gap-2" : "flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5"}
    >
      <Checkbox checked={checked} onCheckedChange={(value) => onChange(value === true)} />
      <span className={compact ? "text-xs" : "text-sm"}>{label}</span>
    </label>
  );
}

function NumberField({
  label,
  value,
  onChange,
  step
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
  step?: string;
}) {
  return (
    <Field label={label}>
      <Input
        type="number"
        step={step}
        className="h-8 text-sm"
        value={String(value ?? 0)}
        onChange={(event) => onChange(Number(event.target.value) || 0)}
      />
    </Field>
  );
}

function EmptyBlock({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">{children}</div>;
}
