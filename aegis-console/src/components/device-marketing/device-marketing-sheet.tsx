"use client";

import Image from "next/image";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Apple, ImageOff, Loader2, Save, Smartphone, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ApiError } from "@/lib/api-client";
import {
  type DeviceMarketingName,
  type DevicePlatform,
} from "@/lib/api/device-marketing";
import {
  useCreateDeviceMarketingMutation,
  useDeleteDeviceMarketingMutation,
  useUpdateDeviceMarketingMutation,
} from "@/lib/device-marketing-hooks";
import { cn } from "@/lib/utils";

type Mode = "create" | "edit";

export function DeviceMarketingSheet({
  open,
  mode,
  record,
  onOpenChange,
}: {
  open: boolean;
  mode: Mode;
  record: DeviceMarketingName | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [platform, setPlatform] = useState<DevicePlatform>("android");
  const [identifier, setIdentifier] = useState("");
  const [marketingName, setMarketingName] = useState("");
  const [manufacturer, setManufacturer] = useState("");
  const [manufacturerIconUrl, setManufacturerIconUrl] = useState("");
  const [deviceImageUrl, setDeviceImageUrl] = useState("");
  const [notes, setNotes] = useState("");

  const createMutation = useCreateDeviceMarketingMutation();
  const updateMutation = useUpdateDeviceMarketingMutation();
  const deleteMutation = useDeleteDeviceMarketingMutation();
  const pending = createMutation.isPending || updateMutation.isPending || deleteMutation.isPending;

  useEffect(() => {
    if (!open) return;
    if (mode === "edit" && record) {
      setPlatform(record.platform);
      setIdentifier(record.identifier);
      setMarketingName(record.marketingName);
      setManufacturer(record.manufacturer ?? "");
      setManufacturerIconUrl(record.manufacturerIconUrl ?? "");
      setDeviceImageUrl(record.deviceImageUrl ?? "");
      setNotes(record.notes ?? "");
    } else {
      setPlatform("android");
      setIdentifier("");
      setMarketingName("");
      setManufacturer("");
      setManufacturerIconUrl("");
      setDeviceImageUrl("");
      setNotes("");
    }
  }, [open, mode, record]);

  const handleSave = async () => {
    const payload = {
      platform,
      identifier: identifier.trim(),
      marketingName: marketingName.trim(),
      manufacturer: manufacturer.trim(),
      manufacturerIconUrl: manufacturerIconUrl.trim(),
      deviceImageUrl: deviceImageUrl.trim(),
      notes: notes.trim(),
    };
    if (!payload.identifier || !payload.marketingName) {
      toast.error("标识符和营销名称均不能为空");
      return;
    }
    try {
      if (mode === "create") {
        await createMutation.mutateAsync(payload);
        toast.success("新增成功");
      } else if (record) {
        await updateMutation.mutateAsync({ id: record.id, payload });
        toast.success("更新成功");
      }
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const handleDelete = async () => {
    if (!record) return;
    if (!confirm(`确认删除 ${record.platform}/${record.identifier} → ${record.marketingName} ?`)) {
      return;
    }
    try {
      await deleteMutation.mutateAsync(record.id);
      toast.success("已删除");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full sm:max-w-2xl p-0 flex flex-col gap-0"
        style={{ maxWidth: "min(44rem, 94vw)" }}
      >
        <SheetHeader className="px-6 pt-6 pb-4 border-b">
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant="outline" className="tracking-normal font-normal">
              {mode === "create" ? "新增" : "编辑"}
            </Badge>
            {record && (
              <Badge variant="secondary" className="tracking-normal font-normal">
                来源：{record.source}
              </Badge>
            )}
            {record && (
              <span className="ml-auto text-xs text-muted-foreground font-mono">#{record.id}</span>
            )}
          </div>
          <SheetTitle className="text-base leading-6">
            {mode === "create" ? "新增设备营销名称" : record?.marketingName}
          </SheetTitle>
          <SheetDescription className="text-xs">
            {mode === "create"
              ? "将机器标识码（如 iPhone14,3 / SM-G998B）映射为人类可读名称，并关联厂商与产品图"
              : "修改该条映射，或从字典中删除"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="px-6 py-5 space-y-5">
            {/* 预览卡 */}
            <div className="rounded-xl border bg-muted/30 p-4">
              <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground mb-3">
                预览
              </div>
              <div className="flex items-start gap-4">
                <DevicePreviewImage url={deviceImageUrl} platform={platform} />
                <div className="flex-1 min-w-0 space-y-1.5">
                  <div className="flex items-center gap-2 flex-wrap">
                    <ManufacturerBadge name={manufacturer} iconUrl={manufacturerIconUrl} />
                    <PlatformChip platform={platform} />
                  </div>
                  <div className="font-semibold truncate">{marketingName || "未填写名称"}</div>
                  <code className="block font-mono text-[11px] text-muted-foreground truncate">
                    {identifier || "未填写标识符"}
                  </code>
                </div>
              </div>
            </div>

            <FieldRow>
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                  平台
                </Label>
                <Select
                  value={platform}
                  onValueChange={(v) => setPlatform(v as DevicePlatform)}
                  disabled={mode === "edit"}
                >
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ios">
                      <span className="flex items-center gap-2">
                        <Apple className="size-3.5" /> iOS
                      </span>
                    </SelectItem>
                    <SelectItem value="android">
                      <span className="flex items-center gap-2">
                        <Smartphone className="size-3.5" /> Android
                      </span>
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                  厂商
                </Label>
                <Input
                  className="h-9"
                  value={manufacturer}
                  onChange={(e) => setManufacturer(e.target.value)}
                  placeholder="如 Apple / Samsung / Xiaomi"
                />
              </div>
            </FieldRow>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                标识符（iOS machine id 或 Android Build.MODEL）
              </Label>
              <Input
                className="h-9 font-mono"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                placeholder={platform === "ios" ? "iPhone14,3" : "SM-G998B"}
                disabled={mode === "edit"}
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                营销名称
              </Label>
              <Input
                className="h-9"
                value={marketingName}
                onChange={(e) => setMarketingName(e.target.value)}
                placeholder={platform === "ios" ? "iPhone 13 Pro Max" : "Galaxy S21 Ultra 5G"}
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                厂商图标 URL
              </Label>
              <Input
                className="h-9 font-mono"
                value={manufacturerIconUrl}
                onChange={(e) => setManufacturerIconUrl(e.target.value)}
                placeholder="https://.../logo.png"
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                设备展示图 URL
              </Label>
              <Input
                className="h-9 font-mono"
                value={deviceImageUrl}
                onChange={(e) => setDeviceImageUrl(e.target.value)}
                placeholder="https://.../device-render.png"
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground uppercase tracking-widest">
                备注
              </Label>
              <Textarea
                rows={3}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="（可选）人工补录说明"
              />
            </div>

            {mode === "edit" && record && (
              <>
                <Separator />
                <div className="text-[11px] text-muted-foreground space-y-1">
                  <div>
                    创建于：
                    <span className="font-mono">{new Date(record.createdAt).toLocaleString("zh-CN")}</span>
                  </div>
                  <div>
                    最近更新：
                    <span className="font-mono">{new Date(record.updatedAt).toLocaleString("zh-CN")}</span>
                  </div>
                </div>
              </>
            )}
          </div>
        </ScrollArea>

        <div className={cn("flex items-center justify-between gap-2 border-t px-6 py-3")}>
          {mode === "edit" ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="text-red-600 hover:text-red-700"
              onClick={handleDelete}
              disabled={pending}
            >
              <Trash2 className="size-3.5" />
              删除
            </Button>
          ) : <span />}
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={pending}>
              取消
            </Button>
            <Button size="sm" onClick={handleSave} disabled={pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ─── 小组件 ────────────────────────────────────

function FieldRow({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">{children}</div>;
}

function DevicePreviewImage({ url, platform }: { url: string; platform: DevicePlatform }) {
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [url]);
  if (!url || broken) {
    return (
      <div className="flex size-20 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
        {platform === "ios" ? <Apple className="size-7" /> : <Smartphone className="size-7" />}
      </div>
    );
  }
  return (
    <div className="relative size-20 shrink-0 overflow-hidden rounded-lg border bg-background">
      {/* 用原生 img：目标是任意来源 URL，避免 next/image 域名白名单限制 */}
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={url}
        alt="device"
        className="size-full object-contain"
        onError={() => setBroken(true)}
      />
    </div>
  );
}

function ManufacturerBadge({ name, iconUrl }: { name: string; iconUrl: string }) {
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [iconUrl]);
  if (!name) {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[10px] text-muted-foreground">
        未设置厂商
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-0.5 text-[11px] font-medium">
      {iconUrl && !broken ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={iconUrl}
          alt={name}
          className="size-3.5 rounded-sm object-contain"
          onError={() => setBroken(true)}
        />
      ) : (
        <ImageOff className="size-3 text-muted-foreground" />
      )}
      {name}
    </span>
  );
}

function PlatformChip({ platform }: { platform: DevicePlatform }) {
  const isIOS = platform === "ios";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[10px] font-medium",
        isIOS
          ? "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-300"
          : "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
      )}
    >
      {isIOS ? <Apple className="size-3" /> : <Smartphone className="size-3" />}
      {isIOS ? "iOS" : "Android"}
    </span>
  );
}

// 保持 Image import 不被摇树掉（便于未来扩展）
const _keep = Image;
void _keep;
