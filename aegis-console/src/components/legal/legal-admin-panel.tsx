"use client";

import { useCallback, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { ExternalLink, Info, Plus, RotateCcw, Save, ScrollText } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import {
  deleteAdminLegalDocument,
  getAdminLegalDocuments,
  saveAdminLegalDocument,
  type LegalDocType,
  type LegalDocument,
} from "@/lib/api/legal";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichEditor } from "@/components/ui/rich-editor";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

const DOC_TITLE: Record<LegalDocType, string> = {
  terms: "用户服务协议",
  privacy: "隐私政策",
};

type Draft = {
  key: string;
  title: string;
  body: string;
  version: string;
  effectiveAt: string;
  published: boolean;
};

const docKey = (docType: LegalDocType, locale: string) => `${docType}/${locale}`;

/**
 * 法律文本管理。
 *
 * 列表把**自定义与内置合并**展示：管理员要看的是「这个文档在每种语言下现在
 * 对外是什么」，只列自己写过的会让「英文版其实还是内置的」这件事完全看不出来。
 *
 * 编辑内置版本时以它为底稿 —— 从零写一份两万字的隐私政策没人会做，
 * 而在一份写好的基础上改几条是可行的。保存即产生自定义版本；
 * 「恢复内置」把自定义版本删掉，该语言回到内置全文。
 */
export function LegalAdminPanel() {
  const token = useAuthStore((state) => state.accessToken);
  const queryClient = useQueryClient();
  const reduced = useReducedMotion();

  const [draft, setDraft] = useState<Draft | null>(null);
  const [newLocale, setNewLocale] = useState("");
  const [pendingDocType, setPendingDocType] = useState<LegalDocType | null>(null);

  const query = useQuery({
    queryKey: ["admin-legal-documents", token],
    queryFn: () => getAdminLegalDocuments(token as string),
    enabled: Boolean(token),
  });

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["admin-legal-documents"] });
    void queryClient.invalidateQueries({ queryKey: ["legal-document"] });
  }, [queryClient]);

  const saveMutation = useMutation({
    mutationFn: (input: { docType: LegalDocType; locale: string; draft: Draft }) =>
      saveAdminLegalDocument(token as string, input.docType, input.locale, {
        title: input.draft.title,
        body: input.draft.body,
        version: input.draft.version,
        effectiveAt: input.draft.effectiveAt,
        published: input.draft.published,
      }),
    onSuccess: () => {
      toast.success("已保存");
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
      invalidate();
    },
    onError: (error) => toast.error(error instanceof ApiError ? error.message : "恢复失败"),
  });

  const startEdit = useCallback((item: LegalDocument) => {
    setDraft({
      key: docKey(item.docType, item.locale),
      title: item.title,
      body: item.body,
      version: item.version,
      effectiveAt: item.effectiveAt ? item.effectiveAt.slice(0, 10) : "",
      published: item.published,
    });
  }, []);

  if (!token) return <EmptyState title="未认证" description="请重新登录后再试。" />;
  if (query.isPending) return <LoadingState title="加载法律文本" />;
  if (query.isError || !query.data) {
    return <EmptyState title="读取失败" description="服务端未返回法律文本清单。" />;
  }

  const { items, contactConfigured } = query.data;
  const grouped = (["terms", "privacy"] as LegalDocType[]).map((docType) => ({
    docType,
    docs: items.filter((item) => item.docType === docType),
  }));

  return (
    <div className="space-y-5">
      {/* 内置文本的「联系我们」一节引用这个地址，没配就会印出一句占位文字。
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

      {grouped.map((group) => (
        <section key={group.docType} className="rounded-xl border">
          <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
            <ScrollText className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">{DOC_TITLE[group.docType]}</h3>
            <Link
              href={`/legal/${group.docType}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              查看公开页 <ExternalLink className="size-3" />
            </Link>

            <div className="ml-auto flex items-center gap-2">
              <AnimatePresence initial={false}>
                {pendingDocType === group.docType ? (
                  <m.div
                    initial={reduced ? false : { opacity: 0, width: 0 }}
                    animate={{ opacity: 1, width: "auto" }}
                    exit={reduced ? undefined : { opacity: 0, width: 0 }}
                    className="flex items-center gap-2 overflow-hidden"
                  >
                    <Input
                      value={newLocale}
                      onChange={(event) => setNewLocale(event.target.value)}
                      placeholder="ja / zh-Hant / fr"
                      className="h-8 w-36 text-xs"
                      aria-label="语言标签"
                    />
                    <Button
                      size="sm"
                      className="h-8"
                      disabled={!newLocale.trim()}
                      onClick={() => {
                        // 以本文档的第一份文本为底稿：从零起草一份法律文本没人会做
                        const seed = group.docs[0];
                        setDraft({
                          key: docKey(group.docType, newLocale.trim()),
                          title: seed?.title ?? DOC_TITLE[group.docType],
                          body: seed?.body ?? "",
                          version: seed?.version ?? "",
                          effectiveAt: "",
                          published: true,
                        });
                        setPendingDocType(null);
                      }}
                    >
                      起草
                    </Button>
                  </m.div>
                ) : null}
              </AnimatePresence>
              <Button
                variant="outline"
                size="sm"
                className="h-8"
                onClick={() => {
                  setNewLocale("");
                  setPendingDocType(pendingDocType === group.docType ? null : group.docType);
                }}
              >
                <Plus className="size-3.5" />
                新增语言
              </Button>
            </div>
          </header>

          <div className="divide-y">
            {group.docs.map((item) => {
              const key = docKey(item.docType, item.locale);
              const editing = draft?.key === key;
              return (
                <div key={key} className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <span className="font-data text-sm">{item.locale}</span>
                    <Badge variant={item.source === "custom" ? "secondary" : "outline"} size="sm">
                      {item.source === "custom" ? "自定义" : "内置"}
                    </Badge>
                    {item.version ? (
                      <span className="text-xs text-muted-foreground">版本 {item.version}</span>
                    ) : null}
                    {item.published ? null : (
                      <Badge variant="warning" size="sm">
                        未发布
                      </Badge>
                    )}

                    <div className="ml-auto flex items-center gap-2">
                      {item.source === "custom" ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8 text-muted-foreground"
                          disabled={resetMutation.isPending}
                          onClick={() =>
                            resetMutation.mutate({ docType: item.docType, locale: item.locale })
                          }
                        >
                          <RotateCcw className="size-3.5" />
                          恢复内置
                        </Button>
                      ) : null}
                      <Button
                        variant={editing ? "secondary" : "outline"}
                        size="sm"
                        className="h-8"
                        onClick={() => (editing ? setDraft(null) : startEdit(item))}
                      >
                        {editing ? "取消" : "编辑"}
                      </Button>
                    </div>
                  </div>

                  {!editing ? (
                    <p className="mt-1.5 line-clamp-2 text-xs leading-5 text-muted-foreground">
                      {item.summary}
                    </p>
                  ) : null}

                  <AnimatePresence initial={false}>
                    {editing && draft ? (
                      <m.div
                        initial={reduced ? false : { opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={reduced ? undefined : { opacity: 0, height: 0 }}
                        transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
                        className="overflow-hidden"
                      >
                        <DraftEditor
                          draft={draft}
                          onChange={setDraft}
                          saving={saveMutation.isPending}
                          onSave={() =>
                            saveMutation.mutate({
                              docType: item.docType,
                              locale: item.locale,
                              draft,
                            })
                          }
                        />
                      </m.div>
                    ) : null}
                  </AnimatePresence>
                </div>
              );
            })}

            {/* 新语言的起草表单挂在分组底部 */}
            {draft && !group.docs.some((item) => docKey(item.docType, item.locale) === draft.key) &&
            draft.key.startsWith(`${group.docType}/`) ? (
              <div className="px-4 py-3">
                <div className="flex items-center gap-2.5">
                  <span className="font-data text-sm">{draft.key.split("/")[1]}</span>
                  <Badge variant="secondary" size="sm">
                    新语言
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
                  onChange={setDraft}
                  saving={saveMutation.isPending}
                  onSave={() =>
                    saveMutation.mutate({
                      docType: group.docType,
                      locale: draft.key.split("/")[1],
                      draft,
                    })
                  }
                />
              </div>
            ) : null}
          </div>
        </section>
      ))}
    </div>
  );
}

function DraftEditor({
  draft,
  onChange,
  saving,
  onSave,
}: {
  draft: Draft;
  onChange: (next: Draft) => void;
  saving: boolean;
  onSave: () => void;
}) {
  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    onChange({ ...draft, [key]: value });

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
          {/* 生效日期不等于保存时间：条款可以今天写好、下月生效，
              用保存时间冒充会让公示的日期是错的 */}
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

      <div className="space-y-1.5">
        <Label className="text-xs">正文</Label>
        <RichEditor value={draft.body} onChange={(value) => patch("body", value)} />
        <p className="text-[11px] text-muted-foreground/70">
          支持 <code className="font-data">{"{{platformName}}"}</code> 与{" "}
          <code className="font-data">{"{{contactEmail}}"}</code> 占位符，展示时由服务端替换
        </p>
      </div>

      <div className="flex justify-end">
        <Button size="sm" className={cn("h-8")} disabled={saving} onClick={onSave}>
          <Save className="size-3.5" />
          {saving ? "保存中…" : "保存"}
        </Button>
      </div>
    </div>
  );
}
