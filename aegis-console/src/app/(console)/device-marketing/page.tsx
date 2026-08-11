"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  Apple,
  Database,
  Filter,
  ImageOff,
  Loader2,
  Plus,
  RefreshCw,
  Search,
  Smartphone,
} from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { LoadingState } from "@/components/ui/data-state";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useIsSuperAdmin } from "@/lib/permissions";
import {
  type DeviceMarketingName,
  type DevicePlatform,
  type DeviceSource,
} from "@/lib/api/device-marketing";
import {
  useDeviceManufacturersQuery,
  useDeviceMarketingListQuery,
  useReseedDeviceMarketingMutation,
} from "@/lib/device-marketing-hooks";
import { DeviceMarketingSheet } from "@/components/device-marketing/device-marketing-sheet";
import { cn } from "@/lib/utils";
import { ApiError } from "@/lib/api-client";

// ─── 常量 ─────────────────────────────────────

const PLATFORM_OPTIONS = [
  { value: "all", label: "全部平台" },
  { value: "ios", label: "iOS" },
  { value: "android", label: "Android" },
];

const SOURCE_OPTIONS = [
  { value: "all", label: "全部来源" },
  { value: "seed", label: "种子" },
  { value: "manual", label: "手工" },
  { value: "import", label: "批量导入" },
];

// ─── 主页面 ────────────────────────────────────

export default function DeviceMarketingPage() {
  const isSuperAdmin = useIsSuperAdmin();
  const [platform, setPlatform] = useState<DevicePlatform | "">("");
  const [source, setSource] = useState<DeviceSource | "">("");
  const [manufacturer, setManufacturer] = useState("");
  const [keyword, setKeyword] = useState("");
  const [keywordInput, setKeywordInput] = useState("");
  // 搜索防抖 300ms，避免每次按键都请求
  useEffect(() => {
    const t = setTimeout(() => {
      setKeyword(keywordInput);
      setPage(1);
    }, 300);
    return () => clearTimeout(t);
  }, [keywordInput]);
  const [page, setPage] = useState(1);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [sheetMode, setSheetMode] = useState<"create" | "edit">("create");
  const [selected, setSelected] = useState<DeviceMarketingName | null>(null);

  const listQuery = useDeviceMarketingListQuery({
    platform,
    source,
    manufacturer,
    keyword,
    page,
    limit: 30,
  });
  const manufacturersQuery = useDeviceManufacturersQuery(platform);
  const reseedMutation = useReseedDeviceMarketingMutation();

  // 平台变更时清掉厂商筛选（避免 stale 值）
  useEffect(() => {
    setManufacturer("");
  }, [platform]);

  const data = listQuery.data;
  const totalPages = useMemo(() => {
    if (!data) return 1;
    return Math.max(1, Math.ceil(data.total / data.limit));
  }, [data]);

  if (!isSuperAdmin) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="控制台" title="设备营销名称字典" />
        <div className="rounded-xl border bg-card px-6 py-12 text-center">
          <p className="text-sm text-muted-foreground">仅超级管理员可访问此页面</p>
        </div>
      </div>
    );
  }

  const handleReseed = async () => {
    try {
      const stats = await reseedMutation.mutateAsync();
      toast.success(
        `重新种子完成：解析 ${stats.parsed}，影响 ${stats.inserted}（耗时 ${stats.elapsedMs}ms）`,
      );
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重新种子失败");
    }
  };

  const openCreate = () => {
    setSheetMode("create");
    setSelected(null);
    setSheetOpen(true);
  };
  const openEdit = (record: DeviceMarketingName) => {
    setSheetMode("edit");
    setSelected(record);
    setSheetOpen(true);
  };

  const manufacturerList = manufacturersQuery.data?.items ?? [];

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="平台字典"
        title="设备营销名称字典"
        description="将 iOS 的 machine identifier 与 Android 的 Build.MODEL 翻译为人类可读名称，关联厂商与产品图。"
      />

      {/* 过滤 & 操作 */}
      <div className="rounded-xl border bg-card p-3 space-y-2.5">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="relative flex-1 min-w-[240px]">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
            <Input
              className="h-8 pl-8 text-xs"
              placeholder='模糊搜索：支持 "17 Pro Max" / "17promax" / "SM-G998B" / "华为"...'
              value={keywordInput}
              onChange={(e) => setKeywordInput(e.target.value)}
            />
          </div>
          <Select
            value={platform || "all"}
            onValueChange={(v) => {
              setPlatform(v === "all" ? "" : (v as DevicePlatform));
              setPage(1);
            }}
          >
            <SelectTrigger className="w-28 h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PLATFORM_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={manufacturer || "all"}
            onValueChange={(v) => {
              setManufacturer(v === "all" ? "" : v);
              setPage(1);
            }}
            disabled={manufacturersQuery.isLoading}
          >
            <SelectTrigger className="w-36 h-8 text-xs">
              <SelectValue placeholder="全部厂商" />
            </SelectTrigger>
            <SelectContent className="max-h-72">
              <SelectItem value="all">全部厂商</SelectItem>
              {manufacturerList.map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={source || "all"}
            onValueChange={(v) => {
              setSource(v === "all" ? "" : (v as DeviceSource));
              setPage(1);
            }}
          >
            <SelectTrigger className="w-28 h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SOURCE_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="flex-1" />
          <Button
            variant="outline"
            size="sm"
            className="h-8"
            onClick={handleReseed}
            disabled={reseedMutation.isPending}
          >
            {reseedMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            重新种子
          </Button>
          <Button size="sm" className="h-8" onClick={openCreate}>
            <Plus className="size-3.5" />
            新增
          </Button>
        </div>

        {data && (
          <div className="flex items-center gap-3 text-[11px] text-muted-foreground font-mono">
            <span className="inline-flex items-center gap-1">
              <Database className="size-3" />
              共 <span className="text-foreground font-semibold">{data.total}</span> 条
            </span>
            {manufacturerList.length > 0 && (
              <span className="inline-flex items-center gap-1">
                收录厂商 <span className="text-foreground font-semibold">{manufacturerList.length}</span> 家
              </span>
            )}
          </div>
        )}
      </div>

      {/* 表格 —— 卡片化单行布局：左侧设备图 + 中段核心信息 + 右侧元数据 */}
      {listQuery.isLoading ? (
        <LoadingState title="加载中" description="正在读取字典数据" />
      ) : (data?.items?.length ?? 0) === 0 ? (
        <div className="rounded-xl border bg-card px-6 py-16 text-center text-muted-foreground">
          <div className="inline-flex flex-col items-center gap-2">
            <Filter className="size-6 opacity-40" />
            <span className="text-sm">暂无符合条件的字典记录</span>
          </div>
        </div>
      ) : (
        <div className="divide-y rounded-xl border overflow-hidden bg-card">
          {(data?.items ?? []).map((row) => (
            <div
              key={row.id}
              className="group flex items-center gap-4 px-4 py-3 hover:bg-accent/40 cursor-pointer transition-colors"
              onClick={() => openEdit(row)}
            >
              {/* 设备图 */}
              <DeviceThumb url={row.deviceImageUrl} platform={row.platform} />

              {/* 中段核心字段 */}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap mb-1">
                  <PlatformBadge platform={row.platform} />
                  <ManufacturerCell name={row.manufacturer} iconUrl={row.manufacturerIconUrl} />
                  <SourceBadge source={row.source} />
                </div>
                <div className="truncate text-sm font-semibold" title={row.marketingName}>
                  {row.marketingName || row.identifier}
                </div>
                <code className="block truncate font-mono text-[11px] text-muted-foreground mt-0.5">
                  {row.identifier}
                </code>
              </div>

              {/* 右侧更新时间 */}
              <div className="hidden lg:flex flex-col items-end text-[11px] text-muted-foreground font-mono shrink-0 gap-0.5">
                <span>更新</span>
                <span>
                  {new Date(row.updatedAt).toLocaleString("zh-CN", {
                    month: "2-digit",
                    day: "2-digit",
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 分页 */}
      {data && data.total > 0 && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            第 <span className="font-semibold text-foreground">{page}</span> / {totalPages} 页
          </span>
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-7"
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-7"
              disabled={page >= totalPages}
              onClick={() => setPage(page + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      )}

      <DeviceMarketingSheet
        open={sheetOpen}
        mode={sheetMode}
        record={selected}
        onOpenChange={setSheetOpen}
      />
    </div>
  );
}

// ─── 表格原子 ──────────────────────────────────

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="px-3 py-2.5 text-left font-medium uppercase tracking-wider text-[10px]">
      {children}
    </th>
  );
}
function Td({ children, className }: { children: React.ReactNode; className?: string }) {
  return <td className={cn("px-3 py-2 align-middle", className)}>{children}</td>;
}

// 设备缩略图（列表中展示）
function DeviceThumb({ url, platform }: { url: string; platform: DevicePlatform }) {
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [url]);
  const base =
    "flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-lg border bg-gradient-to-br";
  if (!url || broken) {
    return (
      <div
        className={cn(
          base,
          platform === "ios"
            ? "from-sky-50 to-sky-100 text-sky-600 dark:from-sky-950/60 dark:to-sky-900/30 dark:text-sky-400"
            : "from-emerald-50 to-emerald-100 text-emerald-600 dark:from-emerald-950/60 dark:to-emerald-900/30 dark:text-emerald-400",
        )}
      >
        {platform === "ios" ? <Apple className="size-5" /> : <Smartphone className="size-5" />}
      </div>
    );
  }
  return (
    <div className={cn(base, "from-background to-background")}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={url}
        alt="device"
        loading="lazy"
        className="size-full object-contain p-1"
        onError={() => setBroken(true)}
      />
    </div>
  );
}

function ManufacturerCell({ name, iconUrl }: { name: string; iconUrl: string }) {
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [iconUrl]);
  if (!name) {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-dashed px-1.5 py-0.5 text-[10px] text-muted-foreground/70">
        未分类
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border bg-background px-1.5 py-0.5 text-[11px] font-medium">
      {iconUrl && !broken ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={iconUrl}
          alt={name}
          loading="lazy"
          className="size-3.5 rounded-sm object-contain"
          onError={() => setBroken(true)}
        />
      ) : (
        <ImageOff className="size-3 text-muted-foreground" />
      )}
      <span className="truncate max-w-[140px]">{name}</span>
    </span>
  );
}

function PlatformBadge({ platform }: { platform: DevicePlatform }) {
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

function SourceBadge({ source }: { source: DeviceSource }) {
  const map: Record<DeviceSource, { label: string; variant: "secondary" | "outline" | "success" }> = {
    seed: { label: "种子", variant: "secondary" },
    manual: { label: "手工", variant: "success" },
    import: { label: "导入", variant: "outline" },
  };
  const entry = map[source] ?? map.seed;
  return (
    <Badge variant={entry.variant} className="text-[9px] tracking-normal">
      {entry.label}
    </Badge>
  );
}
