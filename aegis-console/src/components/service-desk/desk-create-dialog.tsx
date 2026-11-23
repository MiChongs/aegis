"use client";

import { useMemo, useState } from "react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useCreateTicketMutation, useTicketCategoriesQuery } from "@/lib/ticket-hooks";
import type { TicketPriority } from "@/lib/api/tickets";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { PRIORITY_LABEL } from "@/components/tickets/ticket-shared";
import { AppMark, useDeskApps } from "./desk-app-rail";
import { useDesk } from "./desk-context";

// 代用户登记一条工单 / 反馈，或建一张内部跟进单。
// 应用按「我创建的 → 其他 → 平台内部」排；默认落在工作台当前选中的应用。

const EMPTY_FORM = {
  title: "",
  content: "",
  categoryId: "none",
  priority: "normal" as TicketPriority,
  requesterName: "",
  requesterContact: ""
};

export function DeskCreateDialog({
  open,
  onClose,
  onCreated,
  defaultApp
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: number) => void;
  defaultApp: number | null;
}) {
  const desk = useDesk();
  const appsQuery = useAdminAppsQuery();
  const { owned } = useDeskApps();
  const createMut = useCreateTicketMutation();
  const fallbackApp = defaultApp ?? owned[0]?.appid ?? 0;
  const [appChoice, setAppChoice] = useState<string | null>(null);
  const appId = appChoice ?? String(fallbackApp);
  const categoriesQuery = useTicketCategoriesQuery(Number(appId) || 0);
  const [form, setForm] = useState(EMPTY_FORM);

  const ownedIds = useMemo(() => new Set(owned.map((app) => app.appid)), [owned]);
  const allApps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const mine = allApps.filter((app) => ownedIds.has(app.id));
  const others = allApps.filter((app) => !ownedIds.has(app.id));
  const categories = (categoriesQuery.data ?? []).filter((category) => (category.kind ?? "ticket") === desk.kind && category.enabled);

  const close = () => {
    setAppChoice(null);
    onClose();
  };

  const submit = async () => {
    if (!form.title.trim() || !form.content.trim()) {
      notify.warning("请填写标题与描述");
      return;
    }
    try {
      const ticket = await createMut.mutateAsync({
        appid: Number(appId) || 0,
        kind: desk.kind,
        title: form.title.trim(),
        content: form.content.trim(),
        categoryId: form.categoryId === "none" ? undefined : Number(form.categoryId),
        priority: form.priority,
        requesterName: form.requesterName.trim() || undefined,
        requesterContact: form.requesterContact.trim() || undefined,
        source: "console"
      });
      notify.success(`${desk.noun} ${ticket.ticketNo} 已创建`);
      setForm(EMPTY_FORM);
      close();
      onCreated(ticket.id);
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "创建失败");
    }
  };

  const appItem = (id: number, name: string) => (
    <SelectItem key={id} value={String(id)}>
      <span className="flex items-center gap-2">
        <AppMark appid={id} name={name} className="size-4 rounded text-[9px]" />
        {name}
      </span>
    </SelectItem>
  );

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>新建{desk.noun}</DialogTitle>
          <DialogDescription>代用户登记问题，或建一张内部跟进单。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>所属应用</Label>
              <Select
                value={appId}
                onValueChange={(value) => {
                  setAppChoice(value);
                  setForm((prev) => ({ ...prev, categoryId: "none" }));
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {mine.length ? (
                    <SelectGroup>
                      <SelectLabel>我创建的应用</SelectLabel>
                      {mine.map((app) => appItem(app.id, app.name))}
                    </SelectGroup>
                  ) : null}
                  {others.length ? (
                    <SelectGroup>
                      <SelectLabel>其他应用</SelectLabel>
                      {others.map((app) => appItem(app.id, app.name))}
                    </SelectGroup>
                  ) : null}
                  <SelectGroup>
                    <SelectLabel>平台</SelectLabel>
                    <SelectItem value="0">平台内部</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>分类</Label>
              <Select value={form.categoryId} onValueChange={(value) => setForm((prev) => ({ ...prev, categoryId: value }))}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不指定</SelectItem>
                  {categories.map((category) => (
                    <SelectItem key={category.id} value={String(category.id)}>
                      {category.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="desk-create-title">标题</Label>
            <Input
              id="desk-create-title"
              value={form.title}
              onChange={(event) => setForm((prev) => ({ ...prev, title: event.target.value }))}
              placeholder="一句话说明问题"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="desk-create-content">描述</Label>
            <Textarea
              id="desk-create-content"
              rows={5}
              value={form.content}
              onChange={(event) => setForm((prev) => ({ ...prev, content: event.target.value }))}
              placeholder="现象、复现步骤、期望结果"
            />
          </div>
          <div className="space-y-1.5">
            <Label>优先级</Label>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={form.priority}
              onValueChange={(value) => value && setForm((prev) => ({ ...prev, priority: value as TicketPriority }))}
              className="w-full"
            >
              {(["low", "normal", "high", "urgent"] as TicketPriority[]).map((priority) => (
                <ToggleGroupItem key={priority} value={priority} className="flex-1">
                  {PRIORITY_LABEL[priority]}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="desk-create-requester">{desk.person}</Label>
              <Input
                id="desk-create-requester"
                value={form.requesterName}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterName: event.target.value }))}
                placeholder="留空取当前管理员"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="desk-create-contact">联系方式</Label>
              <Input
                id="desk-create-contact"
                value={form.requesterContact}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterContact: event.target.value }))}
                placeholder="邮箱或手机"
              />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            取消
          </Button>
          <Button disabled={createMut.isPending} onClick={() => void submit()}>
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
