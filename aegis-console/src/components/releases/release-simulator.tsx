"use client";

import { useState } from "react";
import { toast } from "sonner";
import { CheckCircle2, FlaskConical, XCircle } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ReleaseSimulation } from "@/lib/api/types";
import { useSimulateReleaseMutation } from "@/lib/release-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichContent } from "@/components/ui/rich-editor";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { COMMON_ABIS, DATA_FONT, RELEASE_PLATFORMS, UpdateTypeBadge, formatBytes, parseList } from "./release-shared";

const FORCE_REASONS: Record<string, string> = {
  release: "目标版本为强制更新",
  skipped: "跨越的版本中有强制更新",
  unsupported: "当前版本低于最低支持版本"
};

/**
 * 模拟检测：以指定的客户端身份跑一遍真实的检测引擎，并列出每个候选版本的判定理由。
 * 「为什么这台手机收不到」是灰度期间最常被问的问题，答案应当一眼可见。
 */
export function ReleaseSimulator({ appKey }: { appKey: string }) {
  const simulate = useSimulateReleaseMutation(appKey);
  const [form, setForm] = useState({
    versionCode: "",
    platform: "android",
    userId: "",
    deviceId: "",
    abis: "arm64-v8a, armeabi-v7a",
    osVersion: "",
    deviceModel: "",
    locale: "zh-CN",
    region: "CN"
  });
  const [result, setResult] = useState<ReleaseSimulation | null>(null);

  const patch = (key: keyof typeof form, value: string) => setForm((state) => ({ ...state, [key]: value }));

  async function run() {
    try {
      const data = await simulate.mutateAsync({
        versionCode: Number(form.versionCode) || 0,
        platform: form.platform,
        userId: Number(form.userId) || 0,
        deviceId: form.deviceId.trim(),
        abis: parseList(form.abis),
        osVersion: Number(form.osVersion) || 0,
        deviceModel: form.deviceModel.trim(),
        locale: form.locale.trim(),
        region: form.region.trim().toUpperCase()
      });
      setResult(data);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  }

  const check = result?.result;

  return (
    <div className="grid gap-4 lg:grid-cols-[22rem_1fr]">
      <div className="space-y-3 rounded-xl border bg-card p-4">
        <div className="grid grid-cols-2 gap-3">
          <SimField label="当前版本码">
            <Input className={DATA_FONT} inputMode="numeric" value={form.versionCode} placeholder="0" onChange={(e) => patch("versionCode", e.target.value.replace(/\D/g, ""))} />
          </SimField>
          <SimField label="平台">
            <Select value={form.platform} onValueChange={(value) => patch("platform", value)}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RELEASE_PLATFORMS.filter((item) => item.value !== "all").map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SimField>
          <SimField label="用户 ID">
            <Input className={DATA_FONT} inputMode="numeric" value={form.userId} placeholder="未登录" onChange={(e) => patch("userId", e.target.value.replace(/\D/g, ""))} />
          </SimField>
          <SimField label="系统版本（API）">
            <Input className={DATA_FONT} inputMode="numeric" value={form.osVersion} placeholder="不限" onChange={(e) => patch("osVersion", e.target.value.replace(/\D/g, ""))} />
          </SimField>
        </div>
        <SimField label="设备 ID">
          <Input className={DATA_FONT} value={form.deviceId} placeholder="可留空" onChange={(e) => patch("deviceId", e.target.value)} />
        </SimField>
        <SimField label="ABI（按偏好排序）">
          <Input className={DATA_FONT} value={form.abis} placeholder={COMMON_ABIS.slice(0, 2).join(", ")} onChange={(e) => patch("abis", e.target.value)} />
        </SimField>
        <SimField label="机型">
          <Input value={form.deviceModel} placeholder="Pixel 9" onChange={(e) => patch("deviceModel", e.target.value)} />
        </SimField>
        <div className="grid grid-cols-2 gap-3">
          <SimField label="语言">
            <Input value={form.locale} onChange={(e) => patch("locale", e.target.value)} />
          </SimField>
          <SimField label="地区码">
            <Input value={form.region} onChange={(e) => patch("region", e.target.value)} />
          </SimField>
        </div>
        <Button className="w-full" size="sm" disabled={simulate.isPending} onClick={() => void run()}>
          <FlaskConical className="size-3.5" />
          {simulate.isPending ? "检测中" : "模拟检测"}
        </Button>
      </div>

      <div className="space-y-4">
        {simulate.isPending ? (
          <div className="space-y-3">
            <Skeleton className="h-24 w-full rounded-xl" />
            <Skeleton className="h-48 w-full rounded-xl" />
          </div>
        ) : !check ? (
          <div className="flex h-full min-h-48 items-center justify-center rounded-xl border border-dashed text-sm text-muted-foreground">
            填写客户端信息后开始模拟
          </div>
        ) : (
          <>
            <div className="rounded-xl border bg-card p-4">
              {check.hasUpdate && check.release ? (
                <div className="space-y-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" />
                    <span className="text-sm font-semibold">
                      收到 <span className={DATA_FONT}>{check.release.version}</span>
                    </span>
                    <UpdateTypeBadge value={check.updateType} />
                    {check.release.channel ? <Badge variant="outline">{check.release.channel.name}</Badge> : null}
                  </div>
                  {check.forceReason ? <p className="text-xs text-muted-foreground">{FORCE_REASONS[check.forceReason]}</p> : null}
                  {check.asset ? (
                    <p className="text-xs text-muted-foreground">
                      安装包 <span className={DATA_FONT}>{check.asset.abi}</span>，{formatBytes(check.asset.fileSize)}
                    </p>
                  ) : (
                    <p className="text-xs text-destructive">该版本没有可用的安装包</p>
                  )}
                  {check.changelog && check.changelog.length > 1 ? (
                    <div className="space-y-2 border-t pt-3">
                      <p className="text-xs text-muted-foreground">将跨越 {check.changelog.length} 个版本</p>
                      {check.changelog.map((entry) => (
                        <div key={entry.versionCode} className="space-y-1">
                          <div className="flex items-center gap-2 text-xs">
                            <span className={cn("font-medium", DATA_FONT)}>{entry.version}</span>
                            {entry.updateType !== "optional" ? <UpdateTypeBadge value={entry.updateType} /> : null}
                          </div>
                          {entry.notes ? <RichContent html={entry.notes} className="text-xs" /> : null}
                        </div>
                      ))}
                    </div>
                  ) : null}
                </div>
              ) : (
                <div className="flex items-center gap-2 text-sm">
                  <XCircle className="size-4 text-muted-foreground" />
                  没有可下发的新版本
                </div>
              )}
            </div>

            <div className="overflow-hidden rounded-xl border bg-card">
              <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">候选版本判定</div>
              {result.decisions.length === 0 ? (
                <p className="px-4 py-6 text-center text-sm text-muted-foreground">当前没有下发中或待发布的版本</p>
              ) : (
                <div className="divide-y">
                  {result.decisions.map((decision) => (
                    <div key={decision.releaseId} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                      {decision.eligible ? (
                        <CheckCircle2 className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
                      ) : (
                        <XCircle className="size-4 shrink-0 text-muted-foreground" />
                      )}
                      <span className={cn("w-24 shrink-0 font-medium", DATA_FONT)}>{decision.version}</span>
                      <span className={cn("w-24 shrink-0 text-xs text-muted-foreground", DATA_FONT)}>{decision.versionCode}</span>
                      <span className="min-w-0 flex-1 text-muted-foreground">{decision.reason}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function SimField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}
