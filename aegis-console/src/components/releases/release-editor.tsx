"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { FileUp, Link2, Loader2, Plus, Trash2 } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type {
  Release,
  ReleaseSavePayload,
  ReleaseTargeting,
  ReleaseUpdateType,
  ReleaseVisibility,
  VersionChannel
} from "@/lib/api/types";
import { usePublishReleaseMutation, useSaveReleaseMutation, useUploadReleaseAssetMutation } from "@/lib/release-hooks";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichEditor } from "@/components/ui/rich-editor";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import {
  COMMON_ABIS,
  ChannelDot,
  DATA_FONT,
  RELEASE_PLATFORMS,
  ROLLOUT_PRESETS,
  UPDATE_TYPES,
  VISIBILITIES,
  formatBytes,
  parseIdList,
  parseList
} from "./release-shared";

/**
 * 版本编辑抽屉。
 *
 * 新建一律落在草稿；底部「保存并发布」是最常见的一步到位动作。
 * 安装包整组提交：界面上看到的就是保存后的最终集合，带 id 的保留下载计数。
 */

type AssetRow = {
  key: string;
  id?: number;
  abi: string;
  label: string;
  url: string;
  fileSize: number;
  sha256: string;
  downloadUrl?: string;
  fileName?: string;
};

type Form = {
  version: string;
  versionCode: string;
  title: string;
  notes: string;
  platform: string;
  minOsVersion: string;
  updateType: ReleaseUpdateType;
  minSupportedCode: string;
  channelId: string;
  visibility: ReleaseVisibility;
  rolloutPct: number;
  testerUserIds: string;
  testerDeviceIds: string;
  excludeUserIds: string;
  excludeDeviceIds: string;
  regions: string;
  locales: string;
  deviceModels: string;
  abis: string[];
  minOsApi: string;
  maxOsApi: string;
  minSourceCode: string;
  maxSourceCode: string;
};

let rowSeq = 0;
const nextKey = () => `asset-${++rowSeq}`;

function toForm(item?: Release | null): Form {
  const t = item?.targeting ?? {};
  return {
    version: item?.version ?? "",
    versionCode: item?.versionCode ? String(item.versionCode) : "",
    title: item?.title ?? "",
    notes: item?.notes ?? "",
    platform: item?.platform ?? "android",
    minOsVersion: item?.minOsVersion ?? "",
    updateType: item?.updateType ?? "optional",
    minSupportedCode: item?.minSupportedCode ? String(item.minSupportedCode) : "",
    channelId: item?.channel?.id ? String(item.channel.id) : "none",
    visibility: item?.visibility ?? "public",
    rolloutPct: item?.rolloutPct ?? 100,
    testerUserIds: (t.testerUserIds ?? []).join("\n"),
    testerDeviceIds: (t.testerDeviceIds ?? []).join("\n"),
    excludeUserIds: (t.excludeUserIds ?? []).join("\n"),
    excludeDeviceIds: (t.excludeDeviceIds ?? []).join("\n"),
    regions: (t.regions ?? []).join(", "),
    locales: (t.locales ?? []).join(", "),
    deviceModels: (t.deviceModels ?? []).join(", "),
    abis: t.abis ?? [],
    minOsApi: t.minOsVersion ? String(t.minOsVersion) : "",
    maxOsApi: t.maxOsVersion ? String(t.maxOsVersion) : "",
    minSourceCode: t.minSourceVersionCode ? String(t.minSourceVersionCode) : "",
    maxSourceCode: t.maxSourceVersionCode ? String(t.maxSourceVersionCode) : ""
  };
}

function toAssetRows(item?: Release | null): AssetRow[] {
  return (item?.assets ?? []).map((asset) => ({
    key: nextKey(),
    id: asset.id || undefined,
    abi: asset.abi,
    label: asset.label ?? "",
    url: asset.url ?? asset.downloadUrl,
    fileSize: asset.fileSize,
    sha256: asset.sha256 ?? "",
    downloadUrl: asset.downloadUrl
  }));
}

const toInt = (value: string) => {
  const n = Number(value.trim());
  return Number.isFinite(n) && n > 0 ? Math.trunc(n) : 0;
};

// 与 next.config.ts 的 proxyClientMaxBodySize 一致：超出部分会被同源反代截掉，后端只会报缺少上传文件
const MAX_ASSET_BYTES = 1024 * 1024 * 1024;

export function ReleaseEditor({
  appKey,
  item,
  channels,
  open,
  onOpenChange,
  onSaved
}: {
  appKey: string;
  item?: Release | null;
  channels: VersionChannel[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved?: (release: Release) => void;
}) {
  const [form, setForm] = useState<Form>(() => toForm(item));
  const [assets, setAssets] = useState<AssetRow[]>(() => toAssetRows(item));
  const [errors, setErrors] = useState<Partial<Record<keyof Form | "assets", string>>>({});
  const [uploading, setUploading] = useState(0);
  const fileInput = useRef<HTMLInputElement>(null);

  const save = useSaveReleaseMutation(appKey);
  const publish = usePublishReleaseMutation(appKey);
  const upload = useUploadReleaseAssetMutation(appKey);
  const busy = save.isPending || publish.isPending || uploading > 0;
  const live = item?.effectiveStatus === "published";
  const canPublish = !item || item.status === "draft";

  function patch<K extends keyof Form>(key: K, value: Form[K]) {
    setForm((state) => ({ ...state, [key]: value }));
    setErrors((state) => ({ ...state, [key]: undefined }));
  }

  function patchAsset(key: string, changes: Partial<AssetRow>) {
    setAssets((rows) => rows.map((row) => (row.key === key ? { ...row, ...changes } : row)));
    setErrors((state) => ({ ...state, assets: undefined }));
  }

  async function handleFiles(files: FileList | null) {
    if (!files?.length) return;
    for (const file of Array.from(files)) {
      if (file.size > MAX_ASSET_BYTES) {
        toast.error(`${file.name} 超过 1 GB，无法上传`);
        continue;
      }
      setUploading((n) => n + 1);
      try {
        const result = await upload.mutateAsync(file);
        setAssets((rows) => {
          const existing = rows.find((row) => row.abi === result.abi);
          const row: AssetRow = {
            key: existing?.key ?? nextKey(),
            id: existing?.id,
            abi: result.abi,
            label: existing?.label ?? "",
            url: result.reference,
            fileSize: result.fileSize,
            sha256: result.sha256,
            downloadUrl: result.downloadUrl,
            fileName: result.fileName
          };
          return existing ? rows.map((r) => (r.key === existing.key ? row : r)) : [...rows, row];
        });
        toast.success(`已上传 ${result.fileName}`);
      } catch (error) {
        toast.error(error instanceof ApiError ? error.message : `${file.name} 上传失败`);
      } finally {
        setUploading((n) => n - 1);
      }
    }
    if (fileInput.current) fileInput.current.value = "";
  }

  function validate(): ReleaseSavePayload | null {
    const next: typeof errors = {};
    const versionCode = toInt(form.versionCode);
    if (!form.version.trim()) next.version = "填写版本名";
    if (!versionCode) next.versionCode = "填写正整数版本码";
    const minSupported = toInt(form.minSupportedCode);
    if (minSupported && versionCode && minSupported > versionCode) next.minSupportedCode = "不能高于本版本码";
    const abis = assets.map((row) => row.abi.trim().toLowerCase());
    if (assets.some((row) => !row.abi.trim() || !row.url.trim())) next.assets = "每个安装包都需要类型与地址";
    else if (new Set(abis).size !== abis.length) next.assets = "安装包类型不能重复";
    const minOs = toInt(form.minOsApi);
    const maxOs = toInt(form.maxOsApi);
    if (minOs && maxOs && minOs > maxOs) next.minOsApi = "下限不能高于上限";
    const minSrc = toInt(form.minSourceCode);
    const maxSrc = toInt(form.maxSourceCode);
    if (minSrc && maxSrc && minSrc > maxSrc) next.minSourceCode = "下限不能高于上限";
    setErrors(next);
    if (Object.keys(next).length) return null;

    const targeting: ReleaseTargeting = {
      testerUserIds: parseIdList(form.testerUserIds),
      testerDeviceIds: parseList(form.testerDeviceIds),
      excludeUserIds: parseIdList(form.excludeUserIds),
      excludeDeviceIds: parseList(form.excludeDeviceIds),
      regions: parseList(form.regions).map((item) => item.toUpperCase()),
      locales: parseList(form.locales),
      deviceModels: parseList(form.deviceModels),
      abis: form.abis,
      minOsVersion: minOs,
      maxOsVersion: maxOs,
      minSourceVersionCode: minSrc,
      maxSourceVersionCode: maxSrc
    };
    return {
      version: form.version.trim(),
      versionCode,
      title: form.title.trim(),
      notes: form.notes,
      platform: form.platform,
      minOsVersion: form.minOsVersion.trim(),
      updateType: form.updateType,
      minSupportedCode: minSupported,
      channelId: form.channelId === "none" ? null : Number(form.channelId),
      visibility: form.visibility,
      rolloutPct: form.rolloutPct,
      targeting,
      assets: assets.map((row) => ({
        id: row.id,
        abi: row.abi.trim().toLowerCase(),
        label: row.label.trim(),
        url: row.url.trim(),
        fileSize: row.fileSize || 0,
        sha256: row.sha256.trim().toLowerCase()
      }))
    };
  }

  async function submit(andPublish: boolean) {
    const payload = validate();
    if (!payload) return;
    try {
      let saved = await save.mutateAsync({ releaseId: item?.id, payload });
      if (andPublish) {
        saved = await publish.mutateAsync({ releaseId: saved.id });
        toast.success(`${saved.version} 已发布`);
      } else {
        toast.success("已保存");
      }
      onSaved?.(saved);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[94vw] max-w-3xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{item ? `编辑 ${item.version}` : "新建版本"}</SheetTitle>
          <SheetDescription>
            {live ? "该版本正在下发，保存后立即生效" : item ? "保存后不改变发布状态" : "新建的版本保存为草稿"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-8 px-6 py-5">
            <Section title="基本信息">
              <div className="grid gap-4 sm:grid-cols-3">
                <Field label="版本名" error={errors.version}>
                  <Input value={form.version} placeholder="1.2.0" onChange={(e) => patch("version", e.target.value)} />
                </Field>
                <Field label="版本码" error={errors.versionCode}>
                  <Input
                    className={DATA_FONT}
                    inputMode="numeric"
                    value={form.versionCode}
                    placeholder="1020099"
                    onChange={(e) => patch("versionCode", e.target.value.replace(/\D/g, ""))}
                  />
                </Field>
                <Field label="平台">
                  <Select value={form.platform} onValueChange={(value) => patch("platform", value)}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {RELEASE_PLATFORMS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              <Field label="标题">
                <Input value={form.title} placeholder="版本标题，可留空" onChange={(e) => patch("title", e.target.value)} />
              </Field>
              <Field label="更新说明">
                <RichEditor
                  value={form.notes}
                  onChange={(html) => patch("notes", html)}
                  placeholder="本次更新的内容"
                  fullscreenable
                />
              </Field>
            </Section>

            <Section title="安装包" description="上传后自动识别 ABI 并计算 SHA-256，也可填写外部下载地址">
              <input
                ref={fileInput}
                type="file"
                multiple
                className="hidden"
                accept=".apk,.aab,.ipa,.exe,.msi,.dmg,.zip,.hap,.appimage,.deb,.rpm"
                onChange={(e) => void handleFiles(e.target.files)}
              />
              {assets.length === 0 ? (
                <div className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
                  暂无安装包
                </div>
              ) : (
                <div className="space-y-3">
                  {assets.map((row) => (
                    <div key={row.key} className="space-y-2 rounded-lg border p-3">
                      <div className="grid gap-2 sm:grid-cols-[10rem_1fr_auto]">
                        <Select value={row.abi} onValueChange={(value) => patchAsset(row.key, { abi: value })}>
                          <SelectTrigger className="h-9">
                            <SelectValue placeholder="类型" />
                          </SelectTrigger>
                          <SelectContent>
                            {Array.from(new Set([...COMMON_ABIS, row.abi].filter(Boolean))).map((abi) => (
                              <SelectItem key={abi} value={abi}>
                                {abi}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Input
                          value={row.label}
                          placeholder="显示名称，可留空"
                          onChange={(e) => patchAsset(row.key, { label: e.target.value })}
                        />
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-9 text-muted-foreground"
                          onClick={() => setAssets((rows) => rows.filter((r) => r.key !== row.key))}
                          aria-label="移除安装包"
                        >
                          <Trash2 className="size-4" />
                        </Button>
                      </div>
                      <Input
                        className={cn("text-xs", DATA_FONT)}
                        value={row.url}
                        placeholder="https:// 或上传得到的 storage:// 引用"
                        onChange={(e) => patchAsset(row.key, { url: e.target.value, downloadUrl: undefined })}
                      />
                      <div className="grid gap-2 sm:grid-cols-[10rem_1fr]">
                        <Input
                          className={cn("text-xs", DATA_FONT)}
                          inputMode="numeric"
                          value={row.fileSize ? String(row.fileSize) : ""}
                          placeholder="大小（字节）"
                          onChange={(e) => patchAsset(row.key, { fileSize: Number(e.target.value.replace(/\D/g, "")) || 0 })}
                        />
                        <Input
                          className={cn("text-xs", DATA_FONT)}
                          value={row.sha256}
                          placeholder="SHA-256，可留空"
                          onChange={(e) => patchAsset(row.key, { sha256: e.target.value })}
                        />
                      </div>
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        {row.fileName ? <span>{row.fileName}</span> : null}
                        <span>{formatBytes(row.fileSize)}</span>
                        {row.downloadUrl ? (
                          <a className="underline-offset-2 hover:underline" href={row.downloadUrl} target="_blank" rel="noreferrer">
                            下载测试
                          </a>
                        ) : null}
                      </div>
                    </div>
                  ))}
                </div>
              )}
              {errors.assets ? <p className="text-xs text-destructive">{errors.assets}</p> : null}
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" disabled={uploading > 0} onClick={() => fileInput.current?.click()}>
                  {uploading > 0 ? <Loader2 className="size-3.5 animate-spin" /> : <FileUp className="size-3.5" />}
                  {uploading > 0 ? `上传中（${uploading}）` : "上传安装包"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    setAssets((rows) => [
                      ...rows,
                      {
                        key: nextKey(),
                        abi: COMMON_ABIS.find((abi) => !rows.some((r) => r.abi === abi)) ?? "",
                        label: "",
                        url: "",
                        fileSize: 0,
                        sha256: ""
                      }
                    ])
                  }
                >
                  <Link2 className="size-3.5" />
                  添加外链
                </Button>
              </div>
            </Section>

            <Section title="更新策略">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="更新类型">
                  <Select value={form.updateType} onValueChange={(value) => patch("updateType", value as ReleaseUpdateType)}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {UPDATE_TYPES.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-[11px] text-muted-foreground">
                    {UPDATE_TYPES.find((option) => option.value === form.updateType)?.hint}
                  </p>
                </Field>
                <Field label="最低支持版本码" error={errors.minSupportedCode}>
                  <Input
                    className={DATA_FONT}
                    inputMode="numeric"
                    value={form.minSupportedCode}
                    placeholder="不限"
                    onChange={(e) => patch("minSupportedCode", e.target.value.replace(/\D/g, ""))}
                  />
                  <p className="text-[11px] text-muted-foreground">低于此版本码的客户端必须更新</p>
                </Field>
                <Field label="最低系统版本（展示用）">
                  <Input value={form.minOsVersion} placeholder="如 Android 7.0" onChange={(e) => patch("minOsVersion", e.target.value)} />
                </Field>
              </div>
            </Section>

            <Section title="受众">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="渠道">
                  <Select value={form.channelId} onValueChange={(value) => patch("channelId", value)}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">不限渠道</SelectItem>
                      {channels.map((channel) => (
                        <SelectItem key={channel.id} value={String(channel.id)}>
                          <span className="flex items-center gap-2">
                            <ChannelDot color={channel.color} />
                            {channel.name}
                            {channel.is_default ? <span className="text-muted-foreground">（默认）</span> : null}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="可见范围">
                  <Select value={form.visibility} onValueChange={(value) => patch("visibility", value as ReleaseVisibility)}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {VISIBILITIES.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-[11px] text-muted-foreground">
                    {VISIBILITIES.find((option) => option.value === form.visibility)?.hint}
                  </p>
                </Field>
              </div>

              <div className="space-y-2 rounded-lg border px-4 py-3">
                <div className="flex items-center justify-between">
                  <Label className="text-xs">灰度比例</Label>
                  <span className={cn("text-sm font-semibold", DATA_FONT)}>{form.rolloutPct}%</span>
                </div>
                <Slider
                  value={[form.rolloutPct]}
                  min={0}
                  max={100}
                  step={1}
                  onValueChange={(value) => patch("rolloutPct", value[0] ?? 0)}
                />
                <div className="flex flex-wrap gap-1.5">
                  {ROLLOUT_PRESETS.map((pct) => (
                    <Button
                      key={pct}
                      variant={form.rolloutPct === pct ? "default" : "outline"}
                      size="sm"
                      className="h-7 px-2.5 text-xs"
                      onClick={() => patch("rolloutPct", pct)}
                    >
                      {pct}%
                    </Button>
                  ))}
                </div>
                <p className="text-[11px] text-muted-foreground">
                  按用户（未登录时按设备）稳定分桶，扩大比例时已覆盖的用户保持不变。内测名单不受灰度限制。
                </p>
              </div>
            </Section>

            <Section title="定向条件" description="均为可选，同时设置时需全部满足">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="地区码">
                  <Input value={form.regions} placeholder="CN, HK" onChange={(e) => patch("regions", e.target.value)} />
                </Field>
                <Field label="语言">
                  <Input value={form.locales} placeholder="zh, en-US" onChange={(e) => patch("locales", e.target.value)} />
                </Field>
                <Field label="机型关键字">
                  <Input value={form.deviceModels} placeholder="Pixel, Xiaomi" onChange={(e) => patch("deviceModels", e.target.value)} />
                </Field>
                <Field label="ABI">
                  <div className="flex flex-wrap gap-1.5">
                    {COMMON_ABIS.filter((abi) => abi !== "universal").map((abi) => {
                      const active = form.abis.includes(abi);
                      return (
                        <Button
                          key={abi}
                          variant={active ? "default" : "outline"}
                          size="sm"
                          className={cn("h-7 px-2 text-xs", DATA_FONT)}
                          onClick={() =>
                            patch("abis", active ? form.abis.filter((item) => item !== abi) : [...form.abis, abi])
                          }
                        >
                          {abi}
                        </Button>
                      );
                    })}
                  </div>
                </Field>
                <Field label="系统版本（API Level）" error={errors.minOsApi}>
                  <RangeInputs
                    min={form.minOsApi}
                    max={form.maxOsApi}
                    onMin={(value) => patch("minOsApi", value)}
                    onMax={(value) => patch("maxOsApi", value)}
                  />
                </Field>
                <Field label="来源版本码" error={errors.minSourceCode}>
                  <RangeInputs
                    min={form.minSourceCode}
                    max={form.maxSourceCode}
                    onMin={(value) => patch("minSourceCode", value)}
                    onMax={(value) => patch("maxSourceCode", value)}
                  />
                </Field>
              </div>
            </Section>

            <Section title="名单" description="每行一个，或以逗号分隔">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="内测用户 ID">
                  <Textarea rows={3} className={DATA_FONT} value={form.testerUserIds} onChange={(e) => patch("testerUserIds", e.target.value)} />
                </Field>
                <Field label="内测设备 ID">
                  <Textarea rows={3} className={DATA_FONT} value={form.testerDeviceIds} onChange={(e) => patch("testerDeviceIds", e.target.value)} />
                </Field>
                <Field label="排除用户 ID">
                  <Textarea rows={3} className={DATA_FONT} value={form.excludeUserIds} onChange={(e) => patch("excludeUserIds", e.target.value)} />
                </Field>
                <Field label="排除设备 ID">
                  <Textarea rows={3} className={DATA_FONT} value={form.excludeDeviceIds} onChange={(e) => patch("excludeDeviceIds", e.target.value)} />
                </Field>
              </div>
              <p className="text-[11px] text-muted-foreground">内测名单跳过定向条件与灰度；排除名单优先于一切。</p>
            </Section>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button variant={canPublish ? "outline" : "default"} size="sm" disabled={busy} onClick={() => void submit(false)}>
            {save.isPending && !publish.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {canPublish ? "保存草稿" : "保存"}
          </Button>
          {canPublish ? (
            <Button size="sm" disabled={busy || assets.length === 0} onClick={() => void submit(true)}>
              <Plus className="size-3.5" />
              保存并发布
            </Button>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Section({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
        {description ? <p className="mt-0.5 text-xs text-muted-foreground">{description}</p> : null}
      </div>
      {children}
    </section>
  );
}

function Field({ label, error, children }: { label: string; error?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </div>
  );
}

function RangeInputs({
  min,
  max,
  onMin,
  onMax
}: {
  min: string;
  max: string;
  onMin: (value: string) => void;
  onMax: (value: string) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Input className={DATA_FONT} inputMode="numeric" value={min} placeholder="不限" onChange={(e) => onMin(e.target.value.replace(/\D/g, ""))} />
      <span className="text-xs text-muted-foreground">至</span>
      <Input className={DATA_FONT} inputMode="numeric" value={max} placeholder="不限" onChange={(e) => onMax(e.target.value.replace(/\D/g, ""))} />
    </div>
  );
}
