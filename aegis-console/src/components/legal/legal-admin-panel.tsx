"use client";

import { useCallback, useMemo, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import {
  Check,
  ExternalLink,
  Eye,
  FileText,
  Info,
  Languages,
  Plus,
  RotateCcw,
  Save,
  ScrollText,
  ShieldCheck,
  X,
} from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import {
  deleteAdminLegalDocument,
  getAdminLegalDocuments,
  previewAdminLegalDocument,
  saveAdminLegalDocument,
  type LegalDocType,
  type LegalDocument,
  type LegalLocaleOption,
} from "@/lib/api/legal";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichEditor } from "@/components/ui/rich-editor";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

const EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];

const DOC_META: Record<LegalDocType, { title: string; icon: typeof FileText; hint: string }> = {
  terms: {
    title: "用户服务协议",
    icon: FileText,
    hint: "约束使用方式、责任边界与争议解决",
  },
  privacy: {
    title: "隐私政策",
    icon: ShieldCheck,
    hint: "说明收集什么、为什么、共享给谁、存多久",
  },
};

type Draft = {
  docType: LegalDocType;
  locale: string;
  /** 这一份在服务端是否已经存在自定义版本。false = 正在起草新语言 */
  existing: boolean;
  title: string;
  body: string;
  version: string;
  effectiveAt: string;
  published: boolean;
};

const rowKey = (docType: LegalDocType, locale: string) => `${docType}/${locale}`;

/** 从 HTML 粗算正文规模。管理员判断「这份写完了没有」最直接的两个数。 */
function measure(html: string) {
  const sections = (html.match(/<h2/gi) || []).length;
  const text = html
    .replace(/<[^>]+>/g, " ")
    .replace(/&[a-z]+;/gi, " ")
    .replace(/\s+/g, "");
  return { sections, chars: text.length };
}

/**
 * 法律文本管理台。
 *
 * 列表把**自定义与内置合并**展示：管理员要看的是「这个文档在每种语言下现在
 * 对外是什么」，只列自己写过的会让「英文版其实还是内置的」完全看不出来。
 *
 * 编辑内置版本时以它为底稿 —— 从零写一份两万字的隐私政策没人会做，
 * 在一份写好的基础上改几条才是可行的。保存即产生自定义版本；
 * 「恢复内置」把自定义版本删掉，该语言回到内置全文。
 */
export function LegalAdminPanel() {
  const token = useAuthStore((state) => state.accessToken);
  const queryClient = useQueryClient();
  const reduced = useReducedMotion();

  const [draft, setDraft] = useState<Draft | null>(null);
  const [previewOf, setPreviewOf] = useState<string | null>(null);
  const [addingFor, setAddingFor] = useState<LegalDocType | null>(null);
  const [newLocale, setNewLocale] = useState("");
  const [seedLocale, setSeedLocale] = useState("");
  const [confirmReset, setConfirmReset] = useState<{ docType: LegalDocType; locale: string } | null>(null);
  const [discardTo, setDiscardTo] = useState<(() => void) | null>(null);

  const query = useQuery({
    queryKey: ["admin-legal-documents", token],
    queryFn: () => getAdminLegalDocuments(token as string),
    enabled: Boolean(token),
    // 4xx 不重试：权限不足、路由不存在这类结果重试多少次都一样，
    // 而每重试一次就多转一秒圈 —— 表现成「点了页签一直在加载」，
    // 真正的原因（一句写得很清楚的 403）却一直没机会显示出来。
    retry: (count, error) => {
      const status = error instanceof ApiError ? error.status ?? 0 : 0;
      if (status >= 400 && status < 500) return false;
      return count < 1;
    },
  });

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["admin-legal-documents"] });
    void queryClient.invalidateQueries({ queryKey: ["legal-document"] });
  }, [queryClient]);

  const saveMutation = useMutation({
    mutationFn: (input: Draft) =>
      saveAdminLegalDocument(token as string, input.docType, input.locale, {
        title: input.title,
        body: input.body,
        version: input.version,
        effectiveAt: input.effectiveAt,
        published: input.published,
      }),
    onSuccess: () => {
      toast.success("已保存，公开页立即生效");
      setDraft(null);
      invalidate();
    },
    onError: (error) => toast.error(error instanceof ApiError ? error.message : "保存失败"),
  });

  const resetMutation = useMutation({
    mutationFn: (input: { docType: LegalDocType; locale: string }) =>
      deleteAdminLegalDocument(token as string, input.docType, input.locale),
    onSuccess: () => {
      toast.success("已恢复内置版本");
      setDraft(null);
      setConfirmReset(null);
      invalidate();
    },
    onError: (error) => toast.error(error instanceof ApiError ? error.message : "恢复失败"),
  });

  // 有未保存草稿时切走要先问一句 —— 一份改了半小时的条款不该被一次误点清掉
  const guard = useCallback(
    (action: () => void) => {
      if (draft) {
        setDiscardTo(() => action);
        return;
      }
      action();
    },
    [draft]
  );

  const startEdit = useCallback(
    (item: LegalDocument) =>
      guard(() => {
        setPreviewOf(null);
        setDraft({
          docType: item.docType,
          locale: item.locale,
          existing: item.source === "custom",
          title: item.title,
          body: item.body,
          version: item.version,
          effectiveAt: item.effectiveAt ? item.effectiveAt.slice(0, 10) : "",
          published: item.published,
        });
      }),
    [guard]
  );

  if (!token) return <EmptyState title="未认证" description="请重新登录后再试。" />;
  if (query.isPending) return <LoadingState title="加载法律文本" />;
  if (query.isError || !query.data) {
    // 服务端的错误原文往往已经说清了该做什么（缺哪个权限点、去找谁授权），
    // 用一句「读取失败」盖掉它，等于把唯一有用的信息扔了。
    const message =
      query.error instanceof Error ? query.error.message : "服务端未返回法律文本清单。";
    const denied = query.error instanceof ApiError && query.error.status === 403;
    return (
      <Alert variant={denied ? "default" : "destructive"}>
        {denied ? <ShieldCheck /> : <Info />}
        <AlertTitle>{denied ? "没有管理法律文本的权限" : "读取失败"}</AlertTitle>
        <AlertDescription>{message}</AlertDescription>
      </Alert>
    );
  }

  const { items, contactConfigured, authoritativeLocale, builtinLocales } = query.data;

  const groups = (Object.keys(DOC_META) as LegalDocType[]).map((docType) => {
    const docs = items.filter((item) => item.docType === docType);
    return {
      docType,
      docs,
      custom: docs.filter((item) => item.source === "custom").length,
      hidden: docs.filter((item) => !item.published).length,
    };
  });

  return (
    <div className="space-y-5">
      {/* 内置文本的「联系我们」一节引用这个地址，没配就会印出占位文字。
          让管理员在这一页看见，而不是等用户翻到隐私政策最后一节才发现。 */}
      {contactConfigured ? null : (
        <Alert>
          <Info />
          <AlertDescription>
            尚未配置法律联系邮箱（<code className="font-data">LEGAL_CONTACT_EMAIL</code>）。
            内置文本的「联系我们」一节会显示占位文字，请在部署环境变量中补上。
          </AlertDescription>
        </Alert>
      )}

      {groups.map((group) => {
        const Icon = DOC_META[group.docType].icon;
        return (
          <section key={group.docType} className="rounded-xl border">
            <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-4 py-3">
              <Icon className="size-4 text-muted-foreground" />
              <div className="min-w-0">
                <h3 className="text-sm font-semibold">{DOC_META[group.docType].title}</h3>
                <p className="text-[11px] text-muted-foreground">{DOC_META[group.docType].hint}</p>
              </div>

              <div className="ml-auto flex flex-wrap items-center gap-2">
                <span className="text-[11px] text-muted-foreground">
                  {group.docs.length} 种语言 · 自定义 {group.custom}
                  {group.hidden ? ` · 未发布 ${group.hidden}` : ""}
                </span>
                <Link
                  href={`/legal/${group.docType}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                >
                  公开页 <ExternalLink className="size-3" />
                </Link>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8"
                  onClick={() =>
                    guard(() => {
                      setNewLocale("");
                      setSeedLocale(group.docs[0]?.locale ?? "");
                      setAddingFor(addingFor === group.docType ? null : group.docType);
                    })
                  }
                >
                  <Plus className="size-3.5" />
                  新增语言
                </Button>
              </div>
            </header>

            {/* ── 新增语言 ── */}
            <AnimatePresence initial={false}>
              {addingFor === group.docType ? (
                <m.div
                  initial={reduced ? false : { opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: "auto" }}
                  exit={reduced ? undefined : { opacity: 0, height: 0 }}
                  transition={{ duration: 0.22, ease: EASE }}
                  className="overflow-hidden border-b bg-muted/30"
                >
                  <AddLocaleForm
                    builtinLocales={builtinLocales}
                    existing={group.docs.map((item) => item.locale)}
                    docs={group.docs}
                    locale={newLocale}
                    onLocaleChange={setNewLocale}
                    seed={seedLocale}
                    onSeedChange={setSeedLocale}
                    onCancel={() => setAddingFor(null)}
                    onStart={() => {
                      const base = group.docs.find((item) => item.locale === seedLocale) ?? group.docs[0];
                      setDraft({
                        docType: group.docType,
                        locale: newLocale.trim(),
                        existing: false,
                        title: base?.title ?? DOC_META[group.docType].title,
                        body: base?.body ?? "",
                        version: base?.version ?? "",
                        effectiveAt: "",
                        published: true,
                      });
                      setAddingFor(null);
                    }}
                  />
                </m.div>
              ) : null}
            </AnimatePresence>

            <div className="divide-y">
              {group.docs.map((item) => {
                const key = rowKey(item.docType, item.locale);
                const isDraftRow =
                  draft && draft.docType === item.docType && draft.locale === item.locale;
                const stats = measure(item.body);
                return (
                  <div key={key} className="px-4 py-3">
                    <div className="flex flex-wrap items-center gap-2.5">
                      <span className="font-data text-sm">{item.locale}</span>
                      <Badge variant={item.source === "custom" ? "secondary" : "outline"} size="sm">
                        {item.source === "custom" ? "自定义" : "内置"}
                      </Badge>
                      {item.locale === authoritativeLocale ? (
                        <Badge variant="info" size="sm">
                          准据文本
                        </Badge>
                      ) : null}
                      {item.published ? null : (
                        <Badge variant="warning" size="sm">
                          未发布
                        </Badge>
                      )}
                      <span className="text-[11px] text-muted-foreground">
                        {stats.sections} 节 · 约 {stats.chars.toLocaleString("zh-CN")} 字
                        {item.version ? ` · 版本 ${item.version}` : ""}
                      </span>

                      <div className="ml-auto flex items-center gap-1.5">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8 text-muted-foreground"
                          onClick={() => setPreviewOf(previewOf === key ? null : key)}
                        >
                          <Eye className="size-3.5" />
                          {previewOf === key ? "收起" : "预览"}
                        </Button>
                        {item.source === "custom" ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 text-muted-foreground"
                            onClick={() =>
                              setConfirmReset({ docType: item.docType, locale: item.locale })
                            }
                          >
                            <RotateCcw className="size-3.5" />
                            恢复内置
                          </Button>
                        ) : null}
                        <Button
                          variant={isDraftRow ? "secondary" : "outline"}
                          size="sm"
                          className="h-8"
                          onClick={() => (isDraftRow ? setDraft(null) : startEdit(item))}
                        >
                          {isDraftRow ? "取消" : "编辑"}
                        </Button>
                      </div>
                    </div>

                    {!isDraftRow && previewOf !== key ? (
                      <p className="mt-1.5 line-clamp-2 text-xs leading-5 text-muted-foreground">
                        {item.summary}
                      </p>
                    ) : null}

                    {/* ── 只读预览 ── */}
                    <AnimatePresence initial={false}>
                      {previewOf === key && !isDraftRow ? (
                        <m.div
                          initial={reduced ? false : { opacity: 0, height: 0 }}
                          animate={{ opacity: 1, height: "auto" }}
                          exit={reduced ? undefined : { opacity: 0, height: 0 }}
                          transition={{ duration: 0.25, ease: EASE }}
                          className="overflow-hidden"
                        >
                          <div className="mt-3 max-h-96 overflow-y-auto rounded-lg border bg-card p-4">
                            {/* 正文由服务端在写入时净化，此处直接渲染 */}
                            <div
                              className="legal-prose text-[13px]"
                              dangerouslySetInnerHTML={{ __html: item.body }}
                            />
                          </div>
                        </m.div>
                      ) : null}
                    </AnimatePresence>

                    {/* ── 编辑 ── */}
                    <AnimatePresence initial={false}>
                      {isDraftRow && draft ? (
                        <m.div
                          initial={reduced ? false : { opacity: 0, height: 0 }}
                          animate={{ opacity: 1, height: "auto" }}
                          exit={reduced ? undefined : { opacity: 0, height: 0 }}
                          transition={{ duration: 0.25, ease: EASE }}
                          className="overflow-hidden"
                        >
                          <DraftEditor
                            draft={draft}
                            token={token}
                            onChange={setDraft}
                            saving={saveMutation.isPending}
                            onSave={() => saveMutation.mutate(draft)}
                          />
                        </m.div>
                      ) : null}
                    </AnimatePresence>
                  </div>
                );
              })}

              {/* 新语言的起草表单：它还不在列表里，单独挂一行 */}
              {draft &&
              draft.docType === group.docType &&
              !group.docs.some((item) => item.locale === draft.locale) ? (
                <div className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <span className="font-data text-sm">{draft.locale}</span>
                    <Badge variant="secondary" size="sm">
                      新语言（尚未保存）
                    </Badge>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto h-8"
                      onClick={() => setDraft(null)}
                    >
                      取消
                    </Button>
                  </div>
                  <DraftEditor
                    draft={draft}
                    token={token}
                    onChange={setDraft}
                    saving={saveMutation.isPending}
                    onSave={() => saveMutation.mutate(draft)}
                  />
                </div>
              ) : null}
            </div>
          </section>
        );
      })}

      {/* 恢复内置是不可撤销的删除，必须确认 */}
      <AlertDialog open={Boolean(confirmReset)} onOpenChange={(open) => !open && setConfirmReset(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>恢复内置版本？</AlertDialogTitle>
            <AlertDialogDescription>
              将删除 <span className="font-data">{confirmReset?.locale}</span> 的自定义文本，
              该语言随即回到系统内置全文。<strong>此操作不可撤销</strong>，请先自行留存副本。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => confirmReset && resetMutation.mutate(confirmReset)}
              disabled={resetMutation.isPending}
            >
              恢复内置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* 未保存草稿的离开确认 */}
      <AlertDialog open={Boolean(discardTo)} onOpenChange={(open) => !open && setDiscardTo(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>
              当前有一份没有保存的草稿，继续操作会丢弃它。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>回去继续编辑</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const next = discardTo;
                setDraft(null);
                setDiscardTo(null);
                next?.();
              }}
            >
              放弃
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/* ── 新增语言 ── */

function AddLocaleForm({
  builtinLocales,
  existing,
  docs,
  locale,
  onLocaleChange,
  seed,
  onSeedChange,
  onCancel,
  onStart,
}: {
  builtinLocales: LegalLocaleOption[];
  existing: string[];
  docs: LegalDocument[];
  locale: string;
  onLocaleChange: (value: string) => void;
  seed: string;
  onSeedChange: (value: string) => void;
  onCancel: () => void;
  onStart: () => void;
}) {
  // 已经存在的语言不再列出来 —— 选了也只是覆盖，那是「编辑」不是「新增」
  const candidates = builtinLocales.filter((item) => !existing.includes(item.locale));
  const duplicate = existing.includes(locale.trim());

  return (
    <div className="space-y-3 px-4 py-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label className="text-xs">语言</Label>
          {candidates.length > 0 ? (
            <Select value={locale} onValueChange={onLocaleChange}>
              <SelectTrigger size="sm" className="w-full text-sm">
                <SelectValue placeholder="选择内置语言" />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((item) => (
                  <SelectItem key={item.locale} value={item.locale}>
                    {item.nativeName}
                    <span className="ml-1.5 text-xs text-muted-foreground">{item.locale}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
          <Input
            className="h-8 text-sm"
            placeholder="或直接填 BCP 47：ja / zh-Hant / fr"
            value={locale}
            onChange={(event) => onLocaleChange(event.target.value)}
            aria-invalid={duplicate}
          />
          {duplicate ? (
            <p className="text-[11px] text-destructive">该语言已存在，请直接编辑它</p>
          ) : null}
        </div>

        <div className="space-y-1.5 sm:col-span-2">
          <Label className="text-xs">以哪一份为底稿</Label>
          <Select value={seed} onValueChange={onSeedChange}>
            <SelectTrigger size="sm" className="w-full text-sm">
              <SelectValue placeholder="选择底稿" />
            </SelectTrigger>
            <SelectContent>
              {docs.map((item) => (
                <SelectItem key={item.locale} value={item.locale}>
                  {item.locale} · {item.title}
                  <span className="ml-1.5 text-xs text-muted-foreground">
                    {item.source === "custom" ? "自定义" : "内置"}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {/* 从零起草一份两万字的条款没人会做，在写好的基础上改才可行 */}
          <p className="text-[11px] text-muted-foreground/80">
            正文会复制过来供你翻译改写，不会自动翻译
          </p>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" className="h-8" onClick={onCancel}>
          取消
        </Button>
        <Button size="sm" className="h-8" disabled={!locale.trim() || duplicate} onClick={onStart}>
          开始起草
        </Button>
      </div>
    </div>
  );
}

/* ── 编辑器 ── */

function DraftEditor({
  draft,
  token,
  onChange,
  saving,
  onSave,
}: {
  draft: Draft;
  token: string;
  onChange: (next: Draft) => void;
  saving: boolean;
  onSave: () => void;
}) {
  const [tab, setTab] = useState("edit");
  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    onChange({ ...draft, [key]: value });

  const stats = useMemo(() => measure(draft.body), [draft.body]);

  // 预览走服务端渲染：占位符的取值规则只在服务端实现一次，
  // 前端自己替换必然和公开页渲染出的结果不一致，而预览的意义就是所见即所得。
  const preview = useQuery({
    queryKey: ["legal-preview", draft.docType, draft.locale, draft.body],
    queryFn: () => previewAdminLegalDocument(token, draft.docType, draft.locale, draft.body),
    enabled: tab === "preview" && draft.body.trim().length > 0,
    staleTime: 30_000,
  });

  return (
    <div className="mt-3 space-y-3 rounded-lg border bg-muted/30 p-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="space-y-1.5 sm:col-span-2">
          <Label className="text-xs">标题</Label>
          <Input
            className="h-8 text-sm"
            value={draft.title}
            onChange={(event) => patch("title", event.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">版本号</Label>
          <Input
            className="h-8 text-sm"
            placeholder="2026.08"
            value={draft.version}
            onChange={(event) => patch("version", event.target.value)}
          />
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label className="text-xs">生效日期</Label>
          <Input
            type="date"
            className="h-8 text-sm"
            value={draft.effectiveAt}
            onChange={(event) => patch("effectiveAt", event.target.value)}
          />
          {/* 生效日期不等于保存时间：条款可以今天写好、下月生效 */}
          <p className="text-[11px] text-muted-foreground/70">留空则页面上不显示生效日期</p>
        </div>
        <div className="flex items-end pb-1 sm:col-span-2">
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={draft.published}
              onCheckedChange={(value) => patch("published", value)}
            />
            对外发布
            <span className="text-xs text-muted-foreground">
              关闭后公开页不再提供这个语言
            </span>
          </label>
        </div>
      </div>

      <Separator />

      <Tabs value={tab} onValueChange={setTab} className="gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <TabsList>
            <TabsTrigger value="edit">
              <ScrollText className="size-3.5" />
              编辑
            </TabsTrigger>
            <TabsTrigger value="preview">
              <Eye className="size-3.5" />
              预览
            </TabsTrigger>
          </TabsList>
          <span className="text-[11px] text-muted-foreground">
            {stats.sections} 节 · 约 {stats.chars.toLocaleString("zh-CN")} 字
          </span>
          <span className="ml-auto flex items-center gap-1 text-[11px] text-muted-foreground/70">
            <Languages className="size-3" />
            <code className="font-data">{"{{platformName}}"}</code>
            <code className="font-data">{"{{contactEmail}}"}</code>
            展示时由服务端替换
          </span>
        </div>

        <TabsContent value="edit">
          <RichEditor value={draft.body} onChange={(value) => patch("body", value)} />
        </TabsContent>

        <TabsContent value="preview">
          <div className="max-h-[28rem] overflow-y-auto rounded-lg border bg-card p-4">
            {preview.isPending ? (
              <p className="text-xs text-muted-foreground">正在渲染…</p>
            ) : preview.isError ? (
              <p className="text-xs text-destructive">预览渲染失败</p>
            ) : (
              <div
                className="legal-prose text-[13px]"
                dangerouslySetInnerHTML={{ __html: preview.data?.body ?? "" }}
              />
            )}
          </div>
          <p className="mt-1.5 text-[11px] text-muted-foreground/70">
            与公开页同一套渲染，占位符已按当前部署替换
          </p>
        </TabsContent>
      </Tabs>

      <div className="flex items-center justify-end gap-2">
        <span
          className={cn(
            "mr-auto flex items-center gap-1 text-[11px]",
            stats.chars > 500 ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"
          )}
        >
          {stats.chars > 500 ? <Check className="size-3" /> : <X className="size-3" />}
          {stats.chars > 500 ? "篇幅正常" : "正文偏短，确认是否写完"}
        </span>
        <Button size="sm" className="h-8" disabled={saving || !draft.title.trim()} onClick={onSave}>
          <Save className="size-3.5" />
          {saving ? "保存中…" : draft.existing ? "保存" : "创建并发布"}
        </Button>
      </div>
    </div>
  );
}
