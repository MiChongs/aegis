"use client";

import { useMemo, useState } from "react";
import { Plus, Save, Trash2, UserCog } from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  useAdminOptions,
  useDeleteTicketCategoryMutation,
  useDeleteTicketGroupMutation,
  useDeleteTicketQuickReplyMutation,
  useDeleteTicketSLAPolicyMutation,
  useSaveTicketCategoryMutation,
  useSaveTicketGroupMutation,
  useSaveTicketQuickReplyMutation,
  useSaveTicketSLAPolicyMutation,
  useSetTicketGroupMembersMutation,
  useTicketCategoriesQuery,
  useTicketGroupsQuery,
  useTicketQuickRepliesQuery,
  useTicketSLAPoliciesQuery
} from "@/lib/ticket-hooks";
import type { TicketGroup, TicketPriority } from "@/lib/api/tickets";
import { PRIORITY_LABEL } from "./ticket-shared";

// 工单配置面板：分类 / 处理组 / SLA / 快捷回复。
//
// appId=0 表示平台级配置（对所有应用生效），后端要求超管才能改；
// 选中具体应用则是该应用的私有配置，应用管理员即可维护。

const PRIORITIES: TicketPriority[] = ["urgent", "high", "normal", "low"];

export function TicketSettingsPanel({ appId }: { appId: number }) {
  return (
    <Tabs defaultValue="categories" className="space-y-4">
      <TabsList className="flex-wrap">
        <TabsTrigger value="categories">分类</TabsTrigger>
        <TabsTrigger value="groups">处理组</TabsTrigger>
        <TabsTrigger value="sla">SLA 策略</TabsTrigger>
        <TabsTrigger value="quick-replies">快捷回复</TabsTrigger>
      </TabsList>
      <TabsContent value="categories">
        <CategoriesSection appId={appId} />
      </TabsContent>
      <TabsContent value="groups">
        <GroupsSection appId={appId} />
      </TabsContent>
      <TabsContent value="sla">
        <SLASection appId={appId} />
      </TabsContent>
      <TabsContent value="quick-replies">
        <QuickRepliesSection appId={appId} />
      </TabsContent>
    </Tabs>
  );
}

function reportError(error: unknown, fallback: string) {
  notify.error(error instanceof ApiError ? error.message : fallback);
}

// ─────────────── 分类 ───────────────

function CategoriesSection({ appId }: { appId: number }) {
  const query = useTicketCategoriesQuery(appId);
  const groupsQuery = useTicketGroupsQuery(appId);
  const slaQuery = useTicketSLAPoliciesQuery(appId);
  const saveMut = useSaveTicketCategoryMutation();
  const deleteMut = useDeleteTicketCategoryMutation();

  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | undefined>();
  const [form, setForm] = useState({
    key: "",
    name: "",
    description: "",
    defaultPriority: "normal" as TicketPriority,
    defaultGroupId: "none",
    slaPolicyId: "none",
    userSubmittable: true,
    sort: 0,
    enabled: true
  });

  const openCreate = () => {
    setEditingId(undefined);
    setForm({
      key: "",
      name: "",
      description: "",
      defaultPriority: "normal",
      defaultGroupId: "none",
      slaPolicyId: "none",
      userSubmittable: true,
      sort: 0,
      enabled: true
    });
    setOpen(true);
  };

  const handleSave = async () => {
    if (!form.key.trim() || !form.name.trim()) {
      notify.warning("请填写分类标识与名称");
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editingId,
        payload: {
          appid: appId,
          key: form.key.trim(),
          name: form.name.trim(),
          description: form.description.trim(),
          defaultPriority: form.defaultPriority,
          defaultGroupId: form.defaultGroupId === "none" ? undefined : Number(form.defaultGroupId),
          slaPolicyId: form.slaPolicyId === "none" ? undefined : Number(form.slaPolicyId),
          userSubmittable: form.userSubmittable,
          sort: Number(form.sort) || 0,
          enabled: form.enabled
        }
      });
      notify.success("分类已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const items = query.data ?? [];

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">分类决定默认优先级、默认处理组与 SLA 策略</p>
        <Button size="sm" onClick={openCreate}>
          <Plus className="mr-1 size-3.5" />
          新建分类
        </Button>
      </div>

      {items.length === 0 ? (
        <EmptyState title="暂无分类" description="创建分类后，提单表单才能按业务归类并自动套用 SLA。" />
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {items.map((item) => (
            <Card key={item.id}>
              <CardContent className="flex items-start justify-between gap-3 p-4">
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-sm font-medium text-foreground">{item.name}</span>
                    <Badge variant="outline" size="sm" className="font-mono text-[10px]">
                      {item.key}
                    </Badge>
                    {item.appid === 0 ? (
                      <Badge variant="secondary" size="sm">
                        平台级
                      </Badge>
                    ) : null}
                    {!item.enabled ? (
                      <Badge variant="danger" size="sm">
                        已停用
                      </Badge>
                    ) : null}
                  </div>
                  <p className="truncate text-xs text-muted-foreground">{item.description || "无描述"}</p>
                  <div className="flex flex-wrap gap-1 text-[11px] text-muted-foreground">
                    <span>默认 {PRIORITY_LABEL[item.defaultPriority]}</span>
                    <span>·</span>
                    <span>{item.userSubmittable ? "用户可自助提交" : "仅管理员代提"}</span>
                  </div>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setEditingId(item.id);
                      setForm({
                        key: item.key,
                        name: item.name,
                        description: item.description,
                        defaultPriority: item.defaultPriority,
                        defaultGroupId: item.defaultGroupId ? String(item.defaultGroupId) : "none",
                        slaPolicyId: item.slaPolicyId ? String(item.slaPolicyId) : "none",
                        userSubmittable: item.userSubmittable,
                        sort: item.sort,
                        enabled: item.enabled
                      });
                      setOpen(true);
                    }}
                  >
                    编辑
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive"
                    onClick={async () => {
                      try {
                        await deleteMut.mutateAsync(item.id);
                        notify.success("分类已删除");
                      } catch (error) {
                        reportError(error, "删除失败");
                      }
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingId ? "编辑分类" : "新建分类"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>标识</Label>
                <Input
                  value={form.key}
                  onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                  placeholder="payment"
                />
              </div>
              <div className="space-y-1">
                <Label>名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="支付问题"
                />
              </div>
            </div>
            <div className="space-y-1">
              <Label>描述</Label>
              <Textarea
                rows={2}
                value={form.description}
                onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1">
                <Label>默认优先级</Label>
                <Select
                  value={form.defaultPriority}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, defaultPriority: value as TicketPriority }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PRIORITIES.map((priority) => (
                      <SelectItem key={priority} value={priority}>
                        {PRIORITY_LABEL[priority]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label>默认处理组</Label>
                <Select
                  value={form.defaultGroupId}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, defaultGroupId: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">不指定</SelectItem>
                    {(groupsQuery.data ?? []).map((group) => (
                      <SelectItem key={group.id} value={String(group.id)}>
                        {group.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label>SLA 策略</Label>
                <Select
                  value={form.slaPolicyId}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, slaPolicyId: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">不指定</SelectItem>
                    {(slaQuery.data ?? []).map((policy) => (
                      <SelectItem key={policy.id} value={String(policy.id)}>
                        {policy.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.userSubmittable}
                  onCheckedChange={(value) => setForm((prev) => ({ ...prev, userSubmittable: value }))}
                />
                <Label className="text-xs">允许用户自助提交</Label>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
                />
                <Label className="text-xs">启用</Label>
              </div>
              <div className="flex items-center gap-2">
                <Label className="text-xs">排序</Label>
                <Input
                  type="number"
                  className="h-8 w-20"
                  value={form.sort}
                  onChange={(event) => setForm((prev) => ({ ...prev, sort: Number(event.target.value) }))}
                />
              </div>
            </div>
            <Button className="w-full" disabled={saveMut.isPending} onClick={handleSave}>
              <Save className="mr-1 size-3.5" />
              保存
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─────────────── 处理组 ───────────────

function GroupsSection({ appId }: { appId: number }) {
  const query = useTicketGroupsQuery(appId);
  const saveMut = useSaveTicketGroupMutation();
  const deleteMut = useDeleteTicketGroupMutation();

  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | undefined>();
  const [membersFor, setMembersFor] = useState<TicketGroup | null>(null);
  const [form, setForm] = useState({
    key: "",
    name: "",
    description: "",
    assignStrategy: "manual" as "manual" | "round_robin" | "least_open",
    enabled: true
  });

  const handleSave = async () => {
    if (!form.key.trim() || !form.name.trim()) {
      notify.warning("请填写处理组标识与名称");
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editingId,
        payload: {
          appid: appId,
          key: form.key.trim(),
          name: form.name.trim(),
          description: form.description.trim(),
          assignStrategy: form.assignStrategy,
          enabled: form.enabled
        }
      });
      notify.success("处理组已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const items = query.data ?? [];

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          把「特定人员」编进处理组：组员无需全局工单权限，也能处理指派到本组的工单
        </p>
        <Button
          size="sm"
          onClick={() => {
            setEditingId(undefined);
            setForm({ key: "", name: "", description: "", assignStrategy: "manual", enabled: true });
            setOpen(true);
          }}
        >
          <Plus className="mr-1 size-3.5" />
          新建处理组
        </Button>
      </div>

      {items.length === 0 ? (
        <EmptyState title="暂无处理组" description="创建处理组后即可把工单指派给一组人，并按策略自动分派。" />
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {items.map((group) => (
            <Card key={group.id}>
              <CardContent className="space-y-2 p-4">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-sm font-medium text-foreground">{group.name}</span>
                      <Badge variant="outline" size="sm" className="font-mono text-[10px]">
                        {group.key}
                      </Badge>
                      {group.appid === 0 ? (
                        <Badge variant="secondary" size="sm">
                          平台级
                        </Badge>
                      ) : null}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{group.description || "无描述"}</p>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button size="sm" variant="outline" onClick={() => setMembersFor(group)}>
                      <UserCog className="mr-1 size-3.5" />
                      成员
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditingId(group.id);
                        setForm({
                          key: group.key,
                          name: group.name,
                          description: group.description,
                          assignStrategy: group.assignStrategy,
                          enabled: group.enabled
                        });
                        setOpen(true);
                      }}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive"
                      onClick={async () => {
                        try {
                          await deleteMut.mutateAsync(group.id);
                          notify.success("处理组已删除");
                        } catch (error) {
                          reportError(error, "删除失败");
                        }
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                  <span>{group.memberCount} 名成员</span>
                  <span>·</span>
                  <span>待办 {group.openCount}</span>
                  <span>·</span>
                  <span>
                    分派策略：
                    {group.assignStrategy === "round_robin"
                      ? "轮询"
                      : group.assignStrategy === "least_open"
                        ? "最少待办优先"
                        : "手动"}
                  </span>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingId ? "编辑处理组" : "新建处理组"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>标识</Label>
                <Input
                  value={form.key}
                  onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                  placeholder="payment_support"
                />
              </div>
              <div className="space-y-1">
                <Label>名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="支付支持组"
                />
              </div>
            </div>
            <div className="space-y-1">
              <Label>描述</Label>
              <Textarea
                rows={2}
                value={form.description}
                onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <Label>自动分派策略</Label>
              <Select
                value={form.assignStrategy}
                onValueChange={(value) =>
                  setForm((prev) => ({ ...prev, assignStrategy: value as typeof prev.assignStrategy }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">手动指派</SelectItem>
                  <SelectItem value="round_robin">轮询分派</SelectItem>
                  <SelectItem value="least_open">最少待办优先</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-[11px] text-muted-foreground">
                非手动策略下，新工单落到本组时会自动挑一名成员作为受理人
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Switch
                checked={form.enabled}
                onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
              />
              <Label className="text-xs">启用</Label>
            </div>
            <Button className="w-full" disabled={saveMut.isPending} onClick={handleSave}>
              <Save className="mr-1 size-3.5" />
              保存
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <GroupMembersDialog group={membersFor} onClose={() => setMembersFor(null)} />
    </div>
  );
}

function GroupMembersDialog({ group, onClose }: { group: TicketGroup | null; onClose: () => void }) {
  const admins = useAdminOptions();
  const setMembersMut = useSetTicketGroupMembersMutation();
  const [selected, setSelected] = useState<Record<number, "agent" | "leader">>({});
  const [initialised, setInitialised] = useState<number | null>(null);

  // 打开不同的组时用该组现有成员初始化一次
  if (group && initialised !== group.id) {
    const next: Record<number, "agent" | "leader"> = {};
    for (const member of group.members ?? []) next[member.adminId] = member.role;
    setSelected(next);
    setInitialised(group.id);
  }

  const handleSave = async () => {
    if (!group) return;
    try {
      await setMembersMut.mutateAsync({
        id: group.id,
        members: Object.entries(selected).map(([adminId, role]) => ({ adminId: Number(adminId), role }))
      });
      notify.success("组成员已更新");
      onClose();
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  return (
    <Dialog open={Boolean(group)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{group?.name} · 成员</DialogTitle>
        </DialogHeader>
        <p className="text-xs text-muted-foreground">
          组负责人（leader）可以处理并转派组内全部工单；普通成员只能处理指派给自己或本组的工单。
        </p>
        <ScrollArea className="max-h-80">
          <div className="space-y-1 pr-3">
            {admins.map((admin) => {
              const id = admin.id;
              const checked = id in selected;
              return (
                <div key={id} className="flex items-center gap-2 rounded-lg border px-3 py-2">
                  <Checkbox
                    checked={checked}
                    onCheckedChange={(value) =>
                      setSelected((prev) => {
                        const next = { ...prev };
                        if (value) next[id] = "agent";
                        else delete next[id];
                        return next;
                      })
                    }
                  />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm text-foreground">{admin.name}</p>
                    <p className="truncate text-[11px] text-muted-foreground">{admin.account}</p>
                  </div>
                  {checked ? (
                    <Select
                      value={selected[id]}
                      onValueChange={(value) =>
                        setSelected((prev) => ({ ...prev, [id]: value as "agent" | "leader" }))
                      }
                    >
                      <SelectTrigger className="h-7 w-24 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="agent">成员</SelectItem>
                        <SelectItem value="leader">负责人</SelectItem>
                      </SelectContent>
                    </Select>
                  ) : null}
                </div>
              );
            })}
          </div>
        </ScrollArea>
        <Button disabled={setMembersMut.isPending} onClick={handleSave}>
          <Save className="mr-1 size-3.5" />
          保存成员
        </Button>
      </DialogContent>
    </Dialog>
  );
}

// ─────────────── SLA ───────────────

function SLASection({ appId }: { appId: number }) {
  const query = useTicketSLAPoliciesQuery(appId);
  const saveMut = useSaveTicketSLAPolicyMutation();
  const deleteMut = useDeleteTicketSLAPolicyMutation();

  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | undefined>();
  const [form, setForm] = useState({
    name: "",
    description: "",
    first: { urgent: 30, high: 120, normal: 480, low: 1440 } as Record<string, number>,
    resolve: { urgent: 240, high: 720, normal: 2880, low: 7200 } as Record<string, number>,
    warnRatio: 0.8,
    businessHoursEnabled: false,
    timezone: "Asia/Shanghai",
    start: "09:00",
    end: "18:00",
    days: [1, 2, 3, 4, 5] as number[],
    enabled: true
  });

  const handleSave = async () => {
    if (!form.name.trim()) {
      notify.warning("请填写策略名称");
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editingId,
        payload: {
          appid: appId,
          name: form.name.trim(),
          description: form.description.trim(),
          firstResponseMinutes: form.first,
          resolveMinutes: form.resolve,
          warnRatio: form.warnRatio,
          businessHours: form.businessHoursEnabled
            ? { timezone: form.timezone, days: form.days, start: form.start, end: form.end }
            : null,
          enabled: form.enabled
        }
      });
      notify.success("SLA 策略已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const items = query.data ?? [];

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          按优先级设置首响与解决时限；Worker 每分钟巡检，接近或超出时限时通过统一通知出口告警
        </p>
        <Button
          size="sm"
          onClick={() => {
            setEditingId(undefined);
            setOpen(true);
          }}
        >
          <Plus className="mr-1 size-3.5" />
          新建策略
        </Button>
      </div>

      {items.length === 0 ? (
        <EmptyState title="暂无 SLA 策略" description="创建策略并绑定到分类，工单就会自动带上响应与解决时限。" />
      ) : (
        <div className="space-y-2">
          {items.map((policy) => (
            <Card key={policy.id}>
              <CardContent className="space-y-2 p-4">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-sm font-medium text-foreground">{policy.name}</span>
                      {policy.appid === 0 ? (
                        <Badge variant="secondary" size="sm">
                          平台级
                        </Badge>
                      ) : null}
                      {policy.businessHours ? (
                        <Badge variant="info" size="sm">
                          仅工作时间计时
                        </Badge>
                      ) : (
                        <Badge variant="outline" size="sm">
                          7×24 计时
                        </Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground">{policy.description || "无描述"}</p>
                  </div>
                  <div className="flex gap-1">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditingId(policy.id);
                        setForm({
                          name: policy.name,
                          description: policy.description,
                          first: { ...policy.firstResponseMinutes },
                          resolve: { ...policy.resolveMinutes },
                          warnRatio: policy.warnRatio,
                          businessHoursEnabled: Boolean(policy.businessHours),
                          timezone: policy.businessHours?.timezone || "Asia/Shanghai",
                          start: policy.businessHours?.start || "09:00",
                          end: policy.businessHours?.end || "18:00",
                          days: policy.businessHours?.days || [1, 2, 3, 4, 5],
                          enabled: policy.enabled
                        });
                        setOpen(true);
                      }}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive"
                      onClick={async () => {
                        try {
                          await deleteMut.mutateAsync(policy.id);
                          notify.success("策略已删除");
                        } catch (error) {
                          reportError(error, "删除失败");
                        }
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="grid gap-1 text-[11px] text-muted-foreground sm:grid-cols-4">
                  {PRIORITIES.map((priority) => (
                    <div key={priority}>
                      {PRIORITY_LABEL[priority]}：首响 {policy.firstResponseMinutes[priority] ?? "—"} 分 / 解决{" "}
                      {policy.resolveMinutes[priority] ?? "—"} 分
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editingId ? "编辑 SLA 策略" : "新建 SLA 策略"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>名称</Label>
              <Input
                value={form.name}
                onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="标准 SLA"
              />
            </div>
            <div className="space-y-1">
              <Label>描述</Label>
              <Textarea
                rows={2}
                value={form.description}
                onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
              />
            </div>
            <div className="space-y-2">
              <Label>时限（分钟）</Label>
              <div className="grid gap-2 sm:grid-cols-2">
                {PRIORITIES.map((priority) => (
                  <div key={priority} className="flex items-center gap-2 rounded-lg border px-3 py-2">
                    <span className="w-10 text-xs text-muted-foreground">{PRIORITY_LABEL[priority]}</span>
                    <Input
                      type="number"
                      className="h-8"
                      value={form.first[priority] ?? 0}
                      onChange={(event) =>
                        setForm((prev) => ({
                          ...prev,
                          first: { ...prev.first, [priority]: Number(event.target.value) }
                        }))
                      }
                      placeholder="首响"
                    />
                    <Input
                      type="number"
                      className="h-8"
                      value={form.resolve[priority] ?? 0}
                      onChange={(event) =>
                        setForm((prev) => ({
                          ...prev,
                          resolve: { ...prev.resolve, [priority]: Number(event.target.value) }
                        }))
                      }
                      placeholder="解决"
                    />
                  </div>
                ))}
              </div>
              <p className="text-[11px] text-muted-foreground">左侧为首次响应时限，右侧为解决时限</p>
            </div>
            <div className="flex items-center gap-3">
              <Label className="text-xs">预警阈值</Label>
              <Input
                type="number"
                step="0.05"
                min="0.1"
                max="0.95"
                className="h-8 w-24"
                value={form.warnRatio}
                onChange={(event) => setForm((prev) => ({ ...prev, warnRatio: Number(event.target.value) }))}
              />
              <span className="text-[11px] text-muted-foreground">消耗掉该比例时限后触发 SLA 预警</span>
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.businessHoursEnabled}
                  onCheckedChange={(value) => setForm((prev) => ({ ...prev, businessHoursEnabled: value }))}
                />
                <Label className="text-xs">仅在工作时间内计时</Label>
              </div>
              {form.businessHoursEnabled ? (
                <div className="grid gap-2 sm:grid-cols-3">
                  <div className="space-y-1">
                    <Label className="text-xs">时区</Label>
                    <Input
                      value={form.timezone}
                      onChange={(event) => setForm((prev) => ({ ...prev, timezone: event.target.value }))}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">开始</Label>
                    <Input
                      value={form.start}
                      onChange={(event) => setForm((prev) => ({ ...prev, start: event.target.value }))}
                      placeholder="09:00"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">结束</Label>
                    <Input
                      value={form.end}
                      onChange={(event) => setForm((prev) => ({ ...prev, end: event.target.value }))}
                      placeholder="18:00"
                    />
                  </div>
                  <div className="sm:col-span-3">
                    <Label className="text-xs">工作日</Label>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {[1, 2, 3, 4, 5, 6, 7].map((day) => (
                        <Button
                          key={day}
                          size="sm"
                          variant={form.days.includes(day) ? "default" : "outline"}
                          className="h-7 w-9 p-0 text-xs"
                          onClick={() =>
                            setForm((prev) => ({
                              ...prev,
                              days: prev.days.includes(day)
                                ? prev.days.filter((item) => item !== day)
                                : [...prev.days, day].sort()
                            }))
                          }
                        >
                          {["一", "二", "三", "四", "五", "六", "日"][day - 1]}
                        </Button>
                      ))}
                    </div>
                  </div>
                </div>
              ) : null}
            </div>
            <Button className="w-full" disabled={saveMut.isPending} onClick={handleSave}>
              <Save className="mr-1 size-3.5" />
              保存
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─────────────── 快捷回复 ───────────────

function QuickRepliesSection({ appId }: { appId: number }) {
  const query = useTicketQuickRepliesQuery(appId);
  const saveMut = useSaveTicketQuickReplyMutation();
  const deleteMut = useDeleteTicketQuickReplyMutation();

  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [isPrivate, setIsPrivate] = useState(false);

  const items = useMemo(() => query.data ?? [], [query.data]);

  const handleCreate = async () => {
    if (!title.trim() || !content.trim()) {
      notify.warning("请填写标题与内容");
      return;
    }
    try {
      await saveMut.mutateAsync({
        payload: { appid: appId, title: title.trim(), content: content.trim(), private: isPrivate }
      });
      setTitle("");
      setContent("");
      notify.success("快捷回复已保存");
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  return (
    <div className="space-y-3">
      <Card>
        <CardContent className="space-y-2 p-4">
          <div className="grid gap-2 sm:grid-cols-[200px_1fr]">
            <Input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="话术标题" />
            <Textarea
              rows={2}
              value={content}
              onChange={(event) => setContent(event.target.value)}
              placeholder="回复内容"
            />
          </div>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <Switch checked={isPrivate} onCheckedChange={setIsPrivate} />
              <Label className="text-xs">仅自己可见</Label>
            </div>
            <Button size="sm" className="ml-auto" disabled={saveMut.isPending} onClick={handleCreate}>
              <Plus className="mr-1 size-3.5" />
              添加
            </Button>
          </div>
        </CardContent>
      </Card>

      {items.length === 0 ? (
        <EmptyState title="暂无快捷回复" description="常用话术存下来，回复工单时一键插入。" />
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {items.map((item) => (
            <Card key={item.id}>
              <CardContent className="flex items-start justify-between gap-2 p-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium text-foreground">{item.title}</span>
                    {item.ownerAdminId ? (
                      <Badge variant="secondary" size="sm">
                        私人
                      </Badge>
                    ) : null}
                    <span className="text-[11px] text-muted-foreground">用过 {item.usageCount} 次</span>
                  </div>
                  <p className="line-clamp-2 text-xs text-muted-foreground">{item.content}</p>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  className="shrink-0 text-destructive"
                  onClick={async () => {
                    try {
                      await deleteMut.mutateAsync({ id: item.id, appid: appId });
                      notify.success("已删除");
                    } catch (error) {
                      reportError(error, "删除失败");
                    }
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
