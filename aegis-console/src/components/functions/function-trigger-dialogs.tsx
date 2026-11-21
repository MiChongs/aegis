"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, CalendarClock, Check, Loader2, Play, UserRound, Zap } from "lucide-react";
import { toast } from "sonner";
import type {
  AppFunction,
  AppFunctionTrigger,
  AppFunctionTriggerFireResult,
  AppFunctionTriggerKind,
  FunctionCatalog,
  FunctionTriggerEvent
} from "@/lib/api/app-functions";
import { useFireTriggerMutation, useSaveTriggerMutation } from "@/lib/function-hooks";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { CopyButton } from "@/components/users/detail/kit/surface";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import {
  CRON_PRESETS,
  DEFAULT_TIMEZONE,
  TIMEZONES,
  describeCron,
  formatInZone,
  nextCronRuns,
  parseCron
} from "./function-cron";
import { JsonEditor } from "./json-editor";
import { DetailSection, EffectList, JsonBlock, errorMessage } from "./function-shared";

/** 事件按后端给的分组聚合，保持目录顺序 */
export function groupTriggerEvents(events: FunctionTriggerEvent[]) {
  const map = new Map<string, FunctionTriggerEvent[]>();
  for (const event of events) {
    if (!map.has(event.group)) map.set(event.group, []);
    map.get(event.group)!.push(event);
  }
  return Array.from(map.entries());
}

/** 按事件字段造一份模拟数据：比一个空对象更接近脚本真正收到的东西 */
export function sampleEventData(spec?: FunctionTriggerEvent) {
  const data: Record<string, unknown> = {};
  for (const field of spec?.fields ?? []) {
    data[field.key] = field.type === "number" ? 0 : field.type === "boolean" ? false : "";
  }
  return JSON.stringify(data, null, 2);
}

function hasSchema(schema?: Record<string, unknown>) {
  return Boolean(schema && Object.keys((schema.properties as object) ?? {}).length > 0);
}

function parseObject(text: string): Record<string, unknown> | null {
  if (!text.trim()) return {};
  try {
    const value = JSON.parse(text);
    return value && typeof value === "object" && !Array.isArray(value) ? value : null;
  } catch {
    return null;
  }
}

/** 事件的数据字段：脚本里 ctx.trigger.data 上有什么，在挑事件时就该看见 */
export function EventFieldList({ spec }: { spec: FunctionTriggerEvent }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="flex items-center gap-2 border-b bg-muted/30 px-2.5 py-1.5 text-[11px]">
        <code className="font-mono">ctx.trigger.data</code>
        {spec.hasUser ? (
          <Badge variant="info" size="sm" className="ml-auto gap-1">
            <UserRound className="size-3" />
            以事件用户身份执行
          </Badge>
        ) : null}
      </div>
      {spec.fields.length ? (
        <div className="divide-y">
          {spec.fields.map((field) => (
            <div key={field.key} className="flex items-center gap-2 px-2.5 py-1.5 text-xs">
              <code className="font-mono text-[11px]">.{field.key}</code>
              <span className="text-muted-foreground">{field.label}</span>
              <Badge variant="outline" size="sm" className="ml-auto font-mono font-normal">
                {field.type}
              </Badge>
            </div>
          ))}
        </div>
      ) : (
        <p className="px-2.5 py-2 text-[11px] text-muted-foreground">不携带数据</p>
      )}
    </div>
  );
}

/**
 * 新建 / 编辑触发器。
 *
 * 调用方按触发器编号给 key 重挂载，表单初值只在挂载时读一次，不用 effect 同步。
 * 种类在编辑时不可改（后端也拒绝）：换种类等于换了一件事，删了重建更清楚。
 */
export function TriggerEditorDialog({
  appKey,
  selected,
  catalog,
  trigger,
  onClose
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  trigger: AppFunctionTrigger | null;
  onClose: () => void;
}) {
  const events = useMemo(() => catalog?.triggerEvents ?? [], [catalog]);
  const grouped = useMemo(() => groupTriggerEvents(events), [events]);
  const editing = Boolean(trigger);

  const [kind, setKind] = useState<AppFunctionTriggerKind>(trigger?.kind ?? "event");
  const [event, setEvent] = useState(trigger?.event ?? events[0]?.key ?? "");
  const [cron, setCron] = useState(trigger?.cron ?? "0 0 * * *");
  const [timezone, setTimezone] = useState(trigger?.timezone || DEFAULT_TIMEZONE);
  const [description, setDescription] = useState(trigger?.description ?? "");
  const [inputText, setInputText] = useState(() =>
    trigger?.input && Object.keys(trigger.input).length ? JSON.stringify(trigger.input, null, 2) : selected.inputSample || "{}"
  );
  const [enabled, setEnabled] = useState(trigger?.enabled ?? true);
  const save = useSaveTriggerMutation(appKey);

  const spec = events.find((item) => item.key === event);
  const cronResult = parseCron(cron);
  const cronText = cronResult.ok ? describeCron(cron) : null;
  const upcoming = useMemo(() => (kind === "schedule" ? nextCronRuns(cron, timezone, 3) : []), [kind, cron, timezone]);
  const zoneOptions = TIMEZONES.some((item) => item.value === timezone)
    ? TIMEZONES
    : [...TIMEZONES, { value: timezone, label: timezone }];

  async function submit() {
    const input = parseObject(inputText);
    if (!input) {
      toast.error("固定入参必须是 JSON 对象");
      return;
    }
    if (kind === "event" && !event) {
      toast.error("请选择事件");
      return;
    }
    if (kind === "schedule" && !cronResult.ok) {
      toast.error(cronResult.error);
      return;
    }
    try {
      await save.mutateAsync({
        name: selected.name,
        triggerId: trigger?.id,
        payload: {
          kind,
          event: kind === "event" ? event : undefined,
          cron: kind === "schedule" ? cron.trim() : undefined,
          timezone: kind === "schedule" ? timezone : undefined,
          description: description.trim(),
          input,
          enabled
        }
      });
      toast.success(editing ? "触发器已更新" : "触发器已创建");
      onClose();
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editing ? "编辑触发器" : "新建触发器"}</DialogTitle>
          <DialogDescription className="font-mono text-xs">{selected.name}</DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[68vh] pr-3">
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-2">
              {(
                [
                  { key: "event", icon: Zap, title: "事件触发" },
                  { key: "schedule", icon: CalendarClock, title: "定时触发" }
                ] as const
              ).map((item) => (
                <button
                  key={item.key}
                  type="button"
                  disabled={editing && kind !== item.key}
                  onClick={() => setKind(item.key)}
                  className={cn(
                    "flex items-center gap-2.5 rounded-lg border p-3 text-left text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-40",
                    kind === item.key ? "border-primary/60 bg-primary/5" : "hover:bg-muted/50"
                  )}
                >
                  <span
                    className={cn(
                      "grid size-8 place-items-center rounded-lg",
                      kind === item.key ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground"
                    )}
                  >
                    <item.icon className="size-4" />
                  </span>
                  <span className="font-medium">{item.title}</span>
                  {kind === item.key ? <Check className="ml-auto size-4 text-primary" /> : null}
                </button>
              ))}
            </div>

            {kind === "event" ? (
              <div className="grid gap-3 md:grid-cols-[1fr_15rem]">
                <div className="space-y-3">
                  {!events.length ? <p className="text-xs text-muted-foreground">事件目录加载中…</p> : null}
                  {grouped.map(([group, items]) => (
                    <div key={group} className="space-y-1.5">
                      <p className="text-xs font-medium text-muted-foreground">{group}</p>
                      <div className="grid gap-1.5 sm:grid-cols-2">
                        {items.map((item) => (
                          <button
                            key={item.key}
                            type="button"
                            onClick={() => setEvent(item.key)}
                            className={cn(
                              "space-y-0.5 rounded-lg border p-2.5 text-left transition-colors",
                              event === item.key ? "border-primary/60 bg-primary/5" : "hover:bg-muted/50"
                            )}
                          >
                            <span className="flex items-center gap-1.5 text-xs font-medium">
                              {item.label}
                              <code className="ml-auto font-mono text-[10px] font-normal text-muted-foreground">{item.key}</code>
                            </span>
                            <span className="line-clamp-2 block text-[11px] text-muted-foreground">{item.hint}</span>
                          </button>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
                {spec ? (
                  <div className="space-y-1.5">
                    <p className="text-xs font-medium text-muted-foreground">事件数据</p>
                    <EventFieldList spec={spec} />
                  </div>
                ) : null}
              </div>
            ) : (
              <div className="space-y-3">
                <div className="flex flex-wrap gap-1.5">
                  {CRON_PRESETS.map((preset) => (
                    <button
                      key={preset.cron}
                      type="button"
                      onClick={() => setCron(preset.cron)}
                      className={cn(
                        "rounded-full border px-2.5 py-1 text-xs transition-colors",
                        cron.trim() === preset.cron ? "border-primary/60 bg-primary/10 text-primary" : "hover:bg-muted/60"
                      )}
                    >
                      {preset.label}
                    </button>
                  ))}
                </div>
                <div className="grid gap-3 sm:grid-cols-[1fr_16rem]">
                  <div className="space-y-1.5">
                    <Label className="text-xs">cron 表达式</Label>
                    <Input
                      className="font-mono"
                      value={cron}
                      onChange={(e) => setCron(e.target.value)}
                      placeholder="分 时 日 月 周"
                      aria-invalid={!cronResult.ok}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">时区</Label>
                    <Select value={timezone} onValueChange={setTimezone}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {zoneOptions.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <div className="rounded-lg border bg-muted/30 p-3 text-xs">
                  {cronResult.ok ? (
                    <div className="space-y-2">
                      <p className="font-medium">{cronText ?? "按 cron 计划执行"}</p>
                      {upcoming.length ? (
                        <div className="flex flex-wrap gap-1.5">
                          {upcoming.map((at, index) => (
                            <span key={at.getTime()} className="rounded-md border bg-background px-2 py-0.5 font-data text-[11px] tabular-nums">
                              {index === 0 ? "下次 " : ""}
                              {formatInZone(at, timezone)}
                            </span>
                          ))}
                        </div>
                      ) : cronResult.every ? null : (
                        <p className="text-destructive">近期不会触发</p>
                      )}
                    </div>
                  ) : (
                    <p className="text-destructive">{cronResult.error}</p>
                  )}
                </div>
              </div>
            )}

            <div className="space-y-1.5">
              <Label className="text-xs">说明</Label>
              <Input
                value={description}
                maxLength={200}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="选填"
              />
            </div>

            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label className="text-xs">固定入参</Label>
                <code className="font-mono text-[10px] text-muted-foreground">ctx.input</code>
                <Badge variant="outline" size="sm" className="ml-auto font-normal">
                  {hasSchema(selected.inputSchema) ? "按契约校验" : "未配置契约"}
                </Badge>
              </div>
              <JsonEditor
                height={150}
                value={inputText}
                onChange={setInputText}
                schema={hasSchema(selected.inputSchema) ? selected.inputSchema : undefined}
              />
            </div>

            <label className="flex items-center justify-between rounded-lg border p-3 text-sm">
              <span>启用</span>
              <Switch checked={enabled} onCheckedChange={setEnabled} />
            </label>
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={save.isPending} onClick={submit}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            {editing ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 立即触发：**真实执行**，副作用真实发生、写调用审计、计入触发器统计。
 * 所以这里不叫「试跑」，执行前有一句后果说明，执行后把结果留在弹窗里。
 * 事件触发器必须指定一个用户来模拟这次事件（后端同样要求）。
 */
export function FireTriggerDialog({
  appKey,
  functionName,
  trigger,
  spec,
  title,
  onClose
}: {
  appKey: string;
  functionName: string;
  trigger: AppFunctionTrigger;
  spec?: FunctionTriggerEvent;
  title: string;
  onClose: () => void;
}) {
  const isEvent = trigger.kind === "event";
  const [user, setUser] = useState<PickedUser | null>(null);
  const [dataText, setDataText] = useState(() => sampleEventData(spec));
  const [result, setResult] = useState<AppFunctionTriggerFireResult | null>(null);
  const fire = useFireTriggerMutation(appKey);

  async function run() {
    const data = parseObject(dataText);
    if (isEvent && !data) {
      toast.error("事件数据必须是 JSON 对象");
      return;
    }
    if (isEvent && !user) {
      toast.error("请选择一个用户");
      return;
    }
    try {
      const response = await fire.mutateAsync({
        name: functionName,
        triggerId: trigger.id,
        payload: isEvent ? { userId: user?.id, data: data ?? {} } : {}
      });
      setResult(response);
      if (response.ok) toast.success("已执行");
      else toast.warning("执行失败");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>立即触发</DialogTitle>
          <DialogDescription>{title}</DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[68vh] pr-3">
          <div className="space-y-4">
            <Alert className="border-amber-500/40 bg-amber-500/5">
              <AlertTriangle className="text-amber-600 dark:text-amber-400" />
              <AlertDescription className="text-xs">将在激活版本上真实执行一次，写操作会真实生效。</AlertDescription>
            </Alert>

            {isEvent ? (
              <>
                <div className="space-y-1.5">
                  <Label className="text-xs">事件用户</Label>
                  <UserPicker appKey={appKey} value={user} onChange={setUser} />
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center gap-2">
                    <Label className="text-xs">事件数据</Label>
                    <code className="font-mono text-[10px] text-muted-foreground">ctx.trigger.data</code>
                  </div>
                  <JsonEditor height={130} value={dataText} onChange={setDataText} />
                </div>
              </>
            ) : null}

            {result ? (
              <div className="space-y-3 rounded-lg border p-3">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <Badge variant={result.ok ? "success" : "danger"} size="sm">
                    {result.ok ? "执行成功" : "执行失败"}
                  </Badge>
                  <code className="font-mono text-[11px] text-muted-foreground">{result.eventId}</code>
                  <CopyButton value={result.eventId} label="事件 ID" />
                </div>
                {result.error ? (
                  <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
                    {result.errorCode ? (
                      <span className="mr-1.5 font-mono text-[11px] text-muted-foreground">{result.errorCode}</span>
                    ) : null}
                    {result.error}
                  </div>
                ) : null}
                {result.output !== undefined && result.output !== null ? (
                  <DetailSection title="返回值">
                    <JsonBlock value={result.output} />
                  </DetailSection>
                ) : null}
                <DetailSection title={`副作用（${result.effects.length}）`}>
                  <EffectList effects={result.effects} timeline />
                </DetailSection>
              </div>
            ) : null}
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
          <Button disabled={fire.isPending || (isEvent && !user)} onClick={run}>
            {fire.isPending ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
            {result ? "再执行一次" : "执行"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
