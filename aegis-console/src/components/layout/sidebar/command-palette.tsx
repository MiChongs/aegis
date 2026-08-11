"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Fragment, useEffect, useMemo, useState } from "react";
import { useTheme } from "next-themes";
import {
  BookOpen,
  Clock,
  Eraser,
  LogOut,
  Monitor,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  ShieldCheck,
  Star,
  StarOff,
  Sun,
  UserRound
} from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator
} from "@/components/ui/command";
import { currentTargetKey } from "@/lib/navigation";
import { resolveTargets, useTargetIndex, useVisibleTargets } from "@/lib/navigation-hooks";
import { pinyinMatchIndices, preloadPinyin, usePinyinReady } from "@/lib/pinyin-search";
import { useSidebarStore } from "@/lib/sidebar-store";
import { Kbd, useMetaKeyLabel } from "@/components/layout/sidebar/sidebar-shared";

type EntryIcon = React.ComponentType<{ className?: string }>;

type SectionKey = "recent" | "pinned" | "page" | "panel" | "action";

type Entry = {
  /** cmdk 的 value，必须全局唯一 —— 同一个目标出现在「最近」和「页面」两处时靠前缀区分 */
  id: string;
  section: SectionKey;
  title: string;
  /** 右侧灰字：所属页面或分组 */
  hint?: string;
  icon: EntryIcon;
  /** 纯文本匹配的候选串（标题、所属页面、分组、路径） */
  keywords: string[];
  /** 标题本身的拼音候选。命中它才是"我要找的就是这一项" */
  cjk: string;
  /**
   * 所属页面 / 分组的拼音候选，命中只算旁证。
   *
   * 分开打分是必要的：输 `yh` 时「用户」和「角色」都会中（后者靠分组名"用户与权限"），
   * 同分的话排序就退化成目录顺序，标题正命中的那一项反而排在后面。
   */
  cjkAlt: string[];
  run: () => void;
};

const SECTIONS: { key: SectionKey; heading: string }[] = [
  { key: "recent", heading: "最近访问" },
  { key: "pinned", heading: "收藏" },
  { key: "page", heading: "页面" },
  { key: "panel", heading: "页内面板" },
  { key: "action", heading: "操作" }
];

/** 无搜索词时，页内面板（130+ 条）不铺开 —— 那会把真正常用的页面挤出视野 */
const PANEL_LIMIT_WHEN_SEARCHING = 14;
const RECENT_LIMIT = 5;

/**
 * 拼音命中的档位分。
 *
 * `base` 拉开「标题命中」与「靠所属页面/分组命中」的差距，
 * 再用两个小加成把同档内排序做实：从第一个字起算（anchored）、字与字相邻（contiguous）。
 * 加成合计 0.09，压在纯文本 `includes` 的 0.6 之下 —— 拼音永远不该越过字面匹配。
 */
function pinyinRank(text: string, query: string, base: number): number {
  const hit = pinyinMatchIndices(text, query);
  if (!hit) return 0;
  const anchored = hit[0] === 0;
  const contiguous = hit.every((value, i) => i === 0 || value === hit[i - 1] + 1);
  return base + (anchored ? 0.06 : 0) + (contiguous ? 0.03 : 0);
}

function score(entry: Entry, query: string, pinyinReady: boolean): number {
  if (!query) return 1;
  let best = 0;
  for (const keyword of entry.keywords) {
    const text = keyword.toLowerCase();
    if (text === query) return 1;
    if (text.startsWith(query)) best = Math.max(best, 0.85);
    else if (text.includes(query)) best = Math.max(best, 0.6);
  }
  // 纯文本匹配不中时才走拼音，且只对纯字母查询（"yhgl" / "yonghu"）尝试
  if (best === 0 && pinyinReady && /^[a-z0-9]+$/.test(query)) {
    const primary = pinyinRank(entry.cjk, query, 0.5);
    if (primary) return primary;
    for (const text of entry.cjkAlt) {
      const alt = pinyinRank(text, query, 0.35);
      if (alt) return alt;
    }
  }
  return best;
}

/**
 * 命令面板（`⌘K` / `Ctrl K`）。
 *
 * 侧边栏解决的是「浏览」，它解决的是「我知道要去哪、别让我找」——
 * 130+ 个跳转目标里，靠肌肉记忆敲两三个字母永远比在六个分组里翻更快。
 *
 * 自带 `useSearchParams()`，因此调用方必须把它套在 `<Suspense>` 里
 * （否则 `next build` 会因静态渲染 bailout 报错）。fallback 给 `null` 即可：
 * 面板初始是关的，晚一帧挂载没有任何可见影响。
 */
export function CommandPalette({
  open,
  onOpenChange,
  onLogout
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onLogout: () => void;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const activeTab = useSearchParams().get("tab");
  const { setTheme } = useTheme();
  const [search, setSearch] = useState("");
  const pinyinReady = usePinyinReady();
  const metaKey = useMetaKeyLabel();

  const targets = useVisibleTargets();
  const targetIndex = useTargetIndex();
  const pinned = useSidebarStore((s) => s.pinned);
  const recents = useSidebarStore((s) => s.recents);
  const collapsed = useSidebarStore((s) => s.collapsed);
  const togglePin = useSidebarStore((s) => s.togglePin);
  const toggleCollapsed = useSidebarStore((s) => s.toggle);
  const clearRecents = useSidebarStore((s) => s.clearRecents);

  /** 当前所在的跳转目标，用于「收藏当前页」 */
  const currentKey = useMemo(() => currentTargetKey(pathname, activeTab), [pathname, activeTab]);

  // 打开即确保词典在路上。不能只依赖调用方预热或 Radix 的 onOpenChange ——
  // 后者只在 Radix 自己改状态（esc / 点外面）时触发，父组件把 open 置 true 时它不响。
  useEffect(() => {
    if (open) preloadPinyin();
  }, [open]);

  function close() {
    onOpenChange(false);
    setSearch("");
  }

  function go(href: string) {
    close();
    router.push(href);
  }

  const entries = useMemo<Entry[]>(() => {
    const list: Entry[] = [];

    const navEntry = (
      section: SectionKey,
      target: { key: string; title: string; itemTitle: string; groupTitle: string; icon: EntryIcon; isChild: boolean }
    ): Entry => ({
      id: `${section}:${target.key}`,
      section,
      title: target.title,
      hint: target.isChild ? target.itemTitle : target.groupTitle,
      icon: target.icon,
      keywords: [target.title, target.itemTitle, target.groupTitle, target.key],
      cjk: target.title,
      cjkAlt: [target.itemTitle, target.groupTitle],
      run: () => go(target.key)
    });

    // 当前页不进「最近访问」：列一条"回到你已经在的地方"只是占位置
    const recentKeys = recents.filter((key) => key !== currentKey).slice(0, RECENT_LIMIT);
    for (const target of resolveTargets(recentKeys, targetIndex)) {
      list.push({ ...navEntry("recent", target), icon: Clock });
    }
    for (const target of resolveTargets(pinned, targetIndex)) {
      list.push({ ...navEntry("pinned", target), icon: Star });
    }
    for (const target of targets) {
      list.push(navEntry(target.isChild ? "panel" : "page", target));
    }

    const action = (
      id: string,
      title: string,
      icon: EntryIcon,
      run: () => void,
      hint?: string,
      extraKeywords: string[] = []
    ): Entry => ({
      id: `action:${id}`,
      section: "action",
      title,
      hint,
      icon,
      keywords: [title, ...extraKeywords],
      cjk: title,
      cjkAlt: [],
      run
    });

    list.push(
      action("theme-system", "主题：跟随系统", Monitor, () => { setTheme("system"); close(); }, "外观", ["theme", "system"]),
      action("theme-light", "主题：浅色", Sun, () => { setTheme("light"); close(); }, "外观", ["theme", "light"]),
      action("theme-dark", "主题：深色", Moon, () => { setTheme("dark"); close(); }, "外观", ["theme", "dark"]),
      action(
        "toggle-sidebar",
        collapsed ? "展开侧边栏" : "收起侧边栏",
        collapsed ? PanelLeftOpen : PanelLeftClose,
        () => { toggleCollapsed(); close(); },
        `${metaKey} B`,
        ["sidebar", "collapse"]
      )
    );

    if (currentKey) {
      const isPinned = pinned.includes(currentKey);
      const currentTitle = targetIndex.get(currentKey)?.title ?? "当前页";
      list.push(
        action(
          "toggle-pin",
          isPinned ? `取消收藏「${currentTitle}」` : `收藏「${currentTitle}」`,
          isPinned ? StarOff : Star,
          () => { togglePin(currentKey); close(); },
          "收藏",
          ["pin", "favorite", "收藏"]
        )
      );
    }
    if (recents.length) {
      list.push(action("clear-recents", "清空最近访问", Eraser, () => { clearRecents(); close(); }, "最近访问", ["clear"]));
    }

    list.push(
      action("profile", "个人资料", UserRound, () => go("/profile"), "账户"),
      action("account-security", "账户安全", ShieldCheck, () => go("/security?tab=account"), "账户"),
      action("developer-portal", "开发者门户", BookOpen, () => go("/developers"), "文档", ["docs", "api"]),
      action("logout", "退出登录", LogOut, () => { close(); onLogout(); }, "账户", ["logout", "signout"])
    );

    return list;
    // go / close 只依赖 router 与 onOpenChange，随组件生命周期稳定，不参与依赖计算
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [targets, targetIndex, pinned, recents, collapsed, currentKey, metaKey, setTheme, togglePin, toggleCollapsed, clearRecents, onLogout]);

  /**
   * 自己算过滤而不是用 cmdk 内置的 `filter`：
   * 拼音词典是异步到位的，词典就绪那一刻必须让已经输入的关键词重新过一遍 ——
   * 内置过滤只在 search 变化时重算，会把「刚打完字、词典才加载完」这一秒钉在空结果上。
   */
  const grouped = useMemo(() => {
    const query = search.trim().toLowerCase();
    const scored = entries
      .map((entry) => ({ entry, value: score(entry, query, pinyinReady) }))
      .filter((row) => row.value > 0);

    return SECTIONS.map(({ key, heading }) => {
      let rows = scored.filter((row) => row.entry.section === key);
      // 无搜索词时不铺开页内面板：130+ 条会把常用页面挤出视野
      if (key === "panel" && !query) rows = [];
      rows.sort((a, b) => b.value - a.value);
      if (key === "panel" && query) rows = rows.slice(0, PANEL_LIMIT_WHEN_SEARCHING);
      return { key, heading, entries: rows.map((row) => row.entry) };
    }).filter((section) => section.entries.length > 0);
  }, [entries, search, pinyinReady]);

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) setSearch("");
      }}
      className="max-w-xl"
      title="命令面板"
      description="搜索页面、页内面板与常用操作"
      // 过滤在上面自己算，cmdk 只负责键盘导航与滚动跟随
      shouldFilter={false}
    >
      <CommandInput
        value={search}
        onValueChange={setSearch}
        placeholder="搜索页面、面板或操作…（支持拼音首字母，如 yhgl）"
      />
      <CommandList className="max-h-[min(70vh,420px)]">
        <CommandEmpty>
          <span className="text-muted-foreground">没有匹配的结果</span>
        </CommandEmpty>
        {grouped.map((section, index) => (
          <Fragment key={section.key}>
            {index > 0 ? <CommandSeparator /> : null}
            <CommandGroup heading={section.heading}>
              {section.entries.map((entry) => {
                const Icon = entry.icon;
                return (
                  <CommandItem key={entry.id} value={entry.id} onSelect={entry.run}>
                    <Icon className="size-4" />
                    <span className="truncate">{entry.title}</span>
                    {entry.hint ? (
                      <span className="ml-auto shrink-0 pl-3 text-[11px] text-muted-foreground">{entry.hint}</span>
                    ) : null}
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </Fragment>
        ))}
      </CommandList>
      <div className="flex items-center gap-3 border-t px-3 py-2 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1">
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd> 选择
        </span>
        <span className="flex items-center gap-1">
          <Kbd>↵</Kbd> 打开
        </span>
        <span className="flex items-center gap-1">
          <Kbd>esc</Kbd> 关闭
        </span>
        <span className="ml-auto flex items-center gap-1">
          <Kbd>{metaKey}</Kbd>
          <Kbd>K</Kbd>
        </span>
      </div>
    </CommandDialog>
  );
}
