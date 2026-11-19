"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import NumberFlow from "@number-flow/react";
import { useHotkeys } from "react-hotkeys-hook";
import TextareaAutosize from "react-textarea-autosize";
import {
  AlertTriangle,
  ArrowRight,
  Ban,
  Check,
  ChevronsUpDown,
  Clapperboard,
  Coins,
  Disc3,
  FileText,
  FlaskConical,
  Gift,
  Grid3x3,
  LayoutGrid,
  Loader2,
  Repeat,
  Save,
  Scale,
  ScrollText,
  ShieldCheck,
  Sigma,
  SlidersHorizontal,
  Ticket,
  Users,
  UsersRound,
  X
} from "lucide-react";
import { toast } from "sonner";
import {
  EditorSection,
  InsightPanel,
  IssueList,
  SectionNav,
  issueSection,
  type InsightStat,
  type SectionKey,
  type SectionMeta
} from "@/components/apps/lottery/editor/editor-chrome";
import {
  CharCount,
  ChoiceCard,
  DateTimePicker,
  Field,
  FIELD_SURFACE,
  Segmented,
  Stepper,
  TextField
} from "@/components/apps/lottery/editor/editor-fields";
import { KindGlyph } from "@/components/apps/lottery/editor/prize-visuals";
import {
  CONVERSION_RATES,
  LIMITS,
  OWNABLE_KINDS,
  blankPrize,
  nextPrizeKey,
  rewardOf,
  seedForm,
  swapPreview,
  toPayload,
  validateForm,
  weightedOf,
  type ActivityForm
} from "@/components/apps/lottery/lottery-activity-form";
import { LotteryPerksEditor } from "@/components/apps/lottery/lottery-perks-editor";
import { LotteryPrizeBoard } from "@/components/apps/lottery/lottery-prize-board";
import { LotteryStrategyEditor, strategyEnabledCount } from "@/components/apps/lottery/lottery-strategy-editor";
import { LotteryStrategySimulator } from "@/components/apps/lottery/lottery-strategy-simulator";
import {
  LotteryAnalysisDialog,
  LotteryComputeDialog,
  LotterySolverDialog
} from "@/components/apps/lottery/compute/lottery-compute-dialogs";
import {
  LIFETIME_POLICIES,
  LIFETIME_POLICY_META,
  LOTTERY_STATUSES,
  UI_MODE_LABEL,
  defaultPrizeName,
  lotteryStatusLabel,
  prizeProbabilities
} from "@/components/apps/lottery/lottery-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import type { LotteryAdminActivity, LotteryLifetimePolicy, LotteryStatus, LotteryUIMode } from "@/lib/api/lottery";
import { useLotteryCatalogQuery, useLotterySettingsQuery, useSaveLotteryActivityMutation } from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

const SECTIONS: SectionMeta[] = [
  { key: "basic", label: "基本信息", icon: FileText },
  { key: "draws", label: "抽奖次数", icon: Ticket },
  { key: "prizes", label: "奖盘", icon: LayoutGrid },
  { key: "pity", label: "保底", icon: ShieldCheck },
  { key: "strategy", label: "出奖策略", icon: SlidersHorizontal },
  { key: "perks", label: "人群特权", icon: UsersRound },
  { key: "owned", label: "已拥有的奖品", icon: Repeat },
  { key: "rules", label: "活动规则", icon: ScrollText }
];

const STATUS_VISUAL: Record<LotteryStatus, { dot: string; hint: string }> = {
  draft: { dot: "bg-zinc-400", hint: "未上线，App 不可见" },
  active: { dot: "bg-emerald-500", hint: "时间窗内开放抽奖" },
  paused: { dot: "bg-amber-500", hint: "暂停抽奖" },
  ended: { dot: "bg-zinc-600 dark:bg-zinc-300", hint: "活动已结束" }
};

const POLICY_ICON: Record<LotteryLifetimePolicy, React.ReactNode> = {
  points: <Coins />,
  draws: <Ticket />,
  exclude: <Ban />,
  grant: <Gift />
};

const pad = (value: number) => String(value).padStart(2, "0");
const toLocal = (date: Date) =>
  `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;

/** 两个 datetime-local 值之间的时长，「12 天 6 小时」 */
function spanLabel(start: string, end: string) {
  if (!start || !end) return "";
  const minutes = Math.round((new Date(end).getTime() - new Date(start).getTime()) / 60000);
  if (!Number.isFinite(minutes) || minutes <= 0) return "";
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  if (days > 0) return hours > 0 ? `${days} 天 ${hours} 小时` : `${days} 天`;
  return hours > 0 ? `${hours} 小时` : `${minutes} 分钟`;
}

/**
 * 新建 / 编辑一个活动（连同全部奖品，一次保存）。
 *
 * 奖品与活动是一个整体：九宫格恰好 8 格、保底奖必须是本活动的奖品、至少一个不限库存的奖品，
 * 都是「整张奖盘」的约束，后端只接受整体保存。已有奖品带着 id 回传才能保留已抽中数量。
 * 草稿形状与积分商城编辑器相同：抽屉关闭即清空，不用 effect 同步；有改动时关闭先确认。
 *
 * 布局是三栏：左边分区导航（跟随滚动、显示各分区待处理数），中间分区卡，右边实时概览
 * （中奖率环形图、每人每日上限、待处理问题）。窄屏时两侧栏收起，问题列表挪到底栏。
 */
export function LotteryActivityEditor({
  appKey,
  activity,
  open,
  onOpenChange
}: {
  appKey: string;
  /** 为空即新建 */
  activity: LotteryAdminActivity | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const settingsQuery = useLotterySettingsQuery(appKey);
  const saveMutation = useSaveLotteryActivityMutation(appKey);
  const catalog = catalogQuery.data;

  const [draft, setDraft] = useState<{ id: number; value: ActivityForm } | null>(null);
  const [selectedKey, setSelectedKey] = useState<number | null>(null);
  const [simulating, setSimulating] = useState(false);
  // 计算引擎的三个弹窗：精确分析、配平（反解权重）、推演
  const [tool, setTool] = useState<"analysis" | "solver" | "compute" | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const [activeSection, setActiveSection] = useState<SectionKey>("basic");
  const scrollRef = useRef<HTMLDivElement>(null);

  const scope = activity?.id ?? 0;
  const dirty = draft?.id === scope;
  const form = dirty ? draft.value : seedForm(activity, catalog);
  const update = (next: Partial<ActivityForm>) => setDraft({ id: scope, value: { ...form, ...next } });
  const patch = <K extends keyof ActivityForm>(key: K, value: ActivityForm[K]) => update({ [key]: value } as Partial<ActivityForm>);

  const kinds = catalog?.kinds ?? [];
  const plans = catalog?.plans ?? [];
  const features = catalog?.features ?? [];
  const scenes = catalog?.videoScenes ?? [];
  const gridCount = catalog?.gridPrizeCount ?? 8;
  const wheelMax = catalog?.wheelMaxPrizes ?? 12;

  const probabilities = useMemo(() => prizeProbabilities(form.prizes.map(weightedOf)), [form.prizes]);
  const issues = useMemo(() => validateForm(form, catalog), [form, catalog]);
  const issueCount = (key: SectionKey) => issues.filter((issue) => issueSection(issue) === key).length;

  const reset = () => {
    setDraft(null);
    setSelectedKey(null);
    setActiveSection("basic");
  };
  const close = () => {
    reset();
    onOpenChange(false);
  };
  const requestClose = () => (dirty ? setConfirmDiscard(true) : close());

  const jump = useCallback((key: SectionKey) => {
    const root = scrollRef.current;
    const target = root?.querySelector<HTMLElement>(`[data-section="${key}"]`);
    if (!root || !target) return;
    setActiveSection(key);
    root.scrollTo({ top: target.offsetTop - 16, behavior: "smooth" });
  }, []);

  /** 当前分区：顶部越过阅读线的最后一个分区；滚到底时取最后一个 */
  const onScroll = () => {
    const root = scrollRef.current;
    if (!root) return;
    let current: SectionKey = SECTIONS[0].key;
    for (const section of SECTIONS) {
      const node = root.querySelector<HTMLElement>(`[data-section="${section.key}"]`);
      if (node && node.offsetTop - root.scrollTop <= 120) current = section.key;
    }
    if (root.scrollTop + root.clientHeight >= root.scrollHeight - 4) current = SECTIONS[SECTIONS.length - 1].key;
    if (current !== activeSection) setActiveSection(current);
  };

  /** 转盘切到九宫格：多于 8 个不替人删，少于 8 个补「谢谢参与」（权重 0，不改变现有概率） */
  const changeMode = (uiMode: LotteryUIMode) => {
    if (uiMode === form.uiMode) return;
    if (uiMode === "grid" && form.prizes.length > gridCount) {
      toast.error(`九宫格需要恰好 ${gridCount} 个奖品，请先删到 ${gridCount} 个`);
      return;
    }
    const prizes = [...form.prizes];
    while (uiMode === "grid" && prizes.length < gridCount) {
      prizes.push(blankPrize(nextPrizeKey(prizes), "none", 0, 0));
    }
    update({ uiMode, prizes });
  };

  const prizeName = (key: number) => {
    const prize = form.prizes.find((item) => item.key === key);
    if (!prize) return "";
    return prize.name.trim() || defaultPrizeName(rewardOf(prize, kinds), kinds, plans, features);
  };
  const pityCandidates = form.prizes.map((prize, index) => ({ prize, index })).filter(({ prize }) => prize.type !== "none");
  const pityPrize = form.prizes.find((prize) => prize.key === form.pityKey);
  const pityThreshold = Number(form.pityThreshold) || 0;

  const policies = catalog?.lifetimePolicies?.length
    ? catalog.lifetimePolicies
    : LIFETIME_POLICIES.map((key) => ({ key, ...LIFETIME_POLICY_META[key] }));
  const ownableKinds = catalog?.ownableKinds ?? OWNABLE_KINDS;
  const ownablePrizes = form.prizes.filter((prize) => ownableKinds.includes(prize.type));
  const activePerks = form.perks.filter((perk) => !perk.disabled).length;
  const showPoints = form.lifetimePolicy === "points" || form.lifetimePolicy === "exclude";
  const rates = catalog?.conversionRates ?? CONVERSION_RATES;

  const currentScene = scenes.find((scene) => scene.key === form.videoScene);
  const sceneHint = !catalog
    ? undefined
    : !catalog.rewardedAds
      ? "激励广告未开启"
      : scenes.length === 0
        ? "暂无发放抽奖次数的激励广告场景"
        : form.videoScene && !currentScene
          ? "该场景已不再发放抽奖次数"
          : currentScene && !currentScene.enabled
            ? "该场景已停用"
            : undefined;

  const pointsOpen = Number(form.pointsCost) > 0;
  /** 每人每天最多能抽几次：免费 + 视频（场景日上限 × 每次发放）+ 积分日上限；任一路不限即不限 */
  const dailyCap = (() => {
    let total = Number(form.freeDaily) || 0;
    if (currentScene) {
      if (currentScene.dailyLimit <= 0) return null;
      total += currentScene.dailyLimit * currentScene.drawsPerView;
    }
    if (pointsOpen) {
      const limit = Number(form.pointsDaily) || 0;
      if (limit <= 0) return null;
      total += limit;
    }
    return total;
  })();
  const limitedCount = form.prizes.filter((prize) => !prize.unlimited).length;
  const strategyCount = strategyEnabledCount(form.strategy);

  const stats: InsightStat[] = [
    {
      label: "每人每日最多",
      value: dailyCap === null ? "不限" : <NumberFlow value={dailyCap} suffix=" 次" />
    },
    { label: "奖品", value: `${form.prizes.length} 个`, hint: limitedCount ? `${limitedCount} 个限量` : "全部不限库存" },
    {
      label: "保底",
      value: form.pity && pityPrize ? `${pityThreshold} 抽` : "未开启",
      hint: form.pity && pityPrize ? prizeName(pityPrize.key) : undefined
    },
    { label: "出奖策略", value: strategyCount ? `${strategyCount} 项` : "按权重", hint: strategyCount ? "已启用" : "未启用策略" },
    { label: "人群特权", value: activePerks ? `${activePerks} 条` : "未设置", hint: activePerks ? "按人群改写次数与概率" : undefined }
  ];

  const submit = async () => {
    if (saveMutation.isPending) return;
    if (issues.length > 0) {
      toast.error(issues[0].message);
      jump(issueSection(issues[0]));
      return;
    }
    try {
      await saveMutation.mutateAsync({ activityId: activity?.id, payload: toPayload(form, kinds, catalog) });
      toast.success(activity ? "活动已保存" : "活动已创建");
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const simulate = () => {
    if (issues.length > 0) {
      toast.error(issues[0].message);
      jump(issueSection(issues[0]));
      return;
    }
    setSimulating(true);
  };

  /** 计算都按整张奖盘算：奖盘还有待修正的问题时先指过去 */
  const openTool = (next: "analysis" | "solver" | "compute") => {
    if (issues.length > 0) {
      toast.error(issues[0].message);
      jump(issueSection(issues[0]));
      return;
    }
    setTool(next);
  };

  /** 配平解出来的权重按位置填回奖盘（位置即奖品在草稿里的下标） */
  const applyWeights = (weights: Map<number, number>) => {
    update({ prizes: form.prizes.map((prize, index) => (weights.has(index) ? { ...prize, weight: String(weights.get(index)) } : prize)) });
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      void submit();
    },
    { enabled: open && !simulating && !tool, enableOnFormTags: true, enableOnContentEditable: true }
  );

  const setRange = (days: number | null) => {
    if (days === null) {
      update({ startTime: "", endTime: "" });
      return;
    }
    const start = form.startTime ? new Date(form.startTime) : new Date();
    if (!form.startTime) start.setSeconds(0, 0);
    const end = new Date(start.getTime() + days * 86_400_000);
    update({ startTime: toLocal(start), endTime: toLocal(end) });
  };
  const timeInvalid = Boolean(form.startTime && form.endTime && new Date(form.endTime) <= new Date(form.startTime));
  const status = STATUS_VISUAL[form.status] ?? STATUS_VISUAL.draft;

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (next) onOpenChange(true);
        else requestClose();
      }}
    >
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-[min(100vw,84rem)] gap-0 bg-background p-0"
        onEscapeKeyDown={(event) => {
          if (dirty) {
            event.preventDefault();
            setConfirmDiscard(true);
          }
        }}
      >
        <TooltipProvider delayDuration={200}>
          {/* 顶栏 */}
          <header className="flex shrink-0 items-center gap-3 border-b border-border bg-card px-5 py-3.5">
            <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-foreground text-background">
              {form.uiMode === "grid" ? <Grid3x3 className="size-5" /> : <Disc3 className="size-5" />}
            </span>
            <div className="min-w-0 flex-1">
              <SheetTitle className="truncate text-base font-semibold tracking-tight">
                {form.name.trim() || (activity ? activity.name : "新建活动")}
              </SheetTitle>
              <SheetDescription asChild>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span>{activity ? "编辑活动" : "新建活动"}</span>
                  <span className="h-3 w-px bg-border" />
                  <span className="flex items-center gap-1.5">
                    <span className={cn("size-1.5 rounded-full", status.dot)} />
                    {lotteryStatusLabel(form.status)}
                  </span>
                  <span className="h-3 w-px bg-border" />
                  <span>{UI_MODE_LABEL[form.uiMode]}</span>
                  {dirty ? (
                    <>
                      <span className="h-3 w-px bg-border" />
                      <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
                        <span className="size-1.5 animate-pulse rounded-full bg-amber-500" />
                        未保存
                      </span>
                    </>
                  ) : null}
                </div>
              </SheetDescription>
            </div>
            <Button variant="ghost" size="icon-sm" className="rounded-xl" onClick={requestClose} aria-label="关闭">
              <X className="size-4" />
            </Button>
          </header>

          <div className="flex min-h-0 flex-1">
            {/* 分区导航 */}
            <aside className="hidden w-52 shrink-0 flex-col justify-between border-r border-border px-3 py-4 lg:flex">
              <SectionNav sections={SECTIONS} active={activeSection} issueCount={issueCount} onJump={jump} />
              <div className="space-y-1 px-2.5 text-[11px] leading-relaxed text-muted-foreground">
                <p>
                  <kbd className="rounded border border-border bg-card px-1 font-data text-[10px]">Ctrl</kbd>{" "}
                  <kbd className="rounded border border-border bg-card px-1 font-data text-[10px]">S</kbd> 保存
                </p>
                <p>奖品与活动一起保存</p>
              </div>
            </aside>

            {/* 分区内容 */}
            <div ref={scrollRef} onScroll={onScroll} className="relative min-w-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]">
              <div className="mx-auto max-w-3xl space-y-4 px-4 py-5 sm:px-6">
                <EditorSection id="basic" icon={FileText} title="基本信息" description="名称、状态与开放时间" issues={issueCount("basic")}>
                  <Field label="活动名称" htmlFor="lottery-name">
                    <TextField
                      id="lottery-name"
                      size="lg"
                      value={form.name}
                      maxLength={LIMITS.name}
                      placeholder="例如：每日幸运抽奖"
                      invalid={!form.name.trim() && dirty}
                      onChange={(value) => patch("name", value)}
                    />
                  </Field>
                  <Field label="简介" aside="显示在 App 活动页顶部">
                    <div className={cn(FIELD_SURFACE, "flex items-end gap-2 px-3 py-2")}>
                      <TextareaAutosize
                        value={form.description}
                        maxLength={LIMITS.description}
                        minRows={2}
                        maxRows={6}
                        placeholder="一句话介绍活动"
                        onChange={(event) => patch("description", event.target.value)}
                        className="min-w-0 flex-1 resize-none bg-transparent text-sm leading-relaxed outline-none placeholder:text-muted-foreground/70"
                      />
                      <CharCount length={Array.from(form.description).length} max={LIMITS.description} />
                    </div>
                  </Field>

                  <Field label="状态">
                    <div role="radiogroup" className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                      {(catalog?.statuses ?? LOTTERY_STATUSES).map((value) => {
                        const visual = STATUS_VISUAL[value] ?? STATUS_VISUAL.draft;
                        return (
                          <ChoiceCard
                            key={value}
                            active={form.status === value}
                            onSelect={() => patch("status", value)}
                            title={
                              <span className="flex items-center gap-1.5">
                                <span className={cn("size-2 rounded-full", visual.dot)} />
                                {lotteryStatusLabel(value)}
                              </span>
                            }
                            description={visual.hint}
                          />
                        );
                      })}
                    </div>
                  </Field>

                  <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_9rem]">
                    <Field label="开始时间">
                      <DateTimePicker
                        value={form.startTime}
                        emptyLabel="立即开始"
                        onChange={(value) => patch("startTime", value)}
                      />
                    </Field>
                    <Field label="结束时间" aside={spanLabel(form.startTime, form.endTime)}>
                      <DateTimePicker
                        value={form.endTime}
                        emptyLabel="长期有效"
                        defaultTime="23:59"
                        invalid={timeInvalid}
                        onChange={(value) => patch("endTime", value)}
                      />
                    </Field>
                    <Field label="排序" aside="越小越前">
                      <Stepper value={form.sortOrder} onChange={(value) => patch("sortOrder", value)} />
                    </Field>
                  </div>
                  <div className="-mt-2 flex flex-wrap gap-1.5">
                    {[
                      { label: "7 天", days: 7 },
                      { label: "14 天", days: 14 },
                      { label: "30 天", days: 30 },
                      { label: "不限时间", days: null }
                    ].map((preset) => (
                      <button
                        key={preset.label}
                        type="button"
                        onClick={() => setRange(preset.days)}
                        className="h-7 rounded-full border border-border px-3 text-[11px] text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
                      >
                        {preset.days === null ? preset.label : `${form.startTime ? "开始后" : "从现在起"} ${preset.label}`}
                      </button>
                    ))}
                  </div>
                </EditorSection>

                <EditorSection
                  id="draws"
                  icon={Ticket}
                  title="抽奖次数"
                  description="每天能抽几次、从哪里获得次数"
                  issues={issueCount("draws")}
                  aside={
                    <span className="rounded-full bg-muted px-2.5 py-1 text-[11px] text-muted-foreground">
                      每人每日最多 <span className="font-data font-semibold tabular-nums text-foreground">{dailyCap === null ? "不限" : `${dailyCap} 次`}</span>
                    </span>
                  }
                >
                  <div className="grid gap-3 md:grid-cols-3">
                    <SourceCard icon={<Gift />} tone="bg-emerald-500/12 text-emerald-600 dark:text-emerald-400" title="每日免费" hint="每天零点重置">
                      <Stepper value={form.freeDaily} min={0} max={LIMITS.freeDaily} suffix="次" onChange={(value) => patch("freeDaily", value)} />
                    </SourceCard>

                    <SourceCard
                      icon={<Coins />}
                      tone="bg-amber-500/12 text-amber-600 dark:text-amber-400"
                      title="积分抽奖"
                      hint={pointsOpen ? "消耗积分换一次" : "未开放"}
                      off={!pointsOpen}
                      aside={
                        <Switch
                          checked={pointsOpen}
                          onCheckedChange={(checked) => patch("pointsCost", checked ? "10" : "0")}
                          aria-label="开放积分抽奖"
                        />
                      }
                    >
                      {pointsOpen ? (
                        <div className="space-y-2">
                          <Stepper value={form.pointsCost} min={1} max={LIMITS.pointsCost} suffix="积分/次" onChange={(value) => patch("pointsCost", value)} />
                          <Stepper
                            value={form.pointsDaily}
                            min={0}
                            max={LIMITS.pointsDaily}
                            suffix={Number(form.pointsDaily) > 0 ? "次/日" : "不限"}
                            onChange={(value) => patch("pointsDaily", value)}
                          />
                        </div>
                      ) : (
                        <p className="text-[11px] text-muted-foreground">打开后设置每次消耗与每日上限</p>
                      )}
                    </SourceCard>

                    <SourceCard
                      icon={<Clapperboard />}
                      tone="bg-violet-500/12 text-violet-600 dark:text-violet-400"
                      title="视频抽奖"
                      hint={currentScene ? `看一次得 ${currentScene.drawsPerView} 次` : "看激励视频获得次数"}
                      off={!form.videoScene}
                    >
                      <ScenePicker
                        value={form.videoScene}
                        scenes={scenes}
                        onChange={(value) => patch("videoScene", value)}
                      />
                      {sceneHint ? (
                        <p className="flex items-center gap-1 text-[11px] text-amber-600 dark:text-amber-400">
                          <AlertTriangle className="size-3 shrink-0" />
                          {sceneHint}
                        </p>
                      ) : currentScene ? (
                        <p className="text-[11px] text-muted-foreground">
                          {currentScene.dailyLimit > 0 ? `每日最多看 ${currentScene.dailyLimit} 次` : "每日不限次数"}
                        </p>
                      ) : null}
                    </SourceCard>
                  </div>
                </EditorSection>

                <EditorSection
                  id="prizes"
                  icon={LayoutGrid}
                  title="奖盘"
                  description={form.uiMode === "grid" ? `九宫格需要恰好 ${gridCount} 个奖品` : `转盘可放 ${catalog?.wheelMinPrizes ?? 2}–${wheelMax} 个奖品`}
                  issues={issueCount("prizes")}
                  aside={
                    <Segmented
                      value={form.uiMode}
                      onChange={changeMode}
                      options={(catalog?.uiModes ?? (["grid", "wheel"] as LotteryUIMode[])).map((mode) => ({
                        value: mode,
                        label: UI_MODE_LABEL[mode],
                        icon: mode === "grid" ? <Grid3x3 /> : <Disc3 />
                      }))}
                    />
                  }
                >
                  <LotteryPrizeBoard
                    uiMode={form.uiMode}
                    prizes={form.prizes}
                    probabilities={probabilities}
                    selectedKey={selectedKey}
                    pityKey={form.pity ? form.pityKey : null}
                    catalog={catalog}
                    swapPreview={(prize) => swapPreview(form, prize, catalog)}
                    onSelect={setSelectedKey}
                    onChange={(prizes) =>
                      update({
                        prizes,
                        pityKey: prizes.some((prize) => prize.key === form.pityKey && prize.type !== "none") ? form.pityKey : null
                      })
                    }
                  />
                </EditorSection>

                <EditorSection
                  id="pity"
                  icon={ShieldCheck}
                  title="保底"
                  description="连续未中到一定次数后必中指定奖品"
                  issues={issueCount("pity")}
                  aside={<Switch checked={form.pity} onCheckedChange={(value) => patch("pity", value)} aria-label="开启保底" />}
                >
                  {form.pity ? (
                    <>
                      <div className="grid gap-4 sm:grid-cols-[11rem_minmax(0,1fr)]">
                        <Field label="保底次数" aside={`2–${LIMITS.pity}`}>
                          <Stepper value={form.pityThreshold} min={2} max={LIMITS.pity} suffix="次" onChange={(value) => patch("pityThreshold", value)} />
                        </Field>
                        <Field label="进度示意" aside={pityThreshold > 24 ? `仅示意前 24 次` : undefined}>
                          <PityTrack threshold={pityThreshold} />
                        </Field>
                      </div>
                      <Field label="保底奖品">
                        {pityCandidates.length === 0 ? (
                          <p className="rounded-xl border border-dashed border-border px-3 py-3 text-xs text-muted-foreground">奖盘里还没有可作保底的奖品</p>
                        ) : (
                          <div role="radiogroup" className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                            {pityCandidates.map(({ prize, index }) => (
                              <ChoiceCard
                                key={prize.key}
                                active={prize.key === form.pityKey}
                                onSelect={() => patch("pityKey", prize.key)}
                                icon={<KindGlyph type={prize.type} />}
                                tone=""
                                activeTone=""
                                title={prizeName(prize.key)}
                                description={`第 ${index + 1} ${form.uiMode === "grid" ? "格" : "个"}${prize.unlimited ? "" : `，限 ${prize.quantity}`}`}
                                className="p-2.5"
                              />
                            ))}
                          </div>
                        )}
                      </Field>
                      {pityPrize && pityThreshold >= 2 ? (
                        <p className="rounded-xl bg-sky-500/[0.07] px-3 py-2.5 text-xs text-sky-800 dark:text-sky-300">
                          连续 {pityThreshold - 1} 次未中后，第 {pityThreshold} 次必中「{prizeName(pityPrize.key)}」
                          {pityPrize.unlimited ? "" : "，保底奖抽完后不再保底"}
                        </p>
                      ) : null}
                    </>
                  ) : null}
                </EditorSection>

                <EditorSection
                  id="strategy"
                  icon={SlidersHorizontal}
                  title="出奖策略"
                  description={strategyCount ? `已启用 ${strategyCount} 项，按策略调整每次的出奖分布` : "未启用，按权重随机"}
                  issues={issueCount("strategy")}
                >
                  <LotteryStrategyEditor
                    embedded
                    value={form.strategy}
                    onChange={(strategy) => patch("strategy", strategy)}
                    catalog={catalog}
                    prizeLabels={form.prizes.map((prize) => prizeName(prize.key))}
                    supremeTiers={settingsQuery.data?.tiers}
                  />
                </EditorSection>

                <EditorSection
                  id="perks"
                  icon={UsersRound}
                  title="人群特权"
                  description={
                    activePerks
                      ? `已启用 ${activePerks} 条，按人群与当天行为改写次数、价格与概率`
                      : "会员、永久会员、免广告用户、签到或在线时长达标的人得到额外次数、免看广告等"
                  }
                  issues={issueCount("perks")}
                >
                  <LotteryPerksEditor
                    value={form.perks}
                    onChange={(perks) => patch("perks", perks)}
                    catalog={catalog}
                    context={{
                      video: !!form.videoScene,
                      points: pointsOpen,
                      pointsDaily: Number(form.pointsDaily) > 0,
                      pity: form.pity
                    }}
                  />
                </EditorSection>

                <EditorSection
                  id="owned"
                  icon={Repeat}
                  title="已拥有的奖品"
                  description="永久会员抽中会员天数，或已永久解锁某个功能时的处理方式"
                  issues={issueCount("owned")}
                >
                  <div role="radiogroup" className="grid gap-2 sm:grid-cols-2">
                    {policies.map((policy) => (
                      <ChoiceCard
                        key={policy.key}
                        active={form.lifetimePolicy === policy.key}
                        onSelect={() => patch("lifetimePolicy", policy.key)}
                        icon={POLICY_ICON[policy.key] ?? <Gift />}
                        title={policy.label}
                        description={policy.hint}
                      />
                    ))}
                  </div>
                  {showPoints ? (
                    <div className="grid gap-4 sm:grid-cols-2">
                      <Field
                        label="每天折算积分"
                        aside="会员天数、限时功能、免广告"
                        hint={`填 0 跟随应用的会员折算策略（会员 ${rates.perVipDay}、限时功能 ${rates.perFeatureDay} 积分/天）`}
                      >
                        <Stepper
                          value={form.lifetimePointsPerDay}
                          min={0}
                          max={LIMITS.lifetimePointsPerDay}
                          suffix="积分/天"
                          onChange={(value) => patch("lifetimePointsPerDay", value)}
                        />
                      </Field>
                      <Field
                        label="永久奖品折算积分"
                        aside="永久套餐与永久功能"
                        hint={`填 0 跟随应用的会员折算策略（${rates.perLifetime} 积分）；功能时长始终按应用每小时 ${rates.perFeatureHour} 积分`}
                      >
                        <Stepper
                          value={form.lifetimePointsLifetime}
                          min={0}
                          max={LIMITS.lifetimePointsLifetime}
                          suffix="积分"
                          onChange={(value) => patch("lifetimePointsLifetime", value)}
                        />
                      </Field>
                    </div>
                  ) : null}
                  {form.lifetimePolicy === "draws" ? (
                    <Field label="改发次数" className="sm:max-w-[16rem]">
                      <Stepper value={form.lifetimeDraws} min={1} max={LIMITS.lifetimeDraws} suffix="次" onChange={(value) => patch("lifetimeDraws", value)} />
                    </Field>
                  ) : null}
                  {form.lifetimePolicy !== "grant" && ownablePrizes.length > 0 ? (
                    <div className="overflow-hidden rounded-xl border border-border">
                      <div className="border-b border-border bg-muted/40 px-3 py-2 text-[11px] font-medium text-muted-foreground">
                        本活动的会员、功能与免广告奖品
                      </div>
                      <ul className="divide-y divide-border">
                        {ownablePrizes.map((prize) => {
                          const target = swapPreview(form, prize, catalog);
                          return (
                            <li key={prize.key} className="flex items-center gap-2.5 px-3 py-2 text-xs">
                              <KindGlyph type={prize.type} size="sm" />
                              <span className="min-w-0 flex-1 truncate">{prizeName(prize.key)}</span>
                              <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
                              <span className="shrink-0 font-medium tabular-nums">{target ?? "照常发放"}</span>
                              {form.lifetimePolicy === "exclude" && Number(prize.weight) > 0 ? (
                                <span className="shrink-0 text-muted-foreground">仅保底时</span>
                              ) : null}
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  ) : null}
                </EditorSection>

                <EditorSection id="rules" icon={ScrollText} title="活动规则" description="展示在 App 的规则说明页" issues={issueCount("rules")}>
                  <div className={cn(FIELD_SURFACE, "px-3.5 py-3")}>
                    <TextareaAutosize
                      value={form.rules}
                      maxLength={LIMITS.rules}
                      minRows={5}
                      maxRows={18}
                      placeholder={"1. 每位用户每天可免费抽奖 1 次\n2. 奖品将在抽中后自动发放至账户"}
                      onChange={(event) => patch("rules", event.target.value)}
                      className="w-full resize-none bg-transparent text-sm leading-relaxed outline-none placeholder:text-muted-foreground/60"
                    />
                    <div className="flex justify-end">
                      <CharCount length={Array.from(form.rules).length} max={LIMITS.rules} />
                    </div>
                  </div>
                </EditorSection>
                <div className="h-8" />
              </div>
            </div>

            {/* 实时概览 */}
            <aside className="hidden w-80 shrink-0 overflow-y-auto border-l border-border p-4 xl:block">
              <InsightPanel prizes={form.prizes} probabilities={probabilities} stats={stats} issues={issues} onJump={jump} />
            </aside>
          </div>

          {/* 底栏 */}
          <footer className="flex shrink-0 items-center gap-2 border-t border-border bg-card px-5 py-3">
            <Button variant="outline" size="sm" className="rounded-xl" onClick={simulate} disabled={catalogQuery.isLoading}>
              <FlaskConical className="size-3.5" />
              模拟
            </Button>
            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openTool("analysis")} disabled={catalogQuery.isLoading}>
              <Sigma className="size-3.5" />
              分析
            </Button>
            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openTool("solver")} disabled={catalogQuery.isLoading}>
              <Scale className="size-3.5" />
              配平
            </Button>
            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openTool("compute")} disabled={catalogQuery.isLoading}>
              <Users className="size-3.5" />
              推演
            </Button>
            <Popover>
              <PopoverTrigger asChild>
                <button
                  type="button"
                  className={cn(
                    "flex h-8 items-center gap-1.5 rounded-xl px-2.5 text-xs transition-colors xl:hidden",
                    issues.length
                      ? "bg-amber-500/10 text-amber-700 hover:bg-amber-500/15 dark:text-amber-400"
                      : "text-emerald-700 dark:text-emerald-400"
                  )}
                >
                  {issues.length ? <AlertTriangle className="size-3.5" /> : <Check className="size-3.5" />}
                  {issues.length ? `${issues.length} 项待处理` : "检查通过"}
                </button>
              </PopoverTrigger>
              <PopoverContent align="start" side="top" className="w-80 rounded-2xl p-2">
                <IssueList issues={issues} onJump={jump} compact />
              </PopoverContent>
            </Popover>
            <div className="ml-auto flex items-center gap-2">
              <Button variant="ghost" size="sm" className="rounded-xl" onClick={requestClose}>
                取消
              </Button>
              <Button size="sm" className="min-w-24 rounded-xl" onClick={submit} disabled={saveMutation.isPending || catalogQuery.isLoading}>
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                {activity ? "保存" : "创建活动"}
              </Button>
            </div>
          </footer>
        </TooltipProvider>
      </SheetContent>

      <LotteryStrategySimulator
        appKey={appKey}
        activityId={activity?.id}
        payload={() => toPayload(form, kinds, catalog)}
        catalog={catalog}
        open={simulating}
        onOpenChange={setSimulating}
      />
      <LotteryAnalysisDialog
        appKey={appKey}
        activityId={activity?.id}
        payload={() => toPayload(form, kinds, catalog)}
        open={tool === "analysis"}
        onOpenChange={(next) => setTool(next ? "analysis" : null)}
      />
      <LotterySolverDialog
        appKey={appKey}
        activityId={activity?.id}
        payload={() => toPayload(form, kinds, catalog)}
        prizes={form.prizes.map((prize, index) => ({
          position: index,
          name: prizeName(prize.key),
          none: prize.type === "none",
          probability: probabilities[index] ?? 0,
          weight: weightedOf(prize).weight
        }))}
        hasFortune={Boolean(form.strategy.fortune)}
        open={tool === "solver"}
        onOpenChange={(next) => setTool(next ? "solver" : null)}
        onApply={applyWeights}
      />
      <LotteryComputeDialog
        appKey={appKey}
        activityId={activity?.id}
        payload={() => toPayload(form, kinds, catalog)}
        open={tool === "compute"}
        onOpenChange={(next) => setTool(next ? "compute" : null)}
      />

      <AlertDialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的更改？</AlertDialogTitle>
            <AlertDialogDescription>关闭后本次对活动与奖盘的修改都会丢失。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmDiscard(false);
                close();
              }}
            >
              放弃更改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Sheet>
  );
}

/** 次数来源卡：色调图标 + 标题，右上角可放开关；关闭态整体变淡 */
function SourceCard({
  icon,
  tone,
  title,
  hint,
  aside,
  off,
  children
}: {
  icon: React.ReactNode;
  tone: string;
  title: string;
  hint?: string;
  aside?: React.ReactNode;
  off?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("flex flex-col gap-3 rounded-2xl border border-border p-3.5 transition-colors", off ? "bg-muted/30" : "bg-card")}>
      <div className="flex items-center gap-2.5">
        <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg transition-opacity [&>svg]:size-4", tone, off && "opacity-50")}>
          {icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-semibold">{title}</div>
          {hint ? <div className="truncate text-[11px] text-muted-foreground">{hint}</div> : null}
        </div>
        {aside}
      </div>
      {children}
    </div>
  );
}

/** 视频抽奖场景选择：可搜索，列出每个场景的发放次数与日上限 */
function ScenePicker({
  value,
  scenes,
  onChange
}: {
  value: string;
  scenes: Array<{ key: string; name: string; enabled: boolean; drawsPerView: number; dailyLimit: number }>;
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const current = scenes.find((scene) => scene.key === value);
  const pick = (next: string) => {
    onChange(next);
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={cn(FIELD_SURFACE, "flex h-9 w-full items-center gap-2 px-3 text-left text-sm", open && "bg-background ring-ring/40")}>
          <span className={cn("min-w-0 flex-1 truncate", !value && "text-muted-foreground")}>
            {value ? current?.name || current?.key || value : "不开放"}
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 rounded-2xl p-0">
        <Command>
          <CommandInput placeholder="搜索激励广告场景" />
          <CommandList>
            <CommandEmpty>没有匹配的场景</CommandEmpty>
            <CommandGroup>
              <CommandItem value="不开放" onSelect={() => pick("")}>
                <span className="flex-1">不开放</span>
                {!value ? <Check className="size-3.5" /> : null}
              </CommandItem>
              {scenes.map((scene) => (
                <CommandItem key={scene.key} value={`${scene.name} ${scene.key}`} onSelect={() => pick(scene.key)}>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 truncate">
                      {scene.name || scene.key}
                      {!scene.enabled ? <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">已停用</span> : null}
                    </div>
                    <div className="text-[11px] text-muted-foreground">
                      看一次得 {scene.drawsPerView} 次{scene.dailyLimit > 0 ? `，每日 ${scene.dailyLimit} 次` : "，每日不限"}
                    </div>
                  </div>
                  {scene.key === value ? <Check className="size-3.5" /> : null}
                </CommandItem>
              ))}
              {value && !current ? (
                <CommandItem value={value} onSelect={() => pick(value)}>
                  <span className="flex-1 font-mono text-xs">{value}</span>
                  <Check className="size-3.5" />
                </CommandItem>
              ) : null}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/** 保底进度示意：前 N-1 格是未中，最后一格是必中 */
function PityTrack({ threshold }: { threshold: number }) {
  const shown = Math.min(Math.max(threshold, 0), 24);
  if (shown < 2) return <div className="flex h-9 items-center text-[11px] text-muted-foreground">至少 2 次</div>;
  return (
    <div className="flex h-9 items-center gap-1">
      {Array.from({ length: shown }, (_, index) => {
        const last = index === shown - 1 && shown === threshold;
        return (
          <span
            key={index}
            className={cn(
              "h-5 min-w-1 flex-1 rounded-[4px] transition-colors",
              last ? "grid place-items-center bg-sky-500 text-white" : "bg-muted dark:bg-muted/60"
            )}
          >
            {last ? <ShieldCheck className="size-3" /> : null}
          </span>
        );
      })}
    </div>
  );
}
