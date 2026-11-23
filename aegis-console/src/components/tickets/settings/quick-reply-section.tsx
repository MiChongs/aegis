"use client";

import { useMemo, useState } from "react";
import { Eye, Lock, MessageSquareText, Pencil, Plus, Save, Search } from "lucide-react";
import { notify } from "@/lib/notify";
import {
  useDeleteTicketQuickReplyMutation,
  useSaveTicketQuickReplyMutation,
  useTicketCategoriesQuery,
  useTicketQuickRepliesQuery
} from "@/lib/ticket-hooks";
import type { TicketQuickReply } from "@/lib/api/tickets";
import { Markdown } from "@/components/ai/markdown";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  DeleteButton,
  EntityList,
  Field,
  ListSkeleton,
  ScopeBadges,
  SectionEmpty,
  SectionError,
  SectionHeader,
  SwitchRow,
  reportError
} from "./settings-shared";
import { cn } from "@/lib/utils";

// 快捷回复：回复框里敲 / 调出的话术。按使用次数排，常用的在前；
// 内容是 Markdown（App 端按 Markdown 渲染客服回复），编辑时可切到预览看效果。

type ReplyForm = {
  title: string;
  content: string;
  categoryId: string;
  private: boolean;
  sort: number;
  enabled: boolean;
};

const EMPTY_REPLY: ReplyForm = { title: "", content: "", categoryId: "none", private: false, sort: 0, enabled: true };

function formFromReply(item: TicketQuickReply): ReplyForm {
  return {
    title: item.title,
    content: item.content,
    categoryId: item.categoryId ? String(item.categoryId) : "none",
    private: Boolean(item.ownerAdminId),
    sort: item.sort,
    enabled: item.enabled
  };
}

export function QuickReplySection({ appId }: { appId: number }) {
  const query = useTicketQuickRepliesQuery(appId);
  const categoriesQuery = useTicketCategoriesQuery(appId);
  const saveMut = useSaveTicketQuickReplyMutation();
  const deleteMut = useDeleteTicketQuickReplyMutation();
  const [editing, setEditing] = useState<{ id?: number; form: ReplyForm } | null>(null);
  const [keyword, setKeyword] = useState("");

  const categoryName = useMemo(
    () => new Map((categoriesQuery.data ?? []).map((item) => [item.id, item.name])),
    [categoriesQuery.data]
  );
  const replies = useMemo(() => {
    const text = keyword.trim().toLowerCase();
    return [...(query.data ?? [])]
      .filter((item) => !text || item.title.toLowerCase().includes(text) || item.content.toLowerCase().includes(text))
      .sort((a, b) => b.usageCount - a.usageCount || a.sort - b.sort || a.id - b.id);
  }, [keyword, query.data]);
  const maxUsage = Math.max(1, ...replies.map((item) => item.usageCount));

  const save = async (id: number | undefined, form: ReplyForm) => {
    if (!form.title.trim() || !form.content.trim()) {
      notify.warning("请填写标题与内容");
      return false;
    }
    try {
      await saveMut.mutateAsync({
        id,
        payload: {
          appid: appId,
          title: form.title.trim(),
          content: form.content.trim(),
          categoryId: form.categoryId === "none" ? undefined : Number(form.categoryId),
          private: form.private,
          sort: Number(form.sort) || 0,
          enabled: form.enabled
        }
      });
      notify.success("快捷回复已保存");
      return true;
    } catch (error) {
      reportError(error, "保存失败");
      return false;
    }
  };

  const total = query.data?.length ?? 0;

  return (
    <div className="space-y-4">
      <SectionHeader
        title="快捷回复"
        description="回复时在空白回复框里输入 / 即可调出。按使用次数排序。"
        action={
          <>
            {total > 6 ? (
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder="搜索话术"
                  className="h-8 w-44 pl-8"
                />
              </div>
            ) : null}
            <Button size="sm" onClick={() => setEditing({ form: EMPTY_REPLY })}>
              <Plus />
              新建话术
            </Button>
          </>
        }
      />

      {query.isLoading ? (
        <ListSkeleton />
      ) : query.isError ? (
        <SectionError error={query.error} onRetry={() => void query.refetch()} />
      ) : total === 0 ? (
        <SectionEmpty
          icon={MessageSquareText}
          title="还没有快捷回复"
          description="把常用的答复存成话术，处理反馈时一键插入。"
          action={
            <Button size="sm" onClick={() => setEditing({ form: EMPTY_REPLY })}>
              <Plus />
              新建话术
            </Button>
          }
        />
      ) : replies.length === 0 ? (
        <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">没有匹配的话术</div>
      ) : (
        <EntityList>
          {replies.map((item) => (
            <div key={item.id} className={cn("flex items-start gap-3 px-4 py-3", !item.enabled && "bg-muted/30")}>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className={cn("text-sm font-medium", item.enabled ? "text-foreground" : "text-muted-foreground")}>
                    {item.title}
                  </span>
                  {item.ownerAdminId ? (
                    <Badge variant="secondary" size="sm">
                      <Lock className="size-3" />
                      仅自己
                    </Badge>
                  ) : null}
                  {item.categoryId && categoryName.get(item.categoryId) ? (
                    <Badge variant="outline" size="sm">
                      {categoryName.get(item.categoryId)}
                    </Badge>
                  ) : null}
                  <ScopeBadges platform={item.appid === 0 && appId !== 0} enabled={item.enabled} />
                </div>
                <p className="line-clamp-2 whitespace-pre-line text-xs text-muted-foreground">{item.content}</p>
              </div>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="hidden w-20 shrink-0 space-y-1 pt-1 sm:block">
                    <p className="text-right text-[11px] tabular-nums text-muted-foreground">{item.usageCount} 次</p>
                    <div className="h-1 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary/70"
                        style={{ width: `${Math.round((item.usageCount / maxUsage) * 100)}%` }}
                      />
                    </div>
                  </div>
                </TooltipTrigger>
                <TooltipContent>已使用 {item.usageCount} 次</TooltipContent>
              </Tooltip>
              <div className="flex shrink-0 items-center">
                <Button size="icon-sm" variant="ghost" aria-label="编辑" onClick={() => setEditing({ id: item.id, form: formFromReply(item) })}>
                  <Pencil />
                </Button>
                <DeleteButton
                  title={`删除话术「${item.title}」？`}
                  description="删除后回复框里不再出现这条话术，此操作不可撤销。"
                  pending={deleteMut.isPending}
                  onConfirm={async () => {
                    try {
                      await deleteMut.mutateAsync({ id: item.id, appid: appId });
                      notify.success("已删除");
                    } catch (error) {
                      reportError(error, "删除失败");
                    }
                  }}
                />
              </div>
            </div>
          ))}
        </EntityList>
      )}

      <ReplySheet
        key={editing ? `${editing.id ?? "new"}` : "closed"}
        editing={editing}
        categories={(categoriesQuery.data ?? []).map((item) => ({ id: item.id, name: item.name }))}
        pending={saveMut.isPending}
        onClose={() => setEditing(null)}
        onSave={async (form) => {
          if (await save(editing?.id, form)) setEditing(null);
        }}
      />
    </div>
  );
}

function ReplySheet({
  editing,
  categories,
  pending,
  onClose,
  onSave
}: {
  editing: { id?: number; form: ReplyForm } | null;
  categories: { id: number; name: string }[];
  pending: boolean;
  onClose: () => void;
  onSave: (form: ReplyForm) => void;
}) {
  const [form, setForm] = useState<ReplyForm>(() => editing?.form ?? EMPTY_REPLY);
  const patch = (next: Partial<ReplyForm>) => setForm((prev) => ({ ...prev, ...next }));

  return (
    <Sheet open={Boolean(editing)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle>{editing?.id ? "编辑话术" : "新建话术"}</SheetTitle>
          <SheetDescription>内容支持 Markdown，用户端按 Markdown 显示。</SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-4 overflow-y-auto p-4">
          <Field label="标题" hint="在快捷回复菜单里显示，也用于搜索">
            <Input value={form.title} onChange={(event) => patch({ title: event.target.value })} placeholder="已收到反馈" />
          </Field>
          <Tabs defaultValue="edit" className="gap-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">内容</span>
              <TabsList className="h-7">
                <TabsTrigger value="edit" className="text-xs">
                  <Pencil className="size-3" />
                  编辑
                </TabsTrigger>
                <TabsTrigger value="preview" className="text-xs">
                  <Eye className="size-3" />
                  预览
                </TabsTrigger>
              </TabsList>
            </div>
            <TabsContent value="edit" className="mt-0">
              <Textarea
                rows={10}
                value={form.content}
                onChange={(event) => patch({ content: event.target.value })}
                placeholder={"您好，感谢反馈！\n\n我们已经记录这个问题，修复后会第一时间通知您。"}
                className="font-mono text-sm"
              />
            </TabsContent>
            <TabsContent value="preview" className="mt-0">
              <div className="min-h-52 rounded-md border bg-muted/20 px-4 py-3 text-sm">
                {form.content.trim() ? (
                  <Markdown text={form.content} />
                ) : (
                  <p className="text-muted-foreground">还没有内容</p>
                )}
              </div>
            </TabsContent>
          </Tabs>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="适用分类" hint="留空表示所有分类通用">
              <Select value={form.categoryId} onValueChange={(value) => patch({ categoryId: value })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">全部分类</SelectItem>
                  {categories.map((item) => (
                    <SelectItem key={item.id} value={String(item.id)}>
                      {item.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="排序" hint="使用次数相同时，数字小的在前">
              <Input type="number" value={form.sort} onChange={(event) => patch({ sort: Number(event.target.value) })} />
            </Field>
          </div>
          <SwitchRow
            title="仅自己可见"
            description="其他处理人的回复框里不会出现"
            control={<Switch checked={form.private} onCheckedChange={(value) => patch({ private: value })} />}
          />
          <SwitchRow
            title="启用"
            control={<Switch checked={form.enabled} onCheckedChange={(value) => patch({ enabled: value })} />}
          />
        </div>
        <SheetFooter className="flex-row justify-end gap-2 border-t">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending} onClick={() => onSave(form)}>
            <Save />
            保存
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
