"use client";

import { useQuery } from "@tanstack/react-query";
import { Cpu, HardDrive, MemoryStick, Monitor, Server, Hash, Timer, Activity } from "lucide-react";
import {
  SiLinux,
  SiApple,
  SiUbuntu,
  SiDebian,
  SiCentos,
  SiAlpinelinux,
  SiRedhat,
  SiGo,
} from "@icons-pack/react-simple-icons";
import { useAuthStore } from "@/lib/auth-store";
import { usePermissionChecker } from "@/lib/permissions";
import { getSystemRuntimeInfo, type SystemRuntimeInfo } from "@/lib/api/system";
import { LoadingState } from "@/components/ui/data-state";

// ── OS 图标映射 ──

function resolveOSIcon(os: string, platform: string) {
  const p = platform.toLowerCase();
  const o = os.toLowerCase();

  if (p.includes("ubuntu")) return { Icon: SiUbuntu, color: "#E95420" };
  if (p.includes("debian")) return { Icon: SiDebian, color: "#A81D33" };
  if (p.includes("centos")) return { Icon: SiCentos, color: "#262577" };
  if (p.includes("red hat") || p.includes("rhel")) return { Icon: SiRedhat, color: "#EE0000" };
  if (p.includes("alpine")) return { Icon: SiAlpinelinux, color: "#0D597F" };
  if (o === "windows") return { Icon: Monitor, color: "#0078D4" };
  if (o === "darwin") return { Icon: SiApple, color: "#999999" };
  if (o === "linux") return { Icon: SiLinux, color: "#FCC624" };
  return { Icon: Server, color: undefined };
}

function fmtBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function fmtUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d} 天 ${h} 小时`;
  if (h > 0) return `${h} 小时 ${m} 分钟`;
  return `${m} 分钟`;
}

// ── 进度条 ──

function ProgressBar({ value, color = "bg-primary" }: { value: number; color?: string }) {
  const pct = Math.min(100, Math.max(0, value));
  const barColor = pct > 90 ? "bg-destructive" : pct > 70 ? "bg-amber-500" : color;
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
      <div className={`h-full rounded-full transition-all duration-500 ${barColor}`} style={{ width: `${pct}%` }} />
    </div>
  );
}

// ── 主组件 ──

export function SystemInfoPanel() {
  const accessToken = useAuthStore((s) => s.accessToken);
  const canRead = usePermissionChecker()("system:config:read");

  const query = useQuery({
    queryKey: ["system-runtime-info", accessToken],
    queryFn: () => getSystemRuntimeInfo(accessToken!),
    enabled: Boolean(accessToken && canRead),
    refetchInterval: 30_000,
  });

  if (!canRead) return null;
  if (query.isLoading) return <LoadingState title="加载系统信息" />;
  if (!query.data) return null;

  const info = query.data;
  const { Icon: OSIcon, color: osColor } = resolveOSIcon(info.os, info.platform);
  const goHeapPct = info.memSys > 0 ? (info.memAlloc / info.memSys) * 100 : 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Monitor className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">主机与进程</h3>
        <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground">
          <span>进程号 <span className="font-data text-foreground">{info.pid}</span></span>
          <span>已运行 <span className="text-foreground">{fmtUptime(info.uptimeSeconds)}</span></span>
        </span>
      </div>

      {/* 资源概览 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        {/* 操作系统 */}
        <div className="flex items-center gap-3 rounded-lg border px-4 py-3">
          <OSIcon className="size-8 shrink-0" style={osColor ? { color: osColor } : undefined} />
          <div className="min-w-0">
            <div className="text-sm font-semibold truncate">{info.platform || info.os}</div>
            <div className="text-[10px] text-muted-foreground truncate">{info.platformVersion} {info.kernelArch}</div>
          </div>
        </div>

        {/* CPU */}
        <div className="space-y-2 rounded-lg border px-4 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              <Cpu className="size-4 text-muted-foreground" />
              <span className="text-xs font-medium">CPU</span>
            </div>
            <span className="text-xs font-semibold">{info.cpuUsage.toFixed(1)}%</span>
          </div>
          <ProgressBar value={info.cpuUsage} />
          <div className="text-[10px] text-muted-foreground truncate">{info.cpuModel}</div>
          <div className="text-[10px] text-muted-foreground">{info.cpuCores}C / {info.cpuThreads}T</div>
        </div>

        {/* 内存 */}
        <div className="space-y-2 rounded-lg border px-4 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              <MemoryStick className="size-4 text-muted-foreground" />
              <span className="text-xs font-medium">内存</span>
            </div>
            <span className="text-xs font-semibold">{info.memUsedPercent.toFixed(1)}%</span>
          </div>
          <ProgressBar value={info.memUsedPercent} />
          <div className="text-[10px] text-muted-foreground">{fmtBytes(info.memUsed)} / {fmtBytes(info.memTotal)}</div>
        </div>

        {/* 磁盘 */}
        <div className="space-y-2 rounded-lg border px-4 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              <HardDrive className="size-4 text-muted-foreground" />
              <span className="text-xs font-medium">磁盘</span>
            </div>
            <span className="text-xs font-semibold">{info.diskUsedPercent.toFixed(1)}%</span>
          </div>
          <ProgressBar value={info.diskUsedPercent} />
          <div className="text-[10px] text-muted-foreground">{fmtBytes(info.diskUsed)} / {fmtBytes(info.diskTotal)}</div>
        </div>
      </div>

      {/* Go 运行时 + 详情 */}
      <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-6">
        <InfoCell icon={SiGo} iconColor="#00ADD8" label="Go" value={info.goVersion.replace("go", "")} />
        <InfoCell icon={Activity} label="Goroutines" value={String(info.goroutines)} />
        <InfoCell icon={MemoryStick} label="堆内存" value={fmtBytes(info.memAlloc)} sub={`/ ${fmtBytes(info.memSys)}`} />
        <InfoCell icon={Hash} label="GC" value={`${info.numGC} 次`} />
        <InfoCell icon={Server} label="进程内存" value={fmtBytes(info.processMemory)} />
        <InfoCell icon={Timer} label="主机名" value={info.hostname} />
      </div>

      {/* Go 堆使用率 */}
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span>Go 堆占用：{fmtBytes(info.memAlloc)} / {fmtBytes(info.memSys)} ({goHeapPct.toFixed(1)}%)</span>
        <div className="flex-1"><ProgressBar value={goHeapPct} color="bg-sky-500" /></div>
        <span>累计分配：{fmtBytes(info.memTotalAlloc)}</span>
      </div>
    </div>
  );
}

// ── 辅助 ──

function InfoCell({ icon: Icon, iconColor, label, value, sub }: {
  icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }>;
  iconColor?: string;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
      <Icon className="size-4 shrink-0 text-muted-foreground" style={iconColor ? { color: iconColor } : undefined} />
      <div className="min-w-0">
        <div className="text-[10px] text-muted-foreground">{label}</div>
        <div className="text-xs font-semibold truncate">
          {value}
          {sub && <span className="font-normal text-muted-foreground"> {sub}</span>}
        </div>
      </div>
    </div>
  );
}
