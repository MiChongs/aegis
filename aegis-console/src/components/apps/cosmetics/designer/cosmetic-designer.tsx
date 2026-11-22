"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import {
  AlertTriangle,
  ArrowLeft,
  CircleCheck,
  History,
  Layers,
  Loader2,
  Images as ImagesIcon,
  Redo2,
  Rocket,
  Save,
  SlidersHorizontal,
  Undo2,
  View
} from "lucide-react";
import { toast } from "sonner";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { errorMessage } from "@/components/users/detail/kit";
import type { CosmeticAsset, CosmeticCatalog, CosmeticItem, Design, Layer } from "@/lib/api/cosmetics";
import {
  useCosmeticAssetsQuery,
  useCosmeticItemQuery,
  usePublishCosmeticMutation,
  useRefreshCosmeticLists,
  useSaveCosmeticDraftMutation,
  useUploadCosmeticPreviewMutation
} from "@/lib/cosmetic-hooks";
import { formatClockSeconds } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { RarityBadge, StatusBadge, slotLabel } from "../cosmetic-shared";
import { renderPreviewPng } from "../renderer";
import {
  AVATAR_ID,
  createAssetLayer,
  designSignature,
  duplicateLayer,
  insertLayer,
  localIssues,
  patchLayer,
  removeKeyframe,
  removeLayer,
  shiftAnimated,
  withAsset
} from "./design-ops";
import { AssetBrowser, AssetPickerDialog } from "./designer-assets";
import { PublishDialog, RevisionsSheet, type PublishStep } from "./designer-dialogs";
import { DesignerInspector } from "./designer-inspector";
import { DesignerLayers } from "./designer-layers";
import { DesignerPreview, NameStage } from "./designer-preview";
import { DesignerStage } from "./designer-stage";
import { DesignerContext, createDesignerStore, designTime, useDesigner, useDesignerApi } from "./designer-store";
import { DesignerTimeline } from "./designer-timeline";

/**
 * 装扮设计器（全屏弹层）：打开单件装扮（GET items/:id 拿草稿与已发布的设计稿），
 * 左边图层与素材、中间画布、右边属性 / 预览 / 问题、下面时间轴。
 *
 * 快捷键：⌘Z 撤销、⌘⇧Z 重做、⌘S 保存、⌘C / ⌘V 复制粘贴图层、⌘D 复制一份、方向键微调（Shift 10 像素）、
 * Delete 删除图层或选中的关键帧、空格播放 / 暂停、Esc 取消选中。
 *
 * 保存走 PUT draft（后端整份校验，错误原样显示在顶栏下）；发布前先保存，再用离屏 canvas 渲染当前帧上传预览图，最后 publish。
 */

/** 图层剪贴板：进程内共享，可以在两件装扮之间复制粘贴（连同引用的素材） */
let layerClipboard: { layer: Layer; assets: Design["assets"] } | null = null;

const AUTOSAVE_KEY = "aegis.cosmetic.autosave";

function blankDesign(item: CosmeticItem, catalog?: CosmeticCatalog | null): Design {
  const spec = catalog?.slots.find((slot) => slot.key === item.slot);
  if (item.slot === "name_style") {
    return { version: 1, canvas: { width: 0, height: 0 }, duration: 0, layers: [], nameStyle: { color: "#3B82F6", effect: "none", speed: 2400, fontWeight: 600 } };
  }
  return {
    version: 1,
    canvas: spec?.canvas ?? { width: 300, height: 300 },
    avatar: item.slot === "avatar_frame" ? (spec?.avatar ?? { x: 50, y: 50, size: 200, shape: "circle" }) : undefined,
    duration: 0,
    layers: []
  };
}

function initialDesign(item: CosmeticItem, catalog?: CosmeticCatalog | null): Design {
  const source = item.draft ?? item.design ?? blankDesign(item, catalog);
  const copy = JSON.parse(JSON.stringify(source)) as Design;
  copy.layers = copy.layers ?? [];
  copy.version = copy.version || 1;
  if (item.slot === "avatar_frame" && !copy.avatar) copy.avatar = blankDesign(item, catalog).avatar;
  if (item.slot === "name_style" && !copy.nameStyle) copy.nameStyle = blankDesign(item, catalog).nameStyle;
  return copy;
}

export function CosmeticDesigner({
  appKey,
  itemId,
  catalog,
  can,
  onClose
}: {
  appKey: string;
  itemId: number | null;
  catalog?: CosmeticCatalog | null;
  can: { write: boolean; delete: boolean; publish: boolean };
  onClose: () => void;
}) {
  const open = itemId !== null;
  const itemQuery = useCosmeticItemQuery(appKey, itemId, open);
  const refresh = useRefreshCosmeticLists();
  const dirtyRef = useRef(false);
  const [confirmClose, setConfirmClose] = useState(false);
  // 打开时取一次快照作为设计器的起点：之后单件缓存因保存、发布而刷新，不能把正在编辑的画面冲掉
  const [seed, setSeed] = useState<{ id: number; item: CosmeticItem } | null>(null);
  const loaded = itemQuery.data && itemQuery.data.id === itemId ? itemQuery.data : null;
  if (loaded && (!seed || seed.id !== loaded.id) && catalog) {
    setSeed({ id: loaded.id, item: loaded });
  }
  if (!open && seed) setSeed(null);

  const close = () => {
    dirtyRef.current = false;
    setConfirmClose(false);
    onClose();
    void refresh();
  };
  const requestClose = () => {
    if (dirtyRef.current) setConfirmClose(true);
    else close();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : requestClose())}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 sm:max-w-none"
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogTitle className="sr-only">装扮设计器</DialogTitle>
        <DialogDescription className="sr-only">编辑图层、关键帧与素材，保存草稿并发布</DialogDescription>
        <TooltipProvider delayDuration={300}>
          {seed && catalog ? (
            <DesignerWorkspace
              key={seed.id}
              appKey={appKey}
              item={seed.item}
              catalog={catalog}
              can={can}
              onClose={requestClose}
              onDirtyChange={(dirty) => {
                dirtyRef.current = dirty;
              }}
            />
          ) : (
            <div className="flex h-full flex-col">
              <div className="flex h-12 items-center gap-3 border-b px-3">
                <Button size="icon-sm" variant="ghost" onClick={close} aria-label="返回">
                  <ArrowLeft />
                </Button>
                <Skeleton className="h-5 w-48" />
              </div>
              {itemQuery.isError ? (
                <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{errorMessage(itemQuery.error, "加载失败")}</div>
              ) : (
                <div className="grid flex-1 grid-cols-[260px_minmax(0,1fr)_300px] gap-3 p-3">
                  <Skeleton className="rounded-xl" />
                  <Skeleton className="rounded-xl" />
                  <Skeleton className="rounded-xl" />
                </div>
              )}
            </div>
          )}
        </TooltipProvider>
      </DialogContent>
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>草稿还有没保存的改动，关闭后会丢失。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" onClick={close}>
              放弃修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}

function DesignerWorkspace(props: {
  appKey: string;
  item: CosmeticItem;
  catalog: CosmeticCatalog;
  can: { write: boolean; delete: boolean; publish: boolean };
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const [store] = useState(() => createDesignerStore(props.item.slot, initialDesign(props.item, props.catalog)));
  return (
    <DesignerContext.Provider value={store}>
      <WorkspaceInner {...props} />
    </DesignerContext.Provider>
  );
}

function WorkspaceInner({
  appKey,
  item: initialItem,
  catalog,
  can,
  onClose,
  onDirtyChange
}: {
  appKey: string;
  item: CosmeticItem;
  catalog: CosmeticCatalog;
  can: { write: boolean; delete: boolean; publish: boolean };
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const api = useDesignerApi();
  const slot = initialItem.slot;
  const readOnly = !can.write;
  const [item, setItem] = useState(initialItem);
  const design = useDesigner((s) => s.design);
  const dirty = useDesigner((s) => designSignature(s.design) !== s.savedSignature);
  const canUndo = useDesigner((s) => s.past.length > 0);
  const canRedo = useDesigner((s) => s.future.length > 0);
  const gesturing = useDesigner((s) => s.gesture !== null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const [autosave, setAutosave] = useState(() => typeof window !== "undefined" && window.localStorage.getItem(AUTOSAVE_KEY) === "1");
  const [publishOpen, setPublishOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [picker, setPicker] = useState<{ kind: "image" | "sprite"; particleLayer?: string } | null>(null);
  const [leftTab, setLeftTab] = useState("layers");
  const [rightTab, setRightTab] = useState("props");
  const [nameSample, setNameSample] = useState(initialItem.name);

  const saveDraft = useSaveCosmeticDraftMutation(appKey);
  const publish = usePublishCosmeticMutation(appKey);
  const uploadPreview = useUploadCosmeticPreviewMutation(appKey);
  const libraryAssets = useCosmeticAssetsQuery(appKey, { limit: 200 });
  const limits = catalog.vocabulary?.limits;
  const issues = useMemo(() => localIssues(design, slot, catalog), [design, slot, catalog]);

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const assetUrl = useMemo(() => {
    const map = new Map<number, string>();
    for (const asset of libraryAssets.data?.items ?? []) map.set(asset.id, asset.url);
    for (const ref of Object.values(design.assets ?? {})) if (ref.url) map.set(ref.id, ref.url);
    return (id: number) => map.get(id);
  }, [libraryAssets.data, design.assets]);

  const save = async (silent = false): Promise<boolean> => {
    if (readOnly) return false;
    const current = api.getState().design;
    try {
      const saved = await saveDraft.mutateAsync({ itemId: item.id, design: current });
      api.getState().markSaved(current);
      setItem((old) => ({ ...old, draftDirty: saved.draftDirty, revision: saved.revision, status: saved.status }));
      setSaveError(null);
      setSavedAt(Date.now());
      if (!silent) toast.success("草稿已保存");
      return true;
    } catch (error) {
      const message = errorMessage(error, "保存失败");
      setSaveError(message);
      if (!silent) toast.error(message);
      return false;
    }
  };

  // 自动保存：停手 2.5 秒后保存一次（拖动中不保存）
  const signature = designSignature(design);
  const saveRef = useRef(save);
  useEffect(() => {
    saveRef.current = save;
  });
  useEffect(() => {
    if (!autosave || readOnly || !dirty || gesturing) return;
    const timer = window.setTimeout(() => void saveRef.current(true), 2500);
    return () => window.clearTimeout(timer);
  }, [autosave, readOnly, dirty, gesturing, signature]);

  const runPublish = async (note: string, onStep: (steps: PublishStep[]) => void) => {
    const steps: PublishStep[] = [
      { key: "save", label: "保存草稿", state: "idle" },
      { key: "preview", label: "生成并上传预览图", state: "idle" },
      { key: "publish", label: "发布", state: "idle" }
    ];
    const mark = (key: PublishStep["key"], state: PublishStep["state"], detail?: string) => {
      const index = steps.findIndex((step) => step.key === key);
      steps[index] = { ...steps[index], state, detail };
      onStep([...steps]);
    };
    if (dirty || saveError) {
      mark("save", "running");
      if (!(await save(true))) {
        mark("save", "failed", "校验未通过");
        return false;
      }
      mark("save", "done");
    } else {
      mark("save", "skipped", "没有未保存的修改");
    }
    mark("preview", "running");
    try {
      const state = api.getState();
      const blob = await renderPreviewPng(state.design, slot, {
        time: designTime(state),
        elapsed: state.clock,
        nameText: item.name,
        maxBytes: limits?.maxPreviewBytes ?? 2 << 20
      });
      await uploadPreview.mutateAsync({ itemId: item.id, file: blob });
      mark("preview", "done");
    } catch (error) {
      const message =
        error instanceof DOMException && error.name === "SecurityError" ? "素材不允许跨域读取，未生成预览图" : errorMessage(error, "预览图上传失败");
      mark("preview", "failed", message);
    }
    mark("publish", "running");
    try {
      const published = await publish.mutateAsync({ itemId: item.id, note });
      setItem((old) => ({ ...old, ...published, design: undefined, draft: undefined }));
      mark("publish", "done");
      toast.success(`已发布第 ${published.revision} 版`);
      return true;
    } catch (error) {
      mark("publish", "failed", errorMessage(error, "发布失败"));
      toast.error(errorMessage(error, "发布失败"));
      return false;
    }
  };

  // ── 图层动作 ──
  const selectedLayerId = () => {
    const id = api.getState().selected;
    return id && id !== AVATAR_ID ? id : null;
  };

  const insertAsset = (asset: CosmeticAsset, kind: "image" | "sprite" = asset.kind) => {
    if (readOnly) return;
    if (api.getState().design.layers.length >= (limits?.maxLayers ?? 40)) {
      toast.error(`最多 ${limits?.maxLayers ?? 40} 个图层`);
      return;
    }
    let created = "";
    api.getState().update((d) => {
      const { design: next, key } = withAsset(d, asset);
      const layer = createAssetLayer(next, asset, key, kind);
      created = layer.id;
      return insertLayer(next, layer, selectedLayerId());
    });
    api.getState().select(created);
    setLeftTab("layers");
  };

  const replaceAsset = (asset: CosmeticAsset) => {
    const id = selectedLayerId();
    const layer = id ? api.getState().design.layers.find((item) => item.id === id) : undefined;
    if (!layer || (layer.type !== "image" && layer.type !== "sprite")) {
      toast.info("先选中一个图片或序列帧图层");
      return;
    }
    api.getState().update((d) => {
      const { design: next, key } = withAsset(d, asset);
      return patchLayer(next, layer.id, { asset: key });
    });
  };

  const pickParticleAsset = (asset: CosmeticAsset, layerId: string) => {
    api.getState().update((d) => {
      const { design: next, key } = withAsset(d, asset);
      return patchLayer(next, layerId, (l) => ({ ...l, particles: l.particles ? { ...l.particles, shape: "image", asset: key } : l.particles }));
    });
  };

  const copyLayer = () => {
    const id = selectedLayerId();
    const state = api.getState();
    const layer = id ? state.design.layers.find((item) => item.id === id) : undefined;
    if (!layer) return;
    const keys = [layer.asset, layer.particles?.asset].filter((key): key is string => Boolean(key));
    layerClipboard = {
      layer: JSON.parse(JSON.stringify(layer)) as Layer,
      assets: Object.fromEntries(keys.filter((key) => state.design.assets?.[key]).map((key) => [key, state.design.assets![key]]))
    };
    toast.success(`已复制「${layer.name || layer.id}」`);
  };

  const pasteLayer = () => {
    if (readOnly || !layerClipboard) return;
    const clip = layerClipboard;
    let created = "";
    api.getState().update((d) => {
      let next = d;
      const rename: Record<string, string> = {};
      for (const [key, ref] of Object.entries(clip.assets ?? {})) {
        const existing = Object.entries(next.assets ?? {}).find(([, other]) => other.id === ref.id);
        if (existing) {
          rename[key] = existing[0];
        } else {
          const result = withAsset(next, { ...ref, kind: "image", name: key, url: ref.url ?? "", width: ref.width ?? 0, height: ref.height ?? 0, size: 0, sha256: ref.sha256 ?? "", contentType: ref.contentType ?? "", createdAt: "", usedBy: 0 });
          next = result.design;
          rename[key] = result.key;
        }
      }
      const source: Layer = {
        ...clip.layer,
        asset: clip.layer.asset ? (rename[clip.layer.asset] ?? clip.layer.asset) : undefined,
        particles: clip.layer.particles
          ? { ...clip.layer.particles, asset: clip.layer.particles.asset ? (rename[clip.layer.particles.asset] ?? clip.layer.particles.asset) : undefined }
          : undefined
      };
      const copy = duplicateLayer(next, source);
      created = copy.id;
      return insertLayer(next, copy, selectedLayerId());
    });
    api.getState().select(created);
  };

  const duplicateSelected = () => {
    const id = selectedLayerId();
    if (readOnly || !id) return;
    let created = "";
    api.getState().update((d) => {
      const layer = d.layers.find((item) => item.id === id);
      if (!layer) return d;
      const copy = duplicateLayer(d, layer);
      created = copy.id;
      return insertLayer(d, copy, id);
    });
    if (created) api.getState().select(created);
  };

  const deleteSelected = () => {
    if (readOnly) return;
    const state = api.getState();
    if (state.keyframe) {
      const ref = state.keyframe;
      state.update((d) => patchLayer(d, ref.layerId, (l) => removeKeyframe(l, ref.property, ref.index)));
      state.selectKeyframe(null);
      return;
    }
    const id = selectedLayerId();
    if (!id) return;
    state.update((d) => removeLayer(d, id));
    state.select(null);
  };

  const nudge = (dx: number, dy: number) => {
    if (readOnly) return;
    const state = api.getState();
    if (state.selected === AVATAR_ID && state.design.avatar) {
      state.update(
        (d) => ({
          ...d,
          avatar: {
            ...d.avatar!,
            x: Math.max(0, Math.min(d.canvas.width - d.avatar!.size, d.avatar!.x + dx)),
            y: Math.max(0, Math.min(d.canvas.height - d.avatar!.size, d.avatar!.y + dy))
          }
        }),
        "nudge.avatar"
      );
      return;
    }
    const id = selectedLayerId();
    if (!id || state.locked[id]) return;
    state.update((d) => patchLayer(d, id, (l) => shiftAnimated(shiftAnimated(l, "x", dx), "y", dy)), `nudge.${id}`);
  };

  const hotkey = { preventDefault: true } as const;
  useHotkeys("mod+z", () => api.getState().undo(), hotkey);
  useHotkeys(["mod+shift+z", "mod+y"], () => api.getState().redo(), hotkey);
  useHotkeys("mod+s", () => void save(), { ...hotkey, enableOnFormTags: true });
  useHotkeys("mod+c", copyLayer);
  useHotkeys("mod+v", pasteLayer);
  useHotkeys("mod+d", duplicateSelected, hotkey);
  useHotkeys(["delete", "backspace"], deleteSelected, hotkey);
  useHotkeys("left", () => nudge(-1, 0), hotkey);
  useHotkeys("right", () => nudge(1, 0), hotkey);
  useHotkeys("up", () => nudge(0, -1), hotkey);
  useHotkeys("down", () => nudge(0, 1), hotkey);
  useHotkeys("shift+left", () => nudge(-10, 0), hotkey);
  useHotkeys("shift+right", () => nudge(10, 0), hotkey);
  useHotkeys("shift+up", () => nudge(0, -10), hotkey);
  useHotkeys("shift+down", () => nudge(0, 10), hotkey);
  useHotkeys("space", () => api.getState().setPlaying(!api.getState().playing), hotkey);
  useHotkeys("escape", () => {
    api.getState().select(null);
    api.getState().selectKeyframe(null);
  });

  const toggleAutosave = (value: boolean) => {
    setAutosave(value);
    window.localStorage.setItem(AUTOSAVE_KEY, value ? "1" : "0");
  };

  const layered = slot !== "name_style";
  const saveState = saveDraft.isPending ? "保存中" : saveError ? "保存失败" : dirty ? "未保存" : savedAt ? `已保存 ${formatClockSeconds(savedAt)}` : "已保存";

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      {/* 顶栏 */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-2 sm:px-3">
        <Button size="icon-sm" variant="ghost" onClick={onClose} aria-label="返回">
          <ArrowLeft />
        </Button>
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-semibold">{item.name}</span>
          <span className="hidden text-xs text-muted-foreground sm:inline">{slotLabel(slot, catalog)}</span>
          <StatusBadge status={item.status} />
          <RarityBadge rarity={item.rarity} catalog={catalog} className="hidden md:inline-flex" />
          {item.revision > 0 ? (
            <Badge variant="outline" size="sm" className="tabular-nums">
              已发布 v{item.revision}
            </Badge>
          ) : (
            <Badge variant="outline" size="sm">
              未发布
            </Badge>
          )}
          {readOnly ? (
            <Badge variant="secondary" size="sm">
              只读
            </Badge>
          ) : null}
        </div>
        <div className="mx-2 hidden items-center gap-0.5 md:flex">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="ghost" disabled={!canUndo || readOnly} onClick={() => api.getState().undo()} aria-label="撤销">
                <Undo2 />
              </Button>
            </TooltipTrigger>
            <TooltipContent>撤销 ⌘Z</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="ghost" disabled={!canRedo || readOnly} onClick={() => api.getState().redo()} aria-label="重做">
                <Redo2 />
              </Button>
            </TooltipTrigger>
            <TooltipContent>重做 ⌘⇧Z</TooltipContent>
          </Tooltip>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span
            className={cn(
              "hidden items-center gap-1 text-xs sm:flex",
              saveError ? "text-red-600 dark:text-red-400" : dirty ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
            )}
          >
            {saveDraft.isPending ? <Loader2 className="size-3 animate-spin" /> : !dirty && !saveError ? <CircleCheck className="size-3" /> : null}
            {saveState}
          </span>
          {!readOnly ? (
            <label className="hidden cursor-pointer items-center gap-1.5 text-xs text-muted-foreground lg:flex">
              <Switch checked={autosave} onCheckedChange={toggleAutosave} className="scale-90" />
              自动保存
            </label>
          ) : null}
          <Button size="sm" variant="ghost" onClick={() => setHistoryOpen(true)}>
            <History />
            <span className="hidden sm:inline">版本历史</span>
          </Button>
          {!readOnly ? (
            <Button size="sm" variant="outline" onClick={() => void save()} disabled={saveDraft.isPending}>
              {saveDraft.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          ) : null}
          {can.publish ? (
            <Button size="sm" onClick={() => setPublishOpen(true)} disabled={readOnly && dirty}>
              <Rocket />
              发布
            </Button>
          ) : null}
        </div>
      </div>
      {saveError ? (
        <div className="flex shrink-0 items-start gap-2 border-b bg-red-500/5 px-4 py-2 text-xs text-red-700 dark:text-red-300">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0 flex-1 break-all">{saveError}</span>
          <button type="button" className="shrink-0 underline-offset-2 hover:underline" onClick={() => setSaveError(null)}>
            关闭
          </button>
        </div>
      ) : null}

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)] lg:grid-cols-[256px_minmax(0,1fr)_300px]">
        {/* 左栏 */}
        <aside className="hidden min-h-0 flex-col border-r lg:flex">
          {layered ? (
            <Tabs value={leftTab} onValueChange={setLeftTab} className="flex min-h-0 flex-1 flex-col gap-0">
              <TabsList className="m-2 mb-0 grid h-8 grid-cols-2">
                <TabsTrigger value="layers" className="gap-1 text-xs">
                  <Layers />
                  图层
                </TabsTrigger>
                <TabsTrigger value="assets" className="gap-1 text-xs">
                  <ImagesIcon />
                  素材
                </TabsTrigger>
              </TabsList>
              <TabsContent value="layers" className="mt-2 flex min-h-0 flex-1 flex-col border-t">
                <DesignerLayers readOnly={readOnly} maxLayers={limits?.maxLayers ?? 40} onPickAsset={(kind) => setPicker({ kind })} />
              </TabsContent>
              <TabsContent value="assets" className="mt-2 flex min-h-0 flex-1 flex-col border-t">
                <AssetBrowser appKey={appKey} catalog={catalog} readOnly={readOnly} dense onInsert={(asset) => insertAsset(asset)} onReplace={replaceAsset} />
              </TabsContent>
            </Tabs>
          ) : (
            <div className="flex flex-col gap-2 p-3">
              <span className="text-xs font-semibold">示例昵称</span>
              <input
                value={nameSample}
                maxLength={16}
                onChange={(event) => setNameSample(event.target.value)}
                className="h-8 rounded-md border bg-background px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
              />
              <p className="text-xs text-muted-foreground">昵称样式不用图层，在右侧调整颜色与效果</p>
            </div>
          )}
        </aside>

        {/* 中间：画布 + 时间轴 */}
        <main className="flex min-h-0 min-w-0 flex-col">
          {layered ? <DesignerStage readOnly={readOnly} assetUrl={assetUrl} /> : <NameStage design={design} text={nameSample} />}
          {layered ? (
            <div className="h-56 shrink-0 border-t bg-card">
              <DesignerTimeline catalog={catalog} readOnly={readOnly} />
            </div>
          ) : null}
        </main>

        {/* 右栏 */}
        <aside className="hidden min-h-0 flex-col border-l lg:flex">
          <Tabs value={rightTab} onValueChange={setRightTab} className="flex min-h-0 flex-1 flex-col gap-0">
            <TabsList className="m-2 mb-0 grid h-8 grid-cols-3">
              <TabsTrigger value="props" className="gap-1 text-xs">
                <SlidersHorizontal />
                属性
              </TabsTrigger>
              <TabsTrigger value="preview" className="gap-1 text-xs">
                <View />
                预览
              </TabsTrigger>
              <TabsTrigger value="issues" className="gap-1 text-xs">
                问题
                {issues.length ? (
                  <Badge variant="warning" size="sm" className="h-4 px-1 text-[10px] tabular-nums">
                    {issues.length}
                  </Badge>
                ) : null}
              </TabsTrigger>
            </TabsList>
            <TabsContent value="props" className="mt-2 min-h-0 flex-1 overflow-y-auto border-t">
              <DesignerInspector catalog={catalog} readOnly={readOnly} onPickParticleAsset={(layerId) => setPicker({ kind: "image", particleLayer: layerId })} />
            </TabsContent>
            <TabsContent value="preview" className="mt-2 min-h-0 flex-1 overflow-y-auto border-t">
              <DesignerPreview slot={slot} design={design} />
            </TabsContent>
            <TabsContent value="issues" className="mt-2 min-h-0 flex-1 overflow-y-auto border-t">
              {issues.length === 0 ? (
                <div className="flex flex-col items-center gap-2 px-4 py-10 text-center text-xs text-muted-foreground">
                  <CircleCheck className="size-5 text-emerald-500" />
                  没有发现问题
                </div>
              ) : (
                <ul className="flex flex-col divide-y">
                  {issues.map((issue, index) => (
                    <li key={index}>
                      <button
                        type="button"
                        className="flex w-full items-start gap-2 px-3 py-2.5 text-left text-xs hover:bg-accent"
                        onClick={() => issue.layerId && api.getState().select(issue.layerId)}
                      >
                        <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
                        <span>{issue.message}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              <p className="px-3 py-2 text-[11px] text-muted-foreground">保存时以服务端校验为准</p>
            </TabsContent>
          </Tabs>
        </aside>
      </div>

      {picker ? (
        <AssetPickerDialog
          open
          onOpenChange={(open) => !open && setPicker(null)}
          appKey={appKey}
          catalog={catalog}
          kind={picker.kind}
          usedIds={Object.values(design.assets ?? {}).map((ref) => ref.id)}
          onPick={(asset) => {
            if (picker.particleLayer) pickParticleAsset(asset, picker.particleLayer);
            else insertAsset(asset, picker.kind);
          }}
        />
      ) : null}

      <PublishDialog
        open={publishOpen}
        onOpenChange={setPublishOpen}
        item={item}
        slot={slot}
        design={design}
        time={designTime(api.getState())}
        onPublish={runPublish}
      />

      <RevisionsSheet
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        appKey={appKey}
        item={item}
        slot={slot}
        dirty={dirty}
        canRestore={can.publish}
        assetUrl={assetUrl}
        onRestored={(restored) => {
          if (restored.draft) {
            const next = JSON.parse(JSON.stringify(restored.draft)) as Design;
            next.layers = next.layers ?? [];
            api.getState().replace(next);
            api.getState().markSaved(next);
          }
          setItem((old) => ({ ...old, draftDirty: restored.draftDirty }));
          setHistoryOpen(false);
        }}
      />
    </div>
  );
}
