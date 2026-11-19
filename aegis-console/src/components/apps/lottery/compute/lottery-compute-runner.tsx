"use client";

import { useState } from "react";
import { Loader2, Play, Square } from "lucide-react";
import { toast } from "sonner";
import { NumberField } from "@/components/apps/app-config-primitives";
import { ComputeResultView } from "@/components/apps/lottery/compute/lottery-compute-result";
import {
  ComputeStatusBadge,
  PERSONA_PRESETS,
  Pills,
  formatDraws,
  formatDuration,
  personaPayload,
  type PersonaKey
} from "@/components/apps/lottery/compute/lottery-compute-shared";
import { SOURCE_LABEL } from "@/components/apps/lottery/lottery-shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { LotteryActivityPayload, LotterySource } from "@/lib/api/lottery";
import type { LotteryComputeJob } from "@/lib/api/lottery-compute";
import {
  useCancelLotteryComputeJobMutation,
  useCreateLotteryComputeJobMutation,
  useLotteryComputeJobQuery
} from "@/lib/lottery-compute-hooks";
import { useLotterySettingsQuery } from "@/lib/lottery-hooks";

/** 与后端 `lottery.MaxCompute*` 同值：超了后端也会拒，这里先拦住 */
const LIMITS = { users: 100_000, days: 60, drawsPerDay: 200, replications: 256, draws: 30_000_000 };

type MixRow = { on: boolean; share: string; draws: string };

const initialMix = (): Record<PersonaKey, MixRow> => ({
  user: { on: true, share: "80", draws: "" },
  member: { on: false, share: "15", draws: "" },
  lifetime: { on: false, share: "5", draws: "" },
  adFree: { on: false, share: "5", draws: "" }
});

/**
 * 发起一次推演并看着它跑完。
 *
 * 推演是落库的异步任务：发起后立刻返回，任意一个实例认领执行，进度每两秒写一次库，这里每秒问一次。
 * `payload` 给了就推演编辑器里还没保存的内容，否则推演已保存的活动。
 */
export function LotteryComputeRunner({
  appKey,
  activityId,
  payload,
  jobId,
  onJobChange
}: {
  appKey: string;
  activityId?: number;
  payload?: () => LotteryActivityPayload;
  /** 当前看的任务（面板里也可以从任务列表点选） */
  jobId: number | null;
  onJobChange: (jobId: number | null) => void;
}) {
  const create = useCreateLotteryComputeJobMutation(appKey);
  const tiers = useLotterySettingsQuery(appKey).data?.tiers ?? [];
  const [users, setUsers] = useState("2000");
  const [days, setDays] = useState("7");
  const [drawsPerDay, setDrawsPerDay] = useState("5");
  const [replications, setReplications] = useState("24");
  const [source, setSource] = useState<LotterySource>("free");
  const [seed, setSeed] = useState("");
  const [name, setName] = useState("");
  const [mix, setMix] = useState(initialMix);
  const [tierIds, setTierIds] = useState<number[]>([]);

  const patchMix = (key: PersonaKey, next: Partial<MixRow>) => setMix({ ...mix, [key]: { ...mix[key], ...next } });
  const enabled = PERSONA_PRESETS.filter((preset) => mix[preset.key].on);
  const shareTotal = enabled.reduce((sum, preset) => sum + Math.max(0, Number(mix[preset.key].share) || 0), 0);
  const perWorld = enabled.reduce((sum, preset) => {
    const row = mix[preset.key];
    const share = shareTotal > 0 ? Math.max(0, Number(row.share) || 0) / shareTotal : 0;
    return sum + share * (Number(users) || 0) * (Number(row.draws) || Number(drawsPerDay) || 0);
  }, 0);
  const total = Math.round(perWorld) * (Number(days) || 0) * (Number(replications) || 0);
  const tiered = tiers.length > 0 && (mix.member.on || mix.lifetime.on);

  const run = async () => {
    if (!enabled.length || shareTotal <= 0) {
      toast.error("至少选一种人群并给它占比");
      return;
    }
    if (total > LIMITS.draws) {
      toast.error(`总抽奖次数不能超过 ${formatDraws(LIMITS.draws)}`);
      return;
    }
    try {
      const job = await create.mutateAsync({
        activityId,
        activity: payload?.(),
        name: name.trim() || undefined,
        users: Number(users) || 1,
        days: Number(days) || 1,
        drawsPerDay: Number(drawsPerDay) || 1,
        replications: Number(replications) || 1,
        source,
        seed: seed.trim() || undefined,
        personas: enabled.map((preset) => {
          const row = mix[preset.key];
          const withTiers = (preset.key === "member" || preset.key === "lifetime") && tierIds.length ? tierIds : undefined;
          return personaPayload(preset.key, {
            share: Number(row.share) || 0,
            drawsPerDay: Number(row.draws) || undefined,
            tiers: withTiers
          });
        })
      });
      onJobChange(job.id);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发起推演失败");
    }
  };

  return (
    <div className="space-y-4">
      <div className="space-y-4 rounded-xl border border-border p-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <NumberField label="每个世界" value={users} min={1} max={LIMITS.users} unit="人" onChange={setUsers} />
          <NumberField label="连续" value={days} min={1} max={LIMITS.days} unit="天" onChange={setDays} />
          <NumberField label="每人每天" value={drawsPerDay} min={1} max={LIMITS.drawsPerDay} unit="次" onChange={setDrawsPerDay} />
          <NumberField label="平行世界" value={replications} min={1} max={LIMITS.replications} unit="遍" onChange={setReplications} />
          <div className="space-y-1.5">
            <span className="text-[11px] font-medium">次数来源</span>
            <Select value={source} onValueChange={(value) => setSource(value as LotterySource)}>
              <SelectTrigger className="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(["free", "bonus", "points"] as LotterySource[]).map((item) => (
                  <SelectItem key={item} value={item}>
                    {SOURCE_LABEL[item]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="space-y-1.5">
          <span className="text-[11px] font-medium">人群构成</span>
          <div className="divide-y divide-border rounded-lg border border-border">
            {PERSONA_PRESETS.map((preset) => {
              const row = mix[preset.key];
              const share = row.on && shareTotal > 0 ? (Math.max(0, Number(row.share) || 0) / shareTotal) * 100 : 0;
              return (
                <div key={preset.key} className="grid grid-cols-[auto_1fr_6.5rem_7.5rem_3.5rem] items-center gap-3 px-3 py-2">
                  <Switch checked={row.on} onCheckedChange={(on) => patchMix(preset.key, { on })} aria-label={preset.label} />
                  <span className="truncate text-xs">{preset.label}</span>
                  <Input
                    inputMode="decimal"
                    value={row.share}
                    disabled={!row.on}
                    aria-label={`${preset.label}占比`}
                    onChange={(event) => patchMix(preset.key, { share: event.target.value.replace(/[^\d.]/g, "") })}
                    className="h-8 text-right font-mono text-xs"
                  />
                  <Input
                    inputMode="numeric"
                    value={row.draws}
                    disabled={!row.on}
                    placeholder={`每天 ${drawsPerDay || 0} 次`}
                    aria-label={`${preset.label}每天次数`}
                    onChange={(event) => patchMix(preset.key, { draws: event.target.value.replace(/\D/g, "") })}
                    className="h-8 text-right font-mono text-xs"
                  />
                  <span className="text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                    {row.on ? `${share.toFixed(share < 10 ? 1 : 0)}%` : "—"}
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {tiered ? (
          <div className="space-y-1.5">
            <span className="text-[11px] font-medium">会员所在档位</span>
            <Pills
              multiple
              options={tiers.map((tier) => ({ key: String(tier.id), label: tier.name }))}
              selected={tierIds.map(String)}
              onToggle={(key) => {
                const id = Number(key);
                setTierIds(tierIds.includes(id) ? tierIds.filter((item) => item !== id) : [...tierIds, id]);
              }}
            />
          </div>
        ) : null}

        <div className="flex flex-wrap items-end gap-3">
          <div className="space-y-1.5">
            <span className="text-[11px] font-medium">备注名</span>
            <Input value={name} maxLength={32} placeholder="可选" onChange={(event) => setName(event.target.value)} className="h-9 w-44 text-xs" />
          </div>
          <div className="space-y-1.5">
            <span className="text-[11px] font-medium">随机种子</span>
            <Input
              inputMode="numeric"
              value={seed}
              placeholder="留空随机"
              onChange={(event) => setSeed(event.target.value.replace(/\D/g, "").slice(0, 20))}
              className="h-9 w-44 font-mono text-xs"
            />
          </div>
          <Button onClick={run} disabled={create.isPending} className="ml-auto shrink-0">
            {create.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
            推演 {formatDraws(total)} 次
          </Button>
        </div>
      </div>

      {jobId ? <ComputeJobView appKey={appKey} jobId={jobId} /> : null}
    </div>
  );
}

/** 一个任务：排队与执行中显示进度与取消，完成后显示结果 */
export function ComputeJobView({ appKey, jobId }: { appKey: string; jobId: number }) {
  const query = useLotteryComputeJobQuery(appKey, jobId);
  const cancel = useCancelLotteryComputeJobMutation(appKey);
  const job = query.data;

  if (query.isLoading) return <Skeleton className="h-32 w-full rounded-xl" />;
  if (!job) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">任务不存在</p>
    );
  }

  const stop = async () => {
    try {
      await cancel.mutateAsync(job.id);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "取消失败");
    }
  };
  const running = job.status === "queued" || job.status === "running";

  return (
    <div className="space-y-4">
      <div className="space-y-2 rounded-xl border border-border px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <ComputeStatusBadge status={job.status} />
          <span className="truncate text-xs font-medium">{jobTitle(job)}</span>
          <span className="text-[11px] text-muted-foreground">
            {job.status === "succeeded" ? `用时 ${formatDuration(job.durationMs)}` : null}
            {job.status === "running" ? `${job.progress} / ${job.total} 遍` : null}
            {job.status === "queued" ? "等待实例认领" : null}
          </span>
          {running ? (
            <Button size="sm" variant="outline" className="ml-auto h-7" onClick={stop} disabled={cancel.isPending}>
              <Square className="size-3" />
              取消
            </Button>
          ) : null}
        </div>
        {running ? <Progress value={job.total ? (job.progress / job.total) * 100 : 0} className="h-1.5" /> : null}
        {job.status === "failed" || (job.status === "cancelled" && job.message) ? (
          <p className="text-[11px] text-muted-foreground">{job.message}</p>
        ) : null}
      </div>
      {job.result ? <ComputeResultView result={job.result} /> : null}
    </div>
  );
}

export function jobTitle(job: LotteryComputeJob) {
  return job.name || job.activityName || `任务 ${job.id}`;
}
