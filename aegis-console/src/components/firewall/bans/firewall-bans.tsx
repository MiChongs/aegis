"use client";

import { useRef, useState } from "react";
import { ChevronRight, Info, Search, ShieldBan } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Segmented, TabStack } from "@/components/traffic/traffic-kit";
import type { IPBanEntry } from "@/lib/api/types";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { BanEditDialog, BatchBanDialog, UnbanDialog, type BanSeed, type EditRequest, type EditTarget, type UnbanRequest, type UnbanTarget } from "./ban-dialogs";
import { InspectDialog, LAYERS, type InspectSeed } from "./ban-inspector";
import { BanList } from "./ban-list";
import { useBanListState, type BanListFilter } from "./ban-list-state";
import { BanOverview } from "./ban-overview";
import { GeoRules } from "./geo-rules";

/**
 * 防火墙 → 封禁。
 *
 * IP 封禁：概况（指标、近 14 天、分布）→ 列表（筛选、多选、批量解封 / 修改 / 导出）。
 * 地域封禁：国家 / 省州 / 城市 / ASN / 运营商规则，可批量启停与删除。
 * 页头的 IP 查询回答「这个地址现在会不会被拦、被哪一层拦」，两个视图都能用。
 *
 * 判定顺序：IP 精确封禁 → 地域规则 → 地理围栏，先命中的生效；封禁排在可信网段之前（可信网段也挡不住封禁）。
 * 自动封禁只数 action = block 的事件，观察模式下的「本该拦截」不计数（docs/firewall.md §6）。
 * 写操作需要 system:firewall:ban，没有时整页只读。
 */

type View = "ip" | "geo";

export function FirewallBansTab() {
  const canBan = usePermissionChecker()("system:firewall:ban");
  const [view, setView] = useState<View>("ip");
  const list = useBanListState();

  const [inspect, setInspect] = useState<InspectSeed | null>(null);
  const [banSeed, setBanSeed] = useState<BanSeed | null>(null);
  const [unbanTarget, setUnbanTarget] = useState<UnbanTarget | null>(null);
  const [editTarget, setEditTarget] = useState<EditTarget | null>(null);

  const listRef = useRef<HTMLDivElement>(null);
  // 概况里点了某一项：套上条件并把列表滚到眼前
  const applyFromOverview = (patch: Partial<BanListFilter>) => {
    list.update(patch);
    listRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  const openInspect = (ip: string) => setInspect({ key: Date.now(), ip });
  const openUnban = (target: UnbanRequest) => setUnbanTarget({ ...target, key: Date.now() });
  const openEdit = (target: EditRequest) => setEditTarget({ ...target, key: Date.now() });

  // 从 IP 查询里发起的操作：先关掉查询窗，避免两层弹窗叠着
  const fromInspector = {
    onUnban: (ban: IPBanEntry) => {
      setInspect(null);
      openUnban({ kind: "ids", rows: [ban] });
    },
    onEdit: (ban: IPBanEntry) => {
      setInspect(null);
      openEdit({ kind: "single", ban });
    },
    onBan: (ip: string) => {
      setInspect(null);
      setBanSeed({ key: Date.now(), ips: ip });
    },
    onShowGeo: () => {
      setInspect(null);
      setView("geo");
    }
  };

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <Segmented
          value={view}
          onChange={setView}
          size="default"
          ariaLabel="封禁类型"
          options={[
            { value: "ip", label: "IP 封禁" },
            { value: "geo", label: "地域封禁" }
          ]}
        />
        <EvaluationOrder current={view === "ip" ? 0 : 1} />
        <div className="flex min-w-0 flex-1 items-center justify-end gap-2 basis-full @2xl:basis-auto">
          <InspectSearch onSubmit={openInspect} />
          {canBan && view === "ip" ? (
            <Button variant="destructive" onClick={() => setBanSeed({ key: Date.now() })}>
              <ShieldBan />
              封禁 IP
            </Button>
          ) : null}
        </div>
      </div>

      {view === "ip" ? (
        <>
          <BanOverview onApply={applyFromOverview} />
          <div ref={listRef} className="min-w-0 scroll-mt-4">
            <BanList state={list} canBan={canBan} onInspect={openInspect} onUnban={openUnban} onEdit={openEdit} />
          </div>
        </>
      ) : (
        <GeoRules canBan={canBan} />
      )}

      <InspectDialog seed={inspect} onClose={() => setInspect(null)} canBan={canBan} {...fromInspector} />
      <BatchBanDialog seed={banSeed} onClose={() => setBanSeed(null)} />
      <UnbanDialog
        target={unbanTarget}
        onClose={() => setUnbanTarget(null)}
        onDone={() => {
          // 单条解封只把它从选择里拿掉；批量解封后清空选择
          if (unbanTarget?.kind === "ids" && unbanTarget.rows.length === 1) list.dropFromSelection(unbanTarget.rows[0]!.id);
          else list.clearSelection();
          setUnbanTarget(null);
        }}
      />
      <BanEditDialog
        target={editTarget}
        onClose={() => setEditTarget(null)}
        onDone={() => {
          if (editTarget && editTarget.kind !== "single") list.clearSelection();
          setEditTarget(null);
        }}
      />
    </TabStack>
  );
}

/** 判定顺序：三个小步骤 + 说明浮层 */
function EvaluationOrder({ current }: { current: number }) {
  return (
    <div className="flex min-w-0 items-center gap-1" aria-label="封禁的判定顺序">
      <ol className="flex min-w-0 items-center gap-0.5">
        {LAYERS.map((layer, index) => (
          <li key={layer.key} className="flex min-w-0 items-center gap-0.5">
            <span
              title={layer.hint}
              className={cn(
                "inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-[11px] whitespace-nowrap",
                index === current ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground"
              )}
            >
              <span
                className={cn(
                  "grid size-4 place-items-center rounded-full text-[10px] font-semibold tabular-nums",
                  index === current ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
                )}
              >
                {index + 1}
              </span>
              {layer.label}
            </span>
            {index < LAYERS.length - 1 ? <ChevronRight aria-hidden className="size-3 shrink-0 text-muted-foreground/60" /> : null}
          </li>
        ))}
      </ol>
      <Popover>
        <PopoverTrigger asChild>
          <Button size="icon-xs" variant="ghost" aria-label="判定顺序说明" className="text-muted-foreground">
            <Info />
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-80 text-xs leading-5">
          <p className="font-medium text-foreground">判定顺序</p>
          <ol className="mt-1.5 flex flex-col gap-1 text-muted-foreground">
            {LAYERS.map((layer, index) => (
              <li key={layer.key}>
                <span className="font-medium text-foreground">
                  {index + 1}. {layer.label}
                </span>
                ：{layer.hint}
              </li>
            ))}
          </ol>
          <ul className="mt-2 flex list-disc flex-col gap-1 border-t pt-2 pl-4 text-muted-foreground">
            <li>先命中的生效，后面的层不再判定。</li>
            <li>封禁排在可信网段之前，可信网段也挡不住封禁；关闭整个防火墙后封禁不再生效。</li>
            <li>自动封禁只统计真实拦截，观察模式下的命中不计数。</li>
          </ul>
        </PopoverContent>
      </Popover>
    </div>
  );
}

/** 页头的 IP 查询入口：回车打开查询窗 */
function InspectSearch({ onSubmit }: { onSubmit: (ip: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <form
      className="relative min-w-0 flex-1 @2xl:w-64 @2xl:flex-none"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit(value.trim());
      }}
    >
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={value}
        onChange={(event) => setValue(event.target.value)}
        placeholder="查询 IP 会不会被拦截"
        spellCheck={false}
        className="h-9 pl-8 font-mono text-sm placeholder:font-sans"
        aria-label="查询 IP"
      />
    </form>
  );
}
