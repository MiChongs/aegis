"use client";

import { useMemo } from "react";
import { useNodeRender, type WorkflowNodeEntity } from "@flowgram.ai/free-layout-editor";
import { Copy, Plus, Trash2, X } from "lucide-react";
import {
  Collapsible,
  Field,
  FieldGroup,
  ItemListEditor,
  NumberInput
} from "@/components/apps/item-grant/item-grant-fields";
import { shortId, defaultItem } from "@/components/apps/item-grant/item-grant-graph";
import {
  NODE_VISUAL,
  WEEKDAYS,
  minuteLabel,
  parseMinute,
  rulesExpr
} from "@/components/apps/item-grant/item-grant-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
  ItemGrantAudienceData,
  ItemGrantCatalog,
  ItemGrantConditionData,
  ItemGrantConditionRule,
  ItemGrantFieldSpec,
  ItemGrantGrantData,
  ItemGrantIssue,
  ItemGrantLimitData,
  ItemGrantNodeType,
  ItemGrantNotifyData,
  ItemGrantRandomData,
  ItemGrantSplitData,
  ItemGrantTriggerData,
  ItemGrantWindowData
} from "@/lib/api/item-grant";
import { cn } from "@/lib/utils";

/**
 * 节点检查器：选中一个节点时右侧显示它的配置表单。每种节点一张表单，字段与取值范围来自后端目录。
 * 改动立刻写回节点表单（Flowgram 的撤销历史记录每次变更）。
 */

const CRON_PRESETS = [
  { label: "每天 10:00", cron: "0 10 * * *" },
  { label: "每天 20:00", cron: "0 20 * * *" },
  { label: "每周一 10:00", cron: "0 10 * * 1" },
  { label: "每月 1 号 10:00", cron: "0 10 1 * *" },
  { label: "每小时整点", cron: "0 * * * *" }
];

export function NodeInspector({
  node,
  catalog,
  issues,
  triggerEvent,
  onDuplicate,
  onDelete,
  onRemoveBranch
}: {
  node: WorkflowNodeEntity;
  catalog: ItemGrantCatalog;
  issues: ItemGrantIssue[];
  /** 触发器是事件触发时的事件键（条件里可以引用事件字段） */
  triggerEvent?: string;
  onDuplicate: () => void;
  onDelete: () => void;
  /** 随机分流删分支时，先删掉从这个分支端口连出去的线 */
  onRemoveBranch: (branchId: string) => void;
}) {
  // data 是节点表单的当前值；updateData 整体写回（Flowgram 的撤销历史记录这次变更）
  const { data: rawData, updateData } = useNodeRender(node);
  const type = node.flowNodeType as ItemGrantNodeType;
  const spec = catalog.nodes.find((candidate) => candidate.type === type);
  const visual = NODE_VISUAL[type];
  const data = (rawData ?? {}) as Record<string, unknown>;
  const patch = (next: Record<string, unknown>) => updateData({ ...data, ...next });

  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={visual.icon} tone={visual.tone} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold">{spec?.label ?? type}</div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{spec?.hint}</p>
        </div>
        <div className="flex shrink-0 gap-1">
          {type !== "trigger" ? (
            <Button size="icon" variant="ghost" className="size-7" onClick={onDuplicate} aria-label="复制节点">
              <Copy className="size-3.5" />
            </Button>
          ) : null}
          <Button
            size="icon"
            variant="ghost"
            className="size-7 hover:text-destructive"
            onClick={onDelete}
            aria-label="删除节点"
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {issues.length ? (
        <div className="space-y-1.5">
          {issues.map((issue, index) => (
            <div
              key={index}
              className={cn(
                "rounded-lg px-2.5 py-1.5 text-xs leading-5",
                issue.level === "error"
                  ? "bg-red-500/10 text-red-700 dark:text-red-300"
                  : "bg-amber-500/10 text-amber-700 dark:text-amber-300"
              )}
            >
              {issue.message}
            </div>
          ))}
        </div>
      ) : null}

      <FieldGroup title="显示">
        <div className="grid gap-3">
          <Field label="节点名称">
            <Input
              className="h-8"
              value={(data.label as string) ?? ""}
              maxLength={40}
              placeholder={spec?.label}
              onChange={(event) => patch({ label: event.target.value })}
            />
          </Field>
          <Field label="备注">
            <Textarea
              className="min-h-14 text-xs"
              value={(data.note as string) ?? ""}
              maxLength={200}
              placeholder="写给团队看的说明，不影响执行"
              onChange={(event) => patch({ note: event.target.value })}
            />
          </Field>
        </div>
      </FieldGroup>

      {type === "trigger" ? <TriggerForm data={data as ItemGrantTriggerData} catalog={catalog} patch={patch} /> : null}
      {type === "audience" ? <AudienceForm data={data as ItemGrantAudienceData} catalog={catalog} patch={patch} /> : null}
      {type === "condition" ? (
        <ConditionForm data={data as ItemGrantConditionData} catalog={catalog} patch={patch} triggerEvent={triggerEvent} />
      ) : null}
      {type === "window" ? <WindowForm data={data as ItemGrantWindowData} patch={patch} /> : null}
      {type === "split" ? (
        <SplitForm
          data={data as ItemGrantSplitData}
          patch={patch}
          onRemoveBranch={onRemoveBranch}
        />
      ) : null}
      {type === "limit" ? <LimitForm data={data as ItemGrantLimitData} catalog={catalog} patch={patch} /> : null}
      {type === "grant" ? (
        <FieldGroup title="物品">
          <ItemListEditor
            catalog={catalog}
            items={(data as ItemGrantGrantData).items ?? []}
            onChange={(items) => updateData({ ...data, items })}
          />
        </FieldGroup>
      ) : null}
      {type === "random" ? <RandomForm data={data as ItemGrantRandomData} catalog={catalog} patch={patch} /> : null}
      {type === "notify" ? <NotifyForm data={data as ItemGrantNotifyData} catalog={catalog} patch={patch} /> : null}
    </div>
  );
}

export type FormProps<T> = { data: T; patch: (next: Record<string, unknown>) => void };

function TriggerForm({ data, catalog, patch }: FormProps<ItemGrantTriggerData> & { catalog: ItemGrantCatalog }) {
  const event = catalog.events.find((candidate) => candidate.key === data.event);
  return (
    <FieldGroup title="触发方式">
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        className="w-full"
        value={data.kind}
        onValueChange={(kind) => kind && patch({ kind })}
      >
        {catalog.triggers.map((trigger) => (
          <ToggleGroupItem key={trigger.kind} value={trigger.kind} className="flex-1 text-xs">
            {trigger.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <p className="text-[11px] leading-4 text-muted-foreground">
        {catalog.triggers.find((trigger) => trigger.kind === data.kind)?.hint}
      </p>
      {data.kind === "schedule" ? (
        <Field
          label="Cron 表达式"
          hint={`分 时 日 月 周，按应用时区（${catalog.timezone}）；相邻两次至少间隔 ${catalog.limits.minCronGapMinutes ?? 10} 分钟`}
        >
          <Input
            className="h-8 font-mono text-xs"
            value={data.cron ?? ""}
            placeholder="0 10 * * *"
            onChange={(eventInput) => patch({ cron: eventInput.target.value })}
          />
          <div className="flex flex-wrap gap-1.5 pt-1">
            {CRON_PRESETS.map((preset) => (
              <button
                key={preset.cron}
                type="button"
                className={cn(
                  "rounded-md border px-2 py-0.5 text-[11px] transition-colors hover:bg-accent",
                  data.cron === preset.cron && "border-primary bg-primary/5 text-primary"
                )}
                onClick={() => patch({ cron: preset.cron })}
              >
                {preset.label}
              </button>
            ))}
          </div>
        </Field>
      ) : null}
      {data.kind === "event" ? (
        <Field label="事件" hint={event?.hint}>
          <Select value={data.event || undefined} onValueChange={(value) => patch({ event: value })}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue placeholder="选择事件" />
            </SelectTrigger>
            <SelectContent position="popper">
              {catalog.events.map((candidate) => (
                <SelectItem key={candidate.key} value={candidate.key}>
                  {candidate.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {event?.fields.length ? (
            <div className="flex flex-wrap gap-1 pt-1">
              {event.fields.map((field) => (
                <Badge key={field.key} variant="outline" size="sm" className="font-mono">
                  {field.key}
                </Badge>
              ))}
            </div>
          ) : null}
        </Field>
      ) : null}
    </FieldGroup>
  );
}

function parseIds(text: string) {
  return Array.from(
    new Set(
      text
        .split(/[\s,，;；]+/)
        .map((part) => Number.parseInt(part, 10))
        .filter((id) => Number.isFinite(id) && id > 0)
    )
  );
}

function IdListField({ label, hint, value, onChange }: { label: string; hint: string; value?: number[]; onChange: (ids: number[]) => void }) {
  return (
    <Field label={`${label}${value?.length ? `（${value.length} 人）` : ""}`} hint={hint}>
      <Textarea
        className="min-h-16 font-mono text-xs"
        defaultValue={(value ?? []).join("\n")}
        placeholder="每行一个用户 ID，也可以用逗号分隔"
        onBlur={(event) => onChange(parseIds(event.target.value))}
      />
    </Field>
  );
}

function AudienceForm({ data, catalog, patch }: FormProps<ItemGrantAudienceData> & { catalog: ItemGrantCatalog }) {
  return (
    <>
      <FieldGroup title="人群">
        <Field label="会员身份">
          <Select value={data.scope || "all"} onValueChange={(scope) => patch({ scope })}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              {catalog.scopes.map((scope) => (
                <SelectItem key={scope.key} value={scope.key}>
                  {scope.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="注册不超过" hint="0 不限">
            <NumberInput value={data.registeredWithinDays ?? 0} min={0} onChange={(value) => patch({ registeredWithinDays: value })} suffix="天" />
          </Field>
          <Field label="注册满" hint="0 不限">
            <NumberInput value={data.registeredBeforeDays ?? 0} min={0} onChange={(value) => patch({ registeredBeforeDays: value })} suffix="天" />
          </Field>
          <Field label="最近活跃" hint="N 天内登录或在线">
            <NumberInput value={data.activeWithinDays ?? 0} min={0} onChange={(value) => patch({ activeWithinDays: value })} suffix="天" />
          </Field>
          <div className="grid grid-cols-2 gap-1.5">
            <Field label="最低等级">
              <NumberInput value={data.minLevel ?? 0} min={0} onChange={(value) => patch({ minLevel: value })} />
            </Field>
            <Field label="最高等级">
              <NumberInput value={data.maxLevel ?? 0} min={0} onChange={(value) => patch({ maxLevel: value })} />
            </Field>
          </div>
        </div>
      </FieldGroup>
      {catalog.tiers?.length ? (
        <FieldGroup title="会员档位">
          <Field label="持有档位" hint="选了之后，只有此刻持有其中任一档位的人命中；不选为不限">
            <ToggleGroup
              type="multiple"
              variant="outline"
              size="sm"
              className="flex-wrap justify-start"
              value={(data.tierIds ?? []).map(String)}
              onValueChange={(values) =>
                patch({ tierIds: values.map(Number), tierLifetimeOnly: values.length ? data.tierLifetimeOnly : false })
              }
            >
              {catalog.tiers.map((tier) => (
                <ToggleGroupItem key={tier.id} value={String(tier.id)} className="px-2.5 text-xs">
                  {tier.name}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
          <div className="flex items-center justify-between rounded-lg border px-3 py-2">
            <div>
              <div className="text-xs font-medium">只认永久持有</div>
              <div className="text-[11px] text-muted-foreground">限时持有同一档位的人不算</div>
            </div>
            <Switch
              checked={Boolean(data.tierLifetimeOnly)}
              disabled={!data.tierIds?.length}
              onCheckedChange={(tierLifetimeOnly) => patch({ tierLifetimeOnly })}
            />
          </div>
        </FieldGroup>
      ) : null}
      <FieldGroup title="名单">
        <IdListField
          label="只发给"
          hint={`填写后只有名单里的人会命中（管理端用户 ID，最多 ${catalog.limits.maxAudienceUsers ?? 5000} 人）`}
          value={data.userIds}
          onChange={(userIds) => patch({ userIds })}
        />
        <IdListField label="排除" hint="名单里的人一律不命中" value={data.excludeUserIds} onChange={(excludeUserIds) => patch({ excludeUserIds })} />
      </FieldGroup>
    </>
  );
}

function groupFields(fields: ItemGrantFieldSpec[]) {
  const groups = new Map<string, ItemGrantFieldSpec[]>();
  for (const field of fields) groups.set(field.group, [...(groups.get(field.group) ?? []), field]);
  return Array.from(groups.entries());
}

/** 条件构建器（可视化规则 / 表达式）。等级体系的晋级条件也用它，`newRule` 是「添加条件」时的默认一行。 */
export function ConditionForm({
  data,
  catalog,
  patch,
  triggerEvent,
  title = "条件",
  newRule = { field: "user.level", op: "gte", value: [1] },
  exprPlaceholder = "user.signStreak >= 7 && now.weekday in [6, 7]"
}: FormProps<ItemGrantConditionData> & {
  catalog: ItemGrantCatalog;
  triggerEvent?: string;
  title?: string;
  newRule?: ItemGrantConditionRule;
  exprPlaceholder?: string;
}) {
  const eventFields = new Set(catalog.events.find((event) => event.key === triggerEvent)?.fields.map((field) => field.key) ?? []);
  const fields = catalog.fields.filter((field) => !field.key.startsWith("event.") || eventFields.has(field.key));
  const grouped = useMemo(() => groupFields(fields), [fields]);
  const rules = data.rules ?? [];
  const setRules = (next: ItemGrantConditionRule[]) => patch({ rules: next });
  const generated = data.mode === "expr" ? "" : rulesExpr(data.mode, rules);

  return (
    <FieldGroup title={title}>
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        className="w-full"
        value={data.mode}
        onValueChange={(mode) => {
          if (!mode) return;
          if (mode === "expr" && data.mode !== "expr") patch({ mode, expr: data.expr || rulesExpr(data.mode, rules) });
          else patch({ mode });
        }}
      >
        <ToggleGroupItem value="all" className="flex-1 text-xs">
          全部满足
        </ToggleGroupItem>
        <ToggleGroupItem value="any" className="flex-1 text-xs">
          任一满足
        </ToggleGroupItem>
        <ToggleGroupItem value="expr" className="flex-1 text-xs">
          表达式
        </ToggleGroupItem>
      </ToggleGroup>

      {data.mode !== "expr" ? (
        <div className="space-y-2">
          {rules.map((rule, index) => (
            <RuleRow
              key={index}
              rule={rule}
              catalog={catalog}
              grouped={grouped}
              onChange={(next) => setRules(rules.map((item, i) => (i === index ? next : item)))}
              onRemove={() => setRules(rules.filter((_, i) => i !== index))}
            />
          ))}
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
            disabled={rules.length >= (catalog.limits.maxConditionRules ?? 20)}
            onClick={() => setRules([...rules, newRule])}
          >
            <Plus className="size-3.5" /> 添加条件
          </Button>
          {generated ? (
            <div className="rounded-lg bg-muted/60 px-2.5 py-2">
              <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">等价表达式</div>
              <code className="block break-all font-mono text-[11px] leading-4">{generated}</code>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="space-y-2">
          <Textarea
            className="min-h-28 font-mono text-xs"
            value={data.expr ?? ""}
            spellCheck={false}
            placeholder={exprPlaceholder}
            onChange={(event) => patch({ expr: event.target.value })}
          />
          <p className="text-[11px] leading-4 text-muted-foreground">
            expr 语法：<code className="font-mono">&& || ! in ?? </code>与比较运算；结果必须是布尔值。点下面的字段插入。
          </p>
          <div className="max-h-48 space-y-2 overflow-y-auto rounded-lg border p-2">
            {grouped.map(([group, items]) => (
              <div key={group}>
                <div className="mb-1 text-[10px] font-semibold text-muted-foreground">{group}</div>
                <div className="flex flex-wrap gap-1">
                  {items.map((field) => (
                    <button
                      key={field.key}
                      type="button"
                      title={field.label}
                      className="rounded border bg-background px-1.5 py-0.5 font-mono text-[10px] transition-colors hover:border-primary hover:text-primary"
                      onClick={() => patch({ expr: `${data.expr ? `${data.expr} ` : ""}${field.key}` })}
                    >
                      {field.key.replace(/^(user|now|event)\./, "")}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </FieldGroup>
  );
}

function RuleRow({
  rule,
  catalog,
  grouped,
  onChange,
  onRemove
}: {
  rule: ItemGrantConditionRule;
  catalog: ItemGrantCatalog;
  grouped: Array<[string, ItemGrantFieldSpec[]]>;
  onChange: (rule: ItemGrantConditionRule) => void;
  onRemove: () => void;
}) {
  const field = catalog.fields.find((candidate) => candidate.key === rule.field);
  const operators = catalog.operators.filter((op) => !field || op.kinds.includes(field.kind));
  const op = catalog.operators.find((candidate) => candidate.op === rule.op);
  const values = rule.value ?? [];

  return (
    <div className="space-y-1.5 rounded-xl border bg-muted/20 p-2">
      <div className="flex items-center gap-1.5">
        <Select
          value={rule.field}
          onValueChange={(key) => {
            const next = catalog.fields.find((candidate) => candidate.key === key);
            const keepOp = operators.find((candidate) => candidate.op === rule.op && next && candidate.kinds.includes(next.kind));
            if (next?.kind === "bool") onChange({ field: key, op: keepOp?.op ?? "true" });
            else onChange({ field: key, op: keepOp?.op ?? "gte", value: keepOp ? values : [0] });
          }}
        >
          <SelectTrigger size="sm" className="h-8 min-w-0 flex-1 text-xs">
            <SelectValue placeholder="字段" />
          </SelectTrigger>
          <SelectContent position="popper" className="max-h-80">
            {grouped.map(([group, items]) => (
              <SelectGroup key={group}>
                <SelectLabel className="text-[10px]">{group}</SelectLabel>
                {items.map((item) => (
                  <SelectItem key={item.key} value={item.key}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
        <Button size="icon" variant="ghost" className="size-7 shrink-0" onClick={onRemove} aria-label="移除条件">
          <X className="size-3.5" />
        </Button>
      </div>
      <div className="flex items-center gap-1.5">
        <Select
          value={rule.op}
          onValueChange={(next) => {
            const nextOp = catalog.operators.find((candidate) => candidate.op === next);
            const arity = nextOp?.arity ?? 1;
            const nextValues = arity === 0 ? undefined : arity === 2 ? [values[0] ?? 0, values[1] ?? values[0] ?? 0] : arity < 0 ? values.length ? values : [0] : [values[0] ?? 0];
            onChange({ ...rule, op: next, value: nextValues });
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-28 shrink-0 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper">
            {operators.map((candidate) => (
              <SelectItem key={candidate.op} value={candidate.op}>
                {candidate.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {op?.arity === 1 ? (
          <NumberInput decimal className="min-w-0 flex-1" value={values[0]} onChange={(value) => onChange({ ...rule, value: [value] })} suffix={field?.unit} />
        ) : null}
        {op?.arity === 2 ? (
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <NumberInput decimal className="min-w-0 flex-1" value={values[0]} onChange={(value) => onChange({ ...rule, value: [value, values[1] ?? value] })} />
            <span className="text-xs text-muted-foreground">至</span>
            <NumberInput decimal className="min-w-0 flex-1" value={values[1]} onChange={(value) => onChange({ ...rule, value: [values[0] ?? 0, value] })} />
          </div>
        ) : null}
        {op && op.arity < 0 ? (
          <Input
            className="h-8 min-w-0 flex-1 font-mono text-xs"
            defaultValue={values.join(", ")}
            placeholder="7, 14, 30"
            onBlur={(event) =>
              onChange({
                ...rule,
                value: event.target.value
                  .split(/[\s,，]+/)
                  .map(Number)
                  .filter((value) => Number.isFinite(value))
              })
            }
          />
        ) : null}
      </div>
    </div>
  );
}

function WindowForm({ data, patch }: FormProps<ItemGrantWindowData>) {
  const weekdays = data.weekdays ?? [];
  const allDay = data.startMinute === data.endMinute;
  return (
    <FieldGroup title="时间窗口">
      <Field label="星期" hint="都不选为每天">
        <ToggleGroup
          type="multiple"
          variant="outline"
          size="sm"
          className="w-full"
          value={weekdays.map(String)}
          onValueChange={(values) => patch({ weekdays: values.map(Number).sort((a, b) => a - b) })}
        >
          {WEEKDAYS.map((label, index) => (
            <ToggleGroupItem key={label} value={String(index + 1)} className="flex-1 text-xs">
              {label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </Field>
      <Field label="每日时段" hint={allDay ? "开始与结束相同为全天" : data.startMinute > data.endMinute ? "跨零点" : undefined}>
        <div className="flex items-center gap-1.5">
          <Input
            type="time"
            className="h-8 flex-1"
            value={minuteLabel(data.startMinute ?? 0)}
            onChange={(event) => patch({ startMinute: parseMinute(event.target.value) })}
          />
          <span className="text-xs text-muted-foreground">至</span>
          <Input
            type="time"
            className="h-8 flex-1"
            value={minuteLabel(data.endMinute ?? 0)}
            onChange={(event) => patch({ endMinute: parseMinute(event.target.value) })}
          />
        </div>
      </Field>
      <Field label="日期范围" hint="含两端，空为不限">
        <div className="flex items-center gap-1.5">
          <Input type="date" className="h-8 flex-1" value={data.dateFrom ?? ""} onChange={(event) => patch({ dateFrom: event.target.value })} />
          <span className="text-xs text-muted-foreground">至</span>
          <Input type="date" className="h-8 flex-1" value={data.dateTo ?? ""} onChange={(event) => patch({ dateTo: event.target.value })} />
        </div>
      </Field>
    </FieldGroup>
  );
}

function WeightBar({ weights, labels }: { weights: number[]; labels: string[] }) {
  const total = weights.reduce((sum, weight) => sum + Math.max(0, weight), 0) || 1;
  const colors = ["#8b5cf6", "#0ea5e9", "#10b981", "#f59e0b", "#f43f5e", "#14b8a6", "#6366f1", "#a855f7"];
  return (
    <div className="space-y-1.5">
      <div className="flex h-2 overflow-hidden rounded-full bg-muted">
        {weights.map((weight, index) => (
          <div
            key={index}
            className="h-full transition-[width] duration-200"
            style={{ width: `${(Math.max(0, weight) / total) * 100}%`, background: colors[index % colors.length] }}
          />
        ))}
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {labels.map((label, index) => (
          <span key={index} className="flex items-center gap-1 text-[11px] text-muted-foreground">
            <span className="size-2 rounded-full" style={{ background: colors[index % colors.length] }} />
            {label} {((Math.max(0, weights[index]) / total) * 100).toFixed(1)}%
          </span>
        ))}
      </div>
    </div>
  );
}

function SplitForm({ data, patch, onRemoveBranch }: FormProps<ItemGrantSplitData> & { onRemoveBranch: (id: string) => void }) {
  const branches = data.branches ?? [];
  const setBranches = (next: typeof branches) => patch({ branches: next });
  return (
    <FieldGroup title="分支">
      <div className="flex items-center justify-between rounded-lg border px-3 py-2">
        <div>
          <div className="text-xs font-medium">固定分配</div>
          <div className="text-[11px] text-muted-foreground">同一个人每次都进同一个分支（A/B 测试）</div>
        </div>
        <Switch checked={Boolean(data.sticky)} onCheckedChange={(sticky) => patch({ sticky })} />
      </div>
      <WeightBar weights={branches.map((branch) => branch.weight)} labels={branches.map((branch) => branch.label || branch.id)} />
      <div className="space-y-1.5">
        {branches.map((branch, index) => (
          <div key={branch.id} className="flex items-center gap-1.5">
            <Input
              className="h-8 min-w-0 flex-1 text-xs"
              value={branch.label}
              maxLength={20}
              onChange={(event) => setBranches(branches.map((item, i) => (i === index ? { ...item, label: event.target.value } : item)))}
            />
            <NumberInput
              className="w-28"
              value={branch.weight}
              min={0}
              decimal
              onChange={(weight) => setBranches(branches.map((item, i) => (i === index ? { ...item, weight } : item)))}
              suffix="权重"
            />
            <Button
              size="icon"
              variant="ghost"
              className="size-7 shrink-0"
              disabled={branches.length <= 2}
              onClick={() => {
                onRemoveBranch(branch.id);
                setBranches(branches.filter((_, i) => i !== index));
              }}
              aria-label="删除分支"
            >
              <X className="size-3.5" />
            </Button>
          </div>
        ))}
      </div>
      <Button
        variant="outline"
        size="sm"
        className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
        disabled={branches.length >= 8}
        onClick={() =>
          setBranches([...branches, { id: shortId(), label: `${String.fromCharCode(65 + branches.length)} 组`, weight: 50 }])
        }
      >
        <Plus className="size-3.5" /> 添加分支
      </Button>
    </FieldGroup>
  );
}

function LimitForm({ data, catalog, patch }: FormProps<ItemGrantLimitData> & { catalog: ItemGrantCatalog }) {
  return (
    <FieldGroup title="限额">
      <Field label="周期" hint="天、周、月按应用时区；周从周一开始">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          className="w-full"
          value={data.period}
          onValueChange={(period) => period && patch({ period })}
        >
          {catalog.periods.map((period) => (
            <ToggleGroupItem key={period.key} value={period.key} className="flex-1 text-xs">
              {period.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="每人最多" hint="0 不限">
          <NumberInput value={data.perUser ?? 0} min={0} onChange={(perUser) => patch({ perUser })} suffix="次" />
        </Field>
        <Field label="全站最多" hint="0 不限">
          <NumberInput value={data.global ?? 0} min={0} onChange={(global) => patch({ global })} suffix="人次" />
        </Field>
      </div>
      <p className="text-[11px] leading-4 text-muted-foreground">
        只数「经过这个节点并且最终发放成功」的次数；超出的人走「超出」端口，不连就到此为止。
      </p>
    </FieldGroup>
  );
}

function RandomForm({ data, catalog, patch }: FormProps<ItemGrantRandomData> & { catalog: ItemGrantCatalog }) {
  const options = data.options ?? [];
  const setOptions = (next: typeof options) => patch({ options: next });
  const total = options.reduce((sum, option) => sum + Math.max(0, option.weight), 0) || 1;
  return (
    <FieldGroup title="礼包选项">
      <Field label="抽取个数" hint="不放回：一次抽出的选项互不相同">
        <NumberInput value={data.picks ?? 1} min={1} max={options.length || 1} onChange={(picks) => patch({ picks })} suffix={`/ ${options.length} 个`} />
      </Field>
      <WeightBar weights={options.map((option) => option.weight)} labels={options.map((option) => option.label || option.id)} />
      <div className="space-y-2">
        {options.map((option, index) => (
          <Collapsible
            key={option.id}
            defaultOpen={options.length <= 3}
            title={
              <span className="flex items-center gap-1.5">
                {option.label || `选项 ${index + 1}`}
                <span className="text-[11px] font-normal tabular-nums text-muted-foreground">
                  {((Math.max(0, option.weight) / total) * 100).toFixed(1)}%
                </span>
              </span>
            }
            aside={
              <Button
                size="icon"
                variant="ghost"
                className="size-6"
                disabled={options.length <= 1}
                onClick={() => setOptions(options.filter((_, i) => i !== index))}
                aria-label="删除选项"
              >
                <X className="size-3.5" />
              </Button>
            }
          >
            <div className="space-y-2">
              <div className="grid grid-cols-[minmax(0,1fr)_7rem] gap-1.5">
                <Input
                  className="h-8 text-xs"
                  value={option.label}
                  maxLength={20}
                  placeholder="选项名称"
                  onChange={(event) => setOptions(options.map((item, i) => (i === index ? { ...item, label: event.target.value } : item)))}
                />
                <NumberInput
                  value={option.weight}
                  min={0}
                  decimal
                  onChange={(weight) => setOptions(options.map((item, i) => (i === index ? { ...item, weight } : item)))}
                  suffix="权重"
                />
              </div>
              <ItemListEditor
                catalog={catalog}
                items={option.items ?? []}
                onChange={(items) => setOptions(options.map((item, i) => (i === index ? { ...item, items } : item)))}
              />
            </div>
          </Collapsible>
        ))}
      </div>
      <Button
        variant="outline"
        size="sm"
        className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
        disabled={options.length >= (catalog.limits.maxRandomOptions ?? 20)}
        onClick={() =>
          setOptions([...options, { id: shortId(), label: `选项 ${options.length + 1}`, weight: 10, items: [defaultItem("points", catalog)] }])
        }
      >
        <Plus className="size-3.5" /> 添加选项
      </Button>
    </FieldGroup>
  );
}

const PLACEHOLDERS = [
  { key: "{items}", label: "本次物品" },
  { key: "{nickname}", label: "昵称" },
  { key: "{rule}", label: "规则名" }
];

function NotifyForm({ data, catalog, patch }: FormProps<ItemGrantNotifyData> & { catalog: ItemGrantCatalog }) {
  return (
    <FieldGroup title="站内信">
      <Field label="标题">
        <Input className="h-8" value={data.title ?? ""} maxLength={60} onChange={(event) => patch({ title: event.target.value })} />
      </Field>
      <Field label="内容">
        <Textarea
          className="min-h-20 text-xs"
          value={data.content ?? ""}
          maxLength={500}
          onChange={(event) => patch({ content: event.target.value })}
        />
        <div className="flex flex-wrap gap-1 pt-1">
          {PLACEHOLDERS.map((placeholder) => (
            <button
              key={placeholder.key}
              type="button"
              className="rounded border bg-background px-1.5 py-0.5 text-[11px] transition-colors hover:border-primary hover:text-primary"
              onClick={() => patch({ content: `${data.content ?? ""}${placeholder.key}` })}
            >
              <span className="font-mono">{placeholder.key}</span> {placeholder.label}
            </button>
          ))}
        </div>
      </Field>
      <Field label="点开后跳转" hint="App 内页面；不选为不跳转">
        <Select value={data.route || "none"} onValueChange={(route) => patch({ route: route === "none" ? "" : route })}>
          <SelectTrigger size="sm" className="h-8 w-full text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper">
            <SelectItem value="none">不跳转</SelectItem>
            {catalog.routes.map((route) => (
              <SelectItem key={route.key} value={route.key}>
                {route.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <p className="text-[11px] leading-4 text-muted-foreground">只在发放成功之后发送；没有走到发放节点的人收不到。</p>
    </FieldGroup>
  );
}
