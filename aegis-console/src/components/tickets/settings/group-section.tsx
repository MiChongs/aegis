"use client";

import { useMemo, useState } from "react";
import { Crown, Pencil, Plus, Save, UserPlus, Users, X } from "lucide-react";
import { notify } from "@/lib/notify";
import {
  useAdminOptions,
  useDeleteTicketGroupMutation,
  useSaveTicketGroupMutation,
  useSetTicketGroupMembersMutation,
  useTicketGroupsQuery
} from "@/lib/ticket-hooks";
import type { TicketGroup, TicketGroupMember } from "@/lib/api/tickets";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { PersonAvatar } from "../ticket-shared";
import {
  DeleteButton,
  EntityList,
  Field,
  ListSkeleton,
  STRATEGY_HINT,
  STRATEGY_LABEL,
  ScopeBadges,
  SectionEmpty,
  SectionError,
  SectionHeader,
  SwitchRow,
  reportError,
  type AssignStrategy
} from "./settings-shared";
import { cn } from "@/lib/utils";

// 处理组：谁来接、怎么分。列表行里直接看到成员头像、组长与当前未结数；
// 编辑组信息与调整成员分开两个抽屉——改成员是高频操作，不该被一整张表单挡着。

type GroupForm = {
  key: string;
  name: string;
  description: string;
  assignStrategy: AssignStrategy;
  enabled: boolean;
};

const EMPTY_GROUP: GroupForm = { key: "", name: "", description: "", assignStrategy: "manual", enabled: true };

export function GroupSection({ appId }: { appId: number }) {
  const query = useTicketGroupsQuery(appId);
  const saveMut = useSaveTicketGroupMutation();
  const deleteMut = useDeleteTicketGroupMutation();
  const [editing, setEditing] = useState<{ id?: number; form: GroupForm } | null>(null);
  const [membersFor, setMembersFor] = useState<TicketGroup | null>(null);

  const groups = query.data ?? [];

  const save = async (id: number | undefined, form: GroupForm) => {
    if (!form.key.trim() || !form.name.trim()) {
      notify.warning("请填写处理组标识与名称");
      return false;
    }
    try {
      await saveMut.mutateAsync({
        id,
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
      return true;
    } catch (error) {
      reportError(error, "保存失败");
      return false;
    }
  };

  const formOf = (group: TicketGroup): GroupForm => ({
    key: group.key,
    name: group.name,
    description: group.description,
    assignStrategy: group.assignStrategy,
    enabled: group.enabled
  });

  return (
    <div className="space-y-4">
      <SectionHeader
        title="处理组"
        description="按业务把处理人分组，分类可以把新工单直接送进某个组。"
        action={
          <Button size="sm" onClick={() => setEditing({ form: EMPTY_GROUP })}>
            <Plus />
            新建处理组
          </Button>
        }
      />

      {query.isLoading ? (
        <ListSkeleton rows={3} />
      ) : query.isError ? (
        <SectionError error={query.error} onRetry={() => void query.refetch()} />
      ) : groups.length === 0 ? (
        <SectionEmpty
          icon={Users}
          title="还没有处理组"
          description="没有处理组时，新工单只能逐条手动指派。"
          action={
            <Button size="sm" onClick={() => setEditing({ form: EMPTY_GROUP })}>
              <Plus />
              新建处理组
            </Button>
          }
        />
      ) : (
        <EntityList>
          {groups.map((group) => {
            const members = group.members ?? [];
            const leaders = members.filter((member) => member.role === "leader");
            return (
              <div key={group.id} className={cn("flex flex-wrap items-center gap-3 px-4 py-3", !group.enabled && "bg-muted/30")}>
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                  <Users className="size-4" />
                </div>
                <div className="min-w-0 flex-1 space-y-0.5">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-sm font-medium text-foreground">{group.name}</span>
                    <code className="rounded bg-muted px-1 py-px font-mono text-[10px] text-muted-foreground">{group.key}</code>
                    <Badge variant="outline" size="sm">
                      {STRATEGY_LABEL[group.assignStrategy]}
                    </Badge>
                    <ScopeBadges platform={group.appid === 0 && appId !== 0} enabled={group.enabled} />
                  </div>
                  <p className="truncate text-xs text-muted-foreground">
                    {[
                      `${group.memberCount} 名成员`,
                      leaders.length ? `组长 ${leaders.map((leader) => leader.displayName || leader.account).join("、")}` : "未设组长",
                      group.description || null
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                </div>

                <button
                  type="button"
                  onClick={() => setMembersFor(group)}
                  className="flex items-center gap-2 rounded-lg px-2 py-1 transition-colors hover:bg-accent"
                  aria-label="管理成员"
                >
                  <MemberStack members={members} total={group.memberCount} />
                </button>

                <div className="w-16 text-right">
                  <p
                    className={cn(
                      "text-sm font-semibold tabular-nums",
                      group.openCount > 0 ? "text-foreground" : "text-muted-foreground"
                    )}
                  >
                    {group.openCount}
                  </p>
                  <p className="text-[10px] text-muted-foreground">未结</p>
                </div>

                <div className="flex items-center">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button size="icon-sm" variant="ghost" aria-label="成员" onClick={() => setMembersFor(group)}>
                        <UserPlus />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>成员</TooltipContent>
                  </Tooltip>
                  <Button size="icon-sm" variant="ghost" aria-label="编辑" onClick={() => setEditing({ id: group.id, form: formOf(group) })}>
                    <Pencil />
                  </Button>
                  <DeleteButton
                    title={`删除处理组「${group.name}」？`}
                    description="删除后不能再把工单分进这个组，此操作不可撤销。"
                    pending={deleteMut.isPending}
                    onConfirm={async () => {
                      try {
                        await deleteMut.mutateAsync(group.id);
                        notify.success("处理组已删除");
                      } catch (error) {
                        reportError(error, "删除失败");
                      }
                    }}
                  />
                </div>
              </div>
            );
          })}
        </EntityList>
      )}

      <GroupSheet
        key={editing ? `${editing.id ?? "new"}` : "closed"}
        editing={editing}
        pending={saveMut.isPending}
        onClose={() => setEditing(null)}
        onSave={async (form) => {
          if (await save(editing?.id, form)) setEditing(null);
        }}
      />
      <MembersSheet key={membersFor?.id ?? "closed"} group={membersFor} onClose={() => setMembersFor(null)} />
    </div>
  );
}

function MemberStack({ members, total }: { members: TicketGroupMember[]; total: number }) {
  if (total === 0) {
    return (
      <span className="flex items-center gap-1 text-xs text-muted-foreground">
        <UserPlus className="size-3.5" />
        添加成员
      </span>
    );
  }
  const shown = members.slice(0, 4);
  const rest = total - shown.length;
  return (
    <span className="flex -space-x-2">
      {shown.map((member) => (
        <PersonAvatar
          key={member.adminId}
          name={member.displayName || member.account}
          src={member.avatar}
          size="sm"
          className="ring-2 ring-card"
        />
      ))}
      {rest > 0 ? (
        <span className="relative flex size-6 items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-card">
          +{rest}
        </span>
      ) : null}
    </span>
  );
}

function GroupSheet({
  editing,
  pending,
  onClose,
  onSave
}: {
  editing: { id?: number; form: GroupForm } | null;
  pending: boolean;
  onClose: () => void;
  onSave: (form: GroupForm) => void;
}) {
  const [form, setForm] = useState<GroupForm>(() => editing?.form ?? EMPTY_GROUP);
  const patch = (next: Partial<GroupForm>) => setForm((prev) => ({ ...prev, ...next }));

  return (
    <Sheet open={Boolean(editing)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle>{editing?.id ? "编辑处理组" : "新建处理组"}</SheetTitle>
          <SheetDescription>成员在保存后从列表里的头像处调整。</SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-4 overflow-y-auto p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="名称">
              <Input value={form.name} onChange={(event) => patch({ name: event.target.value })} placeholder="支付支持组" />
            </Field>
            <Field label="标识">
              <Input
                value={form.key}
                onChange={(event) => patch({ key: event.target.value })}
                placeholder="payment_support"
                className="font-mono"
              />
            </Field>
          </div>
          <Field label="描述">
            <Textarea rows={2} value={form.description} onChange={(event) => patch({ description: event.target.value })} />
          </Field>
          <Field label="自动分派策略">
            <div className="space-y-1.5">
              {(Object.keys(STRATEGY_LABEL) as AssignStrategy[]).map((strategy) => (
                <button
                  key={strategy}
                  type="button"
                  onClick={() => patch({ assignStrategy: strategy })}
                  className={cn(
                    "flex w-full items-start gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors",
                    form.assignStrategy === strategy ? "border-primary bg-primary/5" : "hover:bg-accent"
                  )}
                >
                  <span
                    className={cn(
                      "mt-0.5 size-3.5 shrink-0 rounded-full border-2",
                      form.assignStrategy === strategy ? "border-primary bg-primary" : "border-muted-foreground/40"
                    )}
                  />
                  <span>
                    <span className="block text-sm text-foreground">{STRATEGY_LABEL[strategy]}</span>
                    <span className="block text-xs text-muted-foreground">{STRATEGY_HINT[strategy]}</span>
                  </span>
                </button>
              ))}
            </div>
          </Field>
          <SwitchRow
            title="启用"
            description="停用的组不再接收新工单"
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

type Role = "agent" | "leader";

function MembersSheet({ group, onClose }: { group: TicketGroup | null; onClose: () => void }) {
  const admins = useAdminOptions();
  const setMembersMut = useSetTicketGroupMembersMutation();
  const [members, setMembers] = useState<{ adminId: number; role: Role }[]>(() =>
    (group?.members ?? []).map((member) => ({ adminId: member.adminId, role: member.role }))
  );
  const [pickerOpen, setPickerOpen] = useState(false);

  const existing = useMemo(
    () => new Map((group?.members ?? []).map((member) => [member.adminId, member])),
    [group?.members]
  );
  const adminById = useMemo(() => new Map(admins.map((admin) => [admin.id, admin])), [admins]);
  const selectedIds = new Set(members.map((member) => member.adminId));
  const candidates = admins.filter((admin) => !selectedIds.has(admin.id));

  const ordered = [...members].sort((a, b) => (a.role === b.role ? 0 : a.role === "leader" ? -1 : 1));
  const dirty =
    members.length !== existing.size ||
    members.some((member) => existing.get(member.adminId)?.role !== member.role);

  const handleSave = async () => {
    if (!group) return;
    try {
      await setMembersMut.mutateAsync({ id: group.id, members });
      notify.success("组成员已更新");
      onClose();
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  return (
    <Sheet open={Boolean(group)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle>{group?.name} · 成员</SheetTitle>
          <SheetDescription>组长可以改派组内工单；成员只处理指派给自己的。</SheetDescription>
        </SheetHeader>

        <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
          <span className="text-xs text-muted-foreground">
            {members.length} 名成员 · {members.filter((member) => member.role === "leader").length} 名组长
          </span>
          <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
            <PopoverTrigger asChild>
              <Button size="sm" variant="outline">
                <UserPlus />
                添加成员
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-72 p-0">
              <Command>
                <CommandInput placeholder="搜索管理员" />
                <CommandList>
                  <CommandEmpty>没有可添加的管理员</CommandEmpty>
                  <CommandGroup>
                    {candidates.map((admin) => (
                      <CommandItem
                        key={admin.id}
                        value={`${admin.name} ${admin.account}`}
                        onSelect={() => {
                          setMembers((prev) => [...prev, { adminId: admin.id, role: "agent" }]);
                          setPickerOpen(false);
                        }}
                      >
                        <PersonAvatar name={admin.name} size="sm" />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm">{admin.name}</span>
                          <span className="block truncate text-[11px] text-muted-foreground">{admin.account}</span>
                        </span>
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
        </div>

        <div className="flex-1 overflow-y-auto">
          {ordered.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-6 py-12 text-center text-sm text-muted-foreground">
              <Users className="size-6 opacity-60" />
              这个组还没有成员
            </div>
          ) : (
            <ul className="divide-y">
              {ordered.map((member) => {
                const info = existing.get(member.adminId);
                const admin = adminById.get(member.adminId);
                const name = info?.displayName || admin?.name || info?.account || `管理员 ${member.adminId}`;
                const account = info?.account || admin?.account;
                return (
                  <li key={member.adminId} className="flex items-center gap-3 px-4 py-2.5">
                    <PersonAvatar name={name} src={info?.avatar} />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-1 truncate text-sm text-foreground">
                        {name}
                        {member.role === "leader" ? <Crown className="size-3 text-amber-500" /> : null}
                      </p>
                      <p className="truncate text-[11px] text-muted-foreground">
                        {[account, info ? `未结 ${info.openCount}` : "新加入"].filter(Boolean).join(" · ")}
                      </p>
                    </div>
                    <ToggleGroup
                      type="single"
                      size="sm"
                      variant="outline"
                      value={member.role}
                      onValueChange={(value) =>
                        value &&
                        setMembers((prev) =>
                          prev.map((item) => (item.adminId === member.adminId ? { ...item, role: value as Role } : item))
                        )
                      }
                    >
                      <ToggleGroupItem value="agent" className="h-7 px-2 text-xs">
                        成员
                      </ToggleGroupItem>
                      <ToggleGroupItem value="leader" className="h-7 px-2 text-xs">
                        组长
                      </ToggleGroupItem>
                    </ToggleGroup>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label="移出"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setMembers((prev) => prev.filter((item) => item.adminId !== member.adminId))}
                    >
                      <X />
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <SheetFooter className="flex-row justify-end gap-2 border-t">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={setMembersMut.isPending || !dirty} onClick={handleSave}>
            <Save />
            保存成员
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
