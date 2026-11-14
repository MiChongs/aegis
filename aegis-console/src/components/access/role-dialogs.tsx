"use client";

import { useState } from "react";
import { AppWindow, Layers } from "lucide-react";
import { toast } from "sonner";
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
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  useCopyRoleMutation,
  useCreateRoleMutation,
  useDeleteRoleMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import type { AccessRole } from "@/lib/api/access";
import { cn } from "@/lib/utils";

const KEY_PATTERN = /^[a-z0-9_]{2,56}$/;
const BLANK = "__blank__";

/** 权限字符：固定前缀 custom_，只填后半段 */
function KeyField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex h-9 items-center overflow-hidden rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring/50">
      <span className="flex h-full items-center border-r bg-muted/50 px-2.5 font-mono text-xs text-muted-foreground">
        custom_
      </span>
      <input
        value={value}
        onChange={(e) =>
          onChange(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, "_"))
        }
        className="h-full min-w-0 flex-1 bg-transparent px-2.5 font-mono text-sm outline-none"
        placeholder="lottery_ops"
        maxLength={56}
      />
    </div>
  );
}

type CreateProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  roles: AccessRole[];
  onCreated: (roleKey: string) => void;
};

/** 表单只在打开时挂载：每次打开都是一份新的空表单，不用 effect 去重置。 */
export function RoleCreateDialog(props: CreateProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {props.open && <RoleCreateForm {...props} />}
      </DialogContent>
    </Dialog>
  );
}

function RoleCreateForm({ onOpenChange, roles, onCreated }: CreateProps) {
  const createMutation = useCreateRoleMutation();
  const copyMutation = useCopyRoleMutation();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [scope, setScope] = useState<"app" | "global">("app");
  const [template, setTemplate] = useState(BLANK);
  const [description, setDescription] = useState("");

  const templates = roles.filter(
    (role) => role.key !== "super_admin" && role.scope === scope,
  );
  const valid = name.trim() !== "" && KEY_PATTERN.test(key);
  const pending = createMutation.isPending || copyMutation.isPending;

  const submit = async () => {
    const roleKey = `custom_${key}`;
    try {
      if (template !== BLANK) {
        await copyMutation.mutateAsync({
          source: template,
          roleKey,
          name: name.trim(),
        });
      } else {
        await createMutation.mutateAsync({
          roleKey,
          name: name.trim(),
          description: description.trim(),
          level: 10,
          scope,
          permissions: [],
          deny: [],
        });
      }
      toast.success(`已创建「${name.trim()}」`);
      onOpenChange(false);
      onCreated(roleKey);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "创建失败");
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>新建角色</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          {(
            [
              { value: "app", title: "应用级", icon: AppWindow },
              { value: "global", title: "平台级", icon: Layers },
            ] as const
          ).map((option) => {
            const Icon = option.icon;
            const active = scope === option.value;
            return (
              <button
                key={option.value}
                type="button"
                onClick={() => {
                  setScope(option.value);
                  setTemplate(BLANK);
                }}
                className={cn(
                  "flex items-center gap-3 rounded-xl border p-3 text-left transition-colors",
                  active
                    ? "border-primary/50 bg-primary/6 ring-1 ring-primary/30"
                    : "hover:bg-accent/50",
                )}
              >
                <span
                  className={cn(
                    "grid size-8 place-items-center rounded-lg",
                    active
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted text-muted-foreground",
                  )}
                >
                  <Icon className="size-4" />
                </span>
                <span className="text-sm font-medium">{option.title}</span>
              </button>
            );
          })}
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="new-role-name">名称</Label>
            <Input
              id="new-role-name"
              value={name}
              maxLength={64}
              onChange={(e) => setName(e.target.value)}
              placeholder="抽奖运营"
            />
          </div>
          <div className="space-y-1.5">
            <Label>权限字符</Label>
            <KeyField value={key} onChange={setKey} />
          </div>
        </div>
        <div className="space-y-1.5">
          <Label>权限模板</Label>
          <Select value={template} onValueChange={setTemplate}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={BLANK}>空白角色</SelectItem>
              {templates.map((role) => (
                <SelectItem key={role.key} value={role.key}>
                  复制「{role.name}」的权限
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {template === BLANK && (
          <div className="space-y-1.5">
            <Label htmlFor="new-role-desc">说明</Label>
            <Textarea
              id="new-role-desc"
              rows={2}
              value={description}
              maxLength={300}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
        )}
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => onOpenChange(false)}>
          取消
        </Button>
        <Button onClick={submit} disabled={!valid || pending}>
          {pending ? "创建中" : "创建"}
        </Button>
      </DialogFooter>
    </>
  );
}

type CopyProps = {
  source: AccessRole | null;
  onOpenChange: (open: boolean) => void;
  onCreated: (roleKey: string) => void;
};

export function RoleCopyDialog(props: CopyProps) {
  return (
    <Dialog open={Boolean(props.source)} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {props.source && (
          <RoleCopyForm
            key={props.source.key}
            {...props}
            source={props.source}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function RoleCopyForm({
  source,
  onOpenChange,
  onCreated,
}: CopyProps & { source: AccessRole }) {
  const mutation = useCopyRoleMutation();
  const [name, setName] = useState(`${source.name}（副本）`);
  const [key, setKey] = useState(
    `${source.key.replace(/^custom_/, "")}_copy`.slice(0, 56),
  );

  const submit = async () => {
    const roleKey = `custom_${key}`;
    try {
      await mutation.mutateAsync({
        source: source.key,
        roleKey,
        name: name.trim(),
      });
      toast.success(`已复制为「${name.trim()}」`);
      onOpenChange(false);
      onCreated(roleKey);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "复制失败");
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>复制「{source.name}」</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="copy-role-name">名称</Label>
          <Input
            id="copy-role-name"
            value={name}
            maxLength={64}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label>权限字符</Label>
          <KeyField value={key} onChange={setKey} />
        </div>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => onOpenChange(false)}>
          取消
        </Button>
        <Button
          onClick={submit}
          disabled={
            !name.trim() || !KEY_PATTERN.test(key) || mutation.isPending
          }
        >
          复制
        </Button>
      </DialogFooter>
    </>
  );
}

export function RoleDeleteDialog({
  role,
  onOpenChange,
}: {
  role: AccessRole | null;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <RoleDeleteConfirm
      key={role?.key ?? "none"}
      role={role}
      onOpenChange={onOpenChange}
    />
  );
}

function RoleDeleteConfirm({
  role,
  onOpenChange,
}: {
  role: AccessRole | null;
  onOpenChange: (open: boolean) => void;
}) {
  const mutation = useDeleteRoleMutation();
  const [force, setForce] = useState(false);
  const hasMembers = (role?.memberCount ?? 0) > 0;

  const submit = async () => {
    if (!role) return;
    try {
      await mutation.mutateAsync({
        roleKey: role.key,
        force: hasMembers && force,
      });
      toast.success(`已删除「${role.name}」`);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  };

  return (
    <AlertDialog open={Boolean(role)} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除「{role?.name}」</AlertDialogTitle>
          <AlertDialogDescription>
            {hasMembers
              ? `${role?.memberCount} 位管理员持有该角色，删除后立即失去这些权限。`
              : "删除后不可恢复。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {hasMembers && (
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={force}
              onCheckedChange={(v) => setForce(v === true)}
            />
            同时移除全部成员授权
          </label>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-white hover:bg-destructive/90"
            disabled={(hasMembers && !force) || mutation.isPending}
            onClick={(event) => {
              event.preventDefault();
              void submit();
            }}
          >
            删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
