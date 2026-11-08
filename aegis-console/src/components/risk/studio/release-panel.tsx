"use client";

import { useState } from "react";
import { History, RotateCcw, Rocket, ShieldCheck } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { Field, Figure, Segmented } from "@/components/traffic/traffic-kit";
import type { Release, ReleaseState, StreamSyncResult, ValidateResult } from "@/lib/api/risk";
import { formatDateTime, formatShortDateTime } from "@/lib/datetime";
import {
  usePromoteScene, usePublishScene, useRiskDraftBundle, useRiskRelease, useRiskReleases, useRollbackScene, useSceneTraffic, useWithdrawScene,
} from "@/lib/risk-hooks";

import { CodeDiff } from "./code-editor";
import { errorText, IssueList, stableJson, StudioSection } from "./kit";

type PublishMode = "full" | "canary" | "shadow";

function streamSummary(s: StreamSyncResult | undefined) {
  if (!s) return "";
  const parts: string[] = [];
  if (s.created?.length) parts.push(`新建流式特征 ${s.created.length}`);
  if (s.dropped?.length) parts.push(`清理 ${s.dropped.length}`);
  return parts.join("，");
}

export function PublishDialog({ sceneKey, state, open, onOpenChange, disabledReason }: {
  sceneKey: string;
  state?: ReleaseState;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 非空时不能发布（有未保存修改 / 校验不通过） */
  disabledReason?: string | null;
}) {
  const publish = usePublishScene();
  const [note, setNote] = useState("");
  const [mode, setMode] = useState<PublishMode>("full");
  const [percent, setPercent] = useState(10);
  const hasPrimary = !!state?.primaryId;

  const submit = () => publish.mutate(
    { key: sceneKey, note: note.trim(), mode, canaryPercent: mode === "canary" ? percent : undefined },
    {
      onSuccess: (r) => {
        const extra = streamSummary(r.streams);
        toast.success(`已发布 ${r.release.version}${extra ? `，${extra}` : ""}`);
        onOpenChange(false);
        setNote("");
      },
      onError: (e) => toast.error(errorText(e, "发布失败")),
    },
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>发布场景</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="发布方式">
            <Segmented
              value={mode}
              onChange={setMode}
              options={[
                { value: "full" as const, label: "直接生效" },
                { value: "canary" as const, label: hasPrimary ? "灰度" : "灰度（需先有正式版本）" },
                { value: "shadow" as const, label: hasPrimary ? "影子" : "影子（需先有正式版本）" },
              ].filter((o) => hasPrimary || o.value === "full")}
            />
          </Field>
          {!hasPrimary ? <p className="text-xs text-muted-foreground">首次发布只能直接生效。</p> : null}
          {mode === "canary" ? (
            <Field label={`灰度比例 ${percent}%`}>
              <Slider min={1} max={99} step={1} value={[percent]} onValueChange={(v) => setPercent(v[0] ?? 10)} />
            </Field>
          ) : null}
          {mode === "shadow" ? <p className="text-xs text-muted-foreground">候选版本只评估、不生效，结论写入流水用于对比。</p> : null}
          <Field label="发布说明">
            <Textarea rows={3} value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
          {disabledReason ? <p className="text-xs text-red-600 dark:text-red-400">{disabledReason}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={submit} disabled={publish.isPending || !!disabledReason}>
            <Rocket />
            发布
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type Confirm = { kind: "withdraw" } | { kind: "drop" } | { kind: "promote" } | { kind: "rollback"; release: Release };

function stripVolatile(bundle: Record<string, unknown> | undefined) {
  if (!bundle) return {};
  const rest = { ...bundle };
  for (const k of ["version", "checksum", "publishedAt", "publishedBy", "note"]) delete rest[k];
  return rest;
}

export function ReleasePanel({ sceneKey, validation, validating, onValidate, dirty, onPublish }: {
  sceneKey: string;
  validation?: ValidateResult;
  validating: boolean;
  onValidate: () => void;
  dirty: boolean;
  onPublish: () => void;
}) {
  const releases = useRiskReleases(sceneKey);
  const draft = useRiskDraftBundle(sceneKey);
  const promote = usePromoteScene();
  const rollback = useRollbackScene();
  const withdraw = useWithdrawScene();
  const traffic = useSceneTraffic();
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [diffId, setDiffId] = useState<number | null>(null);
  const [pendingPercent, setPendingPercent] = useState<number | null>(null);
  const list = releases.data?.releases ?? [];
  const state = releases.data?.state;
  const byId = new Map(list.map((r) => [r.id, r]));
  const primary = state?.primaryId ? byId.get(state.primaryId) : undefined;
  const candidate = state?.candidateId ? byId.get(state.candidateId) : undefined;
  const chosenDiff = diffId ?? state?.primaryId ?? list[0]?.id ?? null;
  const diffRelease = useRiskRelease(chosenDiff);
  const percent = pendingPercent ?? state?.canaryPercent ?? 10;

  const done = (msg: string) => ({
    onSuccess: () => {
      toast.success(msg);
      setConfirm(null);
      setPendingPercent(null);
    },
    onError: (e: unknown) => toast.error(errorText(e)),
  });

  const runConfirm = () => {
    if (!confirm) return;
    if (confirm.kind === "withdraw") withdraw.mutate({ key: sceneKey }, done("场景已下线"));
    if (confirm.kind === "drop") traffic.mutate({ key: sceneKey, drop: true }, done("已放弃候选版本"));
    if (confirm.kind === "promote") promote.mutate({ key: sceneKey }, done("候选版本已转正"));
    if (confirm.kind === "rollback") rollback.mutate({ key: sceneKey, releaseId: confirm.release.id }, done(`已回滚到 ${confirm.release.version}`));
  };

  const confirmText: Record<Confirm["kind"], { title: string; body: string; action: string; danger?: boolean }> = {
    withdraw: { title: "下线场景", body: "场景不再评估，接入点按未发布放行。", action: "下线", danger: true },
    drop: { title: "放弃候选版本", body: "候选版本停止接收流量，正式版本不受影响。", action: "放弃", danger: true },
    promote: { title: "候选版本转正", body: "候选版本接管全部流量，原正式版本保留在历史里。", action: "转正" },
    rollback: { title: "回滚", body: "所选版本立即成为正式版本，候选版本一并撤下。", action: "回滚", danger: true },
  };
  const c = confirm ? confirmText[confirm.kind] : null;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="grid min-w-0 gap-4 lg:grid-cols-2">
        <StudioSection
          title="校验"
          aside={
            <Button size="sm" variant="outline" onClick={onValidate} disabled={validating}>
              <ShieldCheck />
              校验
            </Button>
          }
        >
          {validating && !validation ? (
            <Skeleton className="h-16 rounded-lg" />
          ) : validation ? (
            <>
              <div className="grid grid-cols-3 gap-3">
                <Figure label="特征" value={validation.features} />
                <Figure label="规则" value={validation.rules} />
                <Figure label="模型" value={validation.models} />
              </div>
              <IssueList issues={validation.issues ?? []} />
            </>
          ) : (
            <p className="text-sm text-muted-foreground">尚未校验</p>
          )}
          {dirty ? <p className="text-xs text-amber-600 dark:text-amber-400">有未保存的修改，校验与发布只针对已保存的草稿</p> : null}
        </StudioSection>

        <StudioSection
          title="当前状态"
          aside={
            <Button size="sm" onClick={onPublish}>
              <Rocket />
              发布
            </Button>
          }
        >
          {releases.isLoading ? (
            <Skeleton className="h-24 rounded-lg" />
          ) : (
            <div className="flex flex-col gap-3">
              <div className="grid grid-cols-2 gap-3">
                <Figure label="正式版本" value={primary ? primary.version : state?.primaryId ? `#${state.primaryId}` : "未发布"} />
                <Figure
                  label="候选版本"
                  value={candidate ? `${candidate.version} ${state?.shadow ? "影子" : `灰度 ${state?.canaryPercent}%`}` : "无"}
                />
              </div>
              {state?.updatedAt ? <div className="text-xs text-muted-foreground">{formatDateTime(state.updatedAt)} {state.updatedBy}</div> : null}
              {state?.candidateId ? (
                <div className="flex flex-col gap-3 rounded-lg border p-3">
                  {!state.shadow ? (
                    <Field label={`灰度比例 ${percent}%`}>
                      <Slider min={1} max={99} step={1} value={[percent]} onValueChange={(v) => setPendingPercent(v[0] ?? percent)} />
                    </Field>
                  ) : null}
                  <div className="flex flex-wrap gap-2">
                    {!state.shadow && pendingPercent !== null && pendingPercent !== state.canaryPercent ? (
                      <Button size="sm" variant="outline" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, canaryPercent: percent }, done(`灰度比例已调为 ${percent}%`))}>
                        应用比例
                      </Button>
                    ) : null}
                    {state.shadow ? (
                      <Button size="sm" variant="outline" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, canaryPercent: 10 }, done("已转为灰度 10%"))}>
                        转为灰度
                      </Button>
                    ) : (
                      <Button size="sm" variant="outline" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, shadow: true }, done("已转为影子"))}>
                        转为影子
                      </Button>
                    )}
                    <Button size="sm" onClick={() => setConfirm({ kind: "promote" })}>转正</Button>
                    <Button size="sm" variant="ghost" onClick={() => setConfirm({ kind: "drop" })}>放弃候选</Button>
                  </div>
                </div>
              ) : null}
              {state?.primaryId ? (
                <Button size="sm" variant="outline" className="w-fit text-destructive" onClick={() => setConfirm({ kind: "withdraw" })}>下线场景</Button>
              ) : null}
            </div>
          )}
        </StudioSection>
      </div>

      <StudioSection title="发布记录" aside={<History className="size-4 text-muted-foreground" />}>
        {releases.isLoading ? (
          <div className="flex flex-col gap-2">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-10 rounded-md" />)}</div>
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无发布记录</p>
        ) : (
          <div className="min-w-0 overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>版本</TableHead>
                  <TableHead>说明</TableHead>
                  <TableHead>发布人</TableHead>
                  <TableHead>时间</TableHead>
                  <TableHead>摘要</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <span className="font-mono text-sm">{r.version}</span>
                        {r.id === state?.primaryId ? <Badge variant="success" size="sm">正式</Badge> : null}
                        {r.id === state?.candidateId ? <Badge variant="warning" size="sm">候选</Badge> : null}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-64 truncate text-sm">{r.note || "无"}</TableCell>
                    <TableCell className="text-sm">{r.publishedBy}</TableCell>
                    <TableCell className="text-sm">{formatShortDateTime(r.publishedAt)}</TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">{r.checksum.slice(0, 10)}</TableCell>
                    <TableCell>
                      {r.id !== state?.primaryId ? (
                        <Button size="xs" variant="outline" onClick={() => setConfirm({ kind: "rollback", release: r })}>
                          <RotateCcw />
                          回滚
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </StudioSection>

      <StudioSection
        title="与草稿对比"
        aside={
          list.length ? (
            <Select value={chosenDiff ? String(chosenDiff) : undefined} onValueChange={(v) => setDiffId(Number(v))}>
              <SelectTrigger size="sm" className="h-8 w-44"><SelectValue placeholder="选择版本" /></SelectTrigger>
              <SelectContent>
                {list.map((r) => <SelectItem key={r.id} value={String(r.id)}>{r.version} {formatShortDateTime(r.publishedAt)}</SelectItem>)}
              </SelectContent>
            </Select>
          ) : null
        }
      >
        {!list.length ? (
          <p className="text-sm text-muted-foreground">暂无可对比的版本</p>
        ) : diffRelease.isLoading || draft.isLoading ? (
          <Skeleton className="h-[420px] rounded-lg" />
        ) : (
          <>
            <div className="grid grid-cols-2 text-xs text-muted-foreground">
              <span>{diffRelease.data?.version ?? ""}</span>
              <span>草稿{dirty ? "（已保存部分）" : ""}</span>
            </div>
            <CodeDiff original={stableJson(stripVolatile(diffRelease.data?.bundle))} modified={stableJson(stripVolatile(draft.data))} />
          </>
        )}
      </StudioSection>

      <AlertDialog open={!!confirm} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{c?.title}{confirm?.kind === "rollback" ? ` ${confirm.release.version}` : ""}</AlertDialogTitle>
            <AlertDialogDescription>{c?.body}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction className={c?.danger ? "bg-destructive text-white hover:bg-destructive/90" : undefined} onClick={runConfirm}>
              {c?.action}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
