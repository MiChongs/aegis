"use client";

import { useState } from "react";
import { Clock, Pencil, Plus, Save, Timer } from "lucide-react";
import { notify } from "@/lib/notify";
import { browserTimeZone } from "@/lib/datetime";
import {
  useDeleteTicketSLAPolicyMutation,
  useSaveTicketSLAPolicyMutation,
  useTicketSLAPoliciesQuery
} from "@/lib/ticket-hooks";
import type { TicketSLAPolicy } from "@/lib/api/tickets";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { PRIORITY_LABEL } from "../ticket-shared";
import {
  DeleteButton,
  Field,
  ListSkeleton,
  PRIORITIES,
  PRIORITY_TONE,
  ScopeBadges,
  SectionEmpty,
  SectionError,
  SectionHeader,
  SwitchRow,
  WEEKDAYS,
  WeekStrip,
  formatMinutes,
  reportError
} from "./settings-shared";
import { cn } from "@/lib/utils";

// SLA 策略：每条策略一张时限表（四个优先级 × 首响 / 解决），旁边是计时日历。
// 编辑时分钟数旁边直接换算成小时或天，避免在「1440」和「2880」之间心算。

type SLAForm = {
  name: string;
  description: string;
  first: Record<string, number>;
  resolve: Record<string, number>;
  warnRatio: number;
  businessHoursEnabled: boolean;
  timezone: string;
  start: string;
  end: string;
  days: number[];
  enabled: boolean;
};

function defaultForm(): SLAForm {
  return {
    name: "",
    description: "",
    first: { urgent: 30, high: 120, normal: 480, low: 1440 },
    resolve: { urgent: 240, high: 720, normal: 2880, low: 7200 },
    warnRatio: 0.8,
    businessHoursEnabled: false,
    timezone: browserTimeZone(),
    start: "09:00",
    end: "18:00",
    days: [1, 2, 3, 4, 5],
    enabled: true
  };
}

function formFromPolicy(policy: TicketSLAPolicy): SLAForm {
  return {
    name: policy.name,
    description: policy.description,
    first: { ...policy.firstResponseMinutes },
    resolve: { ...policy.resolveMinutes },
    warnRatio: policy.warnRatio,
    businessHoursEnabled: Boolean(policy.businessHours),
    timezone: policy.businessHours?.timezone || browserTimeZone(),
    start: policy.businessHours?.start || "09:00",
    end: policy.businessHours?.end || "18:00",
    days: policy.businessHours?.days || [1, 2, 3, 4, 5],
    enabled: policy.enabled
  };
}

const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;

export function SLASection({ appId }: { appId: number }) {
  const query = useTicketSLAPoliciesQuery(appId);
  const saveMut = useSaveTicketSLAPolicyMutation();
  const deleteMut = useDeleteTicketSLAPolicyMutation();
  const [editing, setEditing] = useState<{ id?: number; form: SLAForm } | null>(null);

  const policies = query.data ?? [];

  const save = async (id: number | undefined, form: SLAForm) => {
    if (!form.name.trim()) {
      notify.warning("请填写策略名称");
      return false;
    }
    if (form.businessHoursEnabled) {
      if (!TIME_PATTERN.test(form.start) || !TIME_PATTERN.test(form.end)) {
        notify.warning("工作时间请按 09:00 这样的格式填写");
        return false;
      }
      if (form.days.length === 0) {
        notify.warning("至少选择一个工作日");
        return false;
      }
    }
    try {
      await saveMut.mutateAsync({
        id,
        payload: {
          appid: appId,
          name: form.name.trim(),
          description: form.description.trim(),
          firstResponseMinutes: form.first,
          resolveMinutes: form.resolve,
          warnRatio: form.warnRatio,
          businessHours: form.businessHoursEnabled
            ? { timezone: form.timezone, days: form.days, start: form.start, end: form.end }
            : null,
          enabled: form.enabled
        }
      });
      notify.success("SLA 策略已保存");
      return true;
    } catch (error) {
      reportError(error, "保存失败");
      return false;
    }
  };

  return (
    <div className="space-y-4">
      <SectionHeader
        title="SLA 策略"
        description="按优先级约定首次响应与解决时限，到达预警比例时提醒处理人。"
        action={
          <Button size="sm" onClick={() => setEditing({ form: defaultForm() })}>
            <Plus />
            新建策略
          </Button>
        }
      />

      {query.isLoading ? (
        <ListSkeleton rows={2} />
      ) : query.isError ? (
        <SectionError error={query.error} onRetry={() => void query.refetch()} />
      ) : policies.length === 0 ? (
        <SectionEmpty
          icon={Timer}
          title="还没有 SLA 策略"
          description="没有策略时工单不计时，也不会出现超时提醒。"
          action={
            <Button size="sm" onClick={() => setEditing({ form: defaultForm() })}>
              <Plus />
              新建策略
            </Button>
          }
        />
      ) : (
        <div className="space-y-3">
          {policies.map((policy) => (
            <div key={policy.id} className={cn("overflow-hidden rounded-xl border bg-card", !policy.enabled && "opacity-70")}>
              <div className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-sm font-medium text-foreground">{policy.name}</span>
                    <ScopeBadges platform={policy.appid === 0 && appId !== 0} enabled={policy.enabled} />
                    <Badge variant="outline" size="sm">
                      用到 {Math.round(policy.warnRatio * 100)}% 时预警
                    </Badge>
                  </div>
                  {policy.description ? <p className="truncate text-xs text-muted-foreground">{policy.description}</p> : null}
                </div>
                <Button size="icon-sm" variant="ghost" aria-label="编辑" onClick={() => setEditing({ id: policy.id, form: formFromPolicy(policy) })}>
                  <Pencil />
                </Button>
                <DeleteButton
                  title={`删除策略「${policy.name}」？`}
                  description="此操作不可撤销。"
                  pending={deleteMut.isPending}
                  onConfirm={async () => {
                    try {
                      await deleteMut.mutateAsync(policy.id);
                      notify.success("策略已删除");
                    } catch (error) {
                      reportError(error, "删除失败");
                    }
                  }}
                />
              </div>
              <div className="grid gap-0 md:grid-cols-[1fr_220px]">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-[11px] text-muted-foreground">
                      <th className="px-4 py-2 font-medium">优先级</th>
                      <th className="px-4 py-2 font-medium">首次响应</th>
                      <th className="px-4 py-2 font-medium">解决</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {PRIORITIES.map((priority) => (
                      <tr key={priority}>
                        <td className="px-4 py-2">
                          <span className="flex items-center gap-2 text-foreground">
                            <span className={cn("size-1.5 rounded-full", PRIORITY_TONE[priority])} />
                            {PRIORITY_LABEL[priority]}
                          </span>
                        </td>
                        <td className="px-4 py-2 tabular-nums text-foreground">
                          {formatMinutes(policy.firstResponseMinutes[priority])}
                        </td>
                        <td className="px-4 py-2 tabular-nums text-foreground">{formatMinutes(policy.resolveMinutes[priority])}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="space-y-2 border-t bg-muted/20 px-4 py-3 md:border-l md:border-t-0">
                  <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                    <Clock className="size-3.5" />
                    计时时间
                  </p>
                  {policy.businessHours ? (
                    <>
                      <WeekStrip days={policy.businessHours.days} />
                      <p className="text-sm tabular-nums text-foreground">
                        {policy.businessHours.start} – {policy.businessHours.end}
                      </p>
                      <p className="truncate text-[11px] text-muted-foreground">{policy.businessHours.timezone}</p>
                    </>
                  ) : (
                    <>
                      <WeekStrip days={[1, 2, 3, 4, 5, 6, 7]} />
                      <p className="text-sm text-foreground">7×24 小时</p>
                      <p className="text-[11px] text-muted-foreground">不区分工作时间</p>
                    </>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      <SLASheet
        key={editing ? `${editing.id ?? "new"}` : "closed"}
        editing={editing}
        pending={saveMut.isPending}
        onClose={() => setEditing(null)}
        onSave={async (form) => {
          if (await save(editing?.id, form)) setEditing(null);
        }}
      />
    </div>
  );
}

function SLASheet({
  editing,
  pending,
  onClose,
  onSave
}: {
  editing: { id?: number; form: SLAForm } | null;
  pending: boolean;
  onClose: () => void;
  onSave: (form: SLAForm) => void;
}) {
  const [form, setForm] = useState<SLAForm>(() => editing?.form ?? defaultForm());
  const patch = (next: Partial<SLAForm>) => setForm((prev) => ({ ...prev, ...next }));

  return (
    <Sheet open={Boolean(editing)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle>{editing?.id ? "编辑 SLA 策略" : "新建 SLA 策略"}</SheetTitle>
          <SheetDescription>时限按分钟填写，0 表示不限。</SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-6 overflow-y-auto p-4">
          <section className="space-y-3">
            <Field label="名称">
              <Input value={form.name} onChange={(event) => patch({ name: event.target.value })} placeholder="标准 SLA" />
            </Field>
            <Field label="描述">
              <Textarea rows={2} value={form.description} onChange={(event) => patch({ description: event.target.value })} />
            </Field>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium text-foreground">时限</h3>
            <div className="overflow-hidden rounded-lg border">
              <div className="grid grid-cols-[80px_1fr_1fr] gap-2 bg-muted/40 px-3 py-2 text-[11px] font-medium text-muted-foreground">
                <span>优先级</span>
                <span>首次响应（分钟）</span>
                <span>解决（分钟）</span>
              </div>
              {PRIORITIES.map((priority) => (
                <div key={priority} className="grid grid-cols-[80px_1fr_1fr] items-start gap-2 border-t px-3 py-2">
                  <span className="flex h-8 items-center gap-2 text-sm text-foreground">
                    <span className={cn("size-1.5 rounded-full", PRIORITY_TONE[priority])} />
                    {PRIORITY_LABEL[priority]}
                  </span>
                  <MinutesInput
                    value={form.first[priority] ?? 0}
                    onChange={(value) => patch({ first: { ...form.first, [priority]: value } })}
                  />
                  <MinutesInput
                    value={form.resolve[priority] ?? 0}
                    onChange={(value) => patch({ resolve: { ...form.resolve, [priority]: value } })}
                  />
                </div>
              ))}
            </div>
          </section>

          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-foreground">预警阈值</h3>
              <span className="text-sm font-semibold tabular-nums text-foreground">{Math.round(form.warnRatio * 100)}%</span>
            </div>
            <Slider
              min={10}
              max={95}
              step={5}
              value={[Math.round(form.warnRatio * 100)]}
              onValueChange={([value]) => patch({ warnRatio: value / 100 })}
            />
            <p className="text-[11px] text-muted-foreground">时限用掉这个比例时把工单标为「即将超时」并提醒处理人。</p>
          </section>

          <section className="space-y-3">
            <SwitchRow
              title="仅在工作时间内计时"
              description="关闭则 7×24 小时连续计时"
              control={
                <Switch
                  checked={form.businessHoursEnabled}
                  onCheckedChange={(value) => patch({ businessHoursEnabled: value })}
                />
              }
            />
            {form.businessHoursEnabled ? (
              <div className="space-y-3 rounded-lg border p-3">
                <Field label="工作日">
                  <div className="flex gap-1">
                    {WEEKDAYS.map((label, index) => {
                      const day = index + 1;
                      const on = form.days.includes(day);
                      return (
                        <button
                          key={label}
                          type="button"
                          aria-pressed={on}
                          onClick={() =>
                            patch({
                              days: on ? form.days.filter((item) => item !== day) : [...form.days, day].sort((a, b) => a - b)
                            })
                          }
                          className={cn(
                            "flex size-9 items-center justify-center rounded-md border text-sm transition-colors",
                            on ? "border-primary bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"
                          )}
                        >
                          {label}
                        </button>
                      );
                    })}
                  </div>
                </Field>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label="开始">
                    <Input type="time" value={form.start} onChange={(event) => patch({ start: event.target.value })} />
                  </Field>
                  <Field label="结束">
                    <Input type="time" value={form.end} onChange={(event) => patch({ end: event.target.value })} />
                  </Field>
                  <Field label="时区">
                    <Input value={form.timezone} onChange={(event) => patch({ timezone: event.target.value })} />
                  </Field>
                </div>
              </div>
            ) : null}
            <SwitchRow
              title="启用"
              description="停用的策略不再生效"
              control={<Switch checked={form.enabled} onCheckedChange={(value) => patch({ enabled: value })} />}
            />
          </section>
        </div>
        <SheetFooter className="flex-row justify-end gap-2 border-t">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending} onClick={() => onSave(form)}>
            <Save />
            保存
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function MinutesInput({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  return (
    <div className="space-y-0.5">
      <Input
        type="number"
        min={0}
        className="h-8 tabular-nums"
        value={value}
        onChange={(event) => onChange(Math.max(0, Number(event.target.value) || 0))}
      />
      <p className="text-[10px] text-muted-foreground">{formatMinutes(value)}</p>
    </div>
  );
}
