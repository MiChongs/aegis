"use client";

import { useMemo, useState } from "react";
import { Dices, Eye, EyeOff } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
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
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useAccessRolesQuery,
  useCreateAccessAdminMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { TIER_LABEL, type AdminTier } from "@/lib/api/access";
import { useIsSystemAdmin } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { TIER_TONE } from "./access-kit";
import { AppPicker } from "./pickers";

const NONE = "__none__";

function generatePassword() {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
  const symbols = "!@#$%^&*";
  const bytes = new Uint32Array(16);
  crypto.getRandomValues(bytes);
  let out = "";
  for (let i = 0; i < 14; i++) out += alphabet[bytes[i]! % alphabet.length];
  return out + symbols[bytes[14]! % symbols.length] + String(bytes[15]! % 10);
}

/** 新增管理员。身份档位只有系统管理员能选超管 / 系统管理员；初始角色可选，之后在「角色授权」里补全。 */
type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (adminId: number) => void;
};

export function AdminCreateDialog(props: Props) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {props.open && <AdminCreateForm {...props} />}
      </DialogContent>
    </Dialog>
  );
}

function AdminCreateForm({ onOpenChange, onCreated }: Props) {
  const isSystem = useIsSystemAdmin();
  const rolesQuery = useAccessRolesQuery();
  const mutation = useCreateAccessAdminMutation();
  const roles = useMemo(
    () =>
      (rolesQuery.data ?? []).filter(
        (r) => r.key !== "super_admin" && r.status !== "disabled",
      ),
    [rolesQuery.data],
  );

  const [form, setForm] = useState(() => ({
    account: "",
    displayName: "",
    email: "",
    password: generatePassword(),
  }));
  const [show, setShow] = useState(false);
  const [tier, setTier] = useState<AdminTier>("admin");
  const [roleKey, setRoleKey] = useState(NONE);
  const [app, setApp] = useState<number[]>([]);

  const role = roles.find((r) => r.key === roleKey);
  const needsApp = role?.scope === "app" && role.dataScope === "assigned";
  const valid =
    form.account.trim().length >= 3 &&
    form.password.length >= 8 &&
    (tier !== "admin" || roleKey === NONE || !needsApp || app.length === 1);

  const submit = async () => {
    try {
      const result = await mutation.mutateAsync({
        account: form.account.trim(),
        displayName: form.displayName.trim(),
        email: form.email.trim(),
        password: form.password,
        tier,
        assignments:
          tier === "admin" && roleKey !== NONE
            ? [{ roleKey, appid: needsApp ? (app[0] ?? null) : null }]
            : [],
      });
      toast.success(`已创建 ${form.account.trim()}`);
      onOpenChange(false);
      const id = (result as { account?: { id?: number } })?.account?.id;
      if (id) onCreated(id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "创建失败");
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>新增管理员</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="new-admin-account">账号</Label>
            <Input
              id="new-admin-account"
              value={form.account}
              maxLength={64}
              autoComplete="off"
              onChange={(e) => setForm({ ...form, account: e.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-admin-name">显示名称</Label>
            <Input
              id="new-admin-name"
              value={form.displayName}
              maxLength={64}
              onChange={(e) =>
                setForm({ ...form, displayName: e.target.value })
              }
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-admin-email">邮箱</Label>
            <Input
              id="new-admin-email"
              type="email"
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-admin-password">初始密码</Label>
            <div className="flex gap-1">
              <Input
                id="new-admin-password"
                type={show ? "text" : "password"}
                value={form.password}
                autoComplete="new-password"
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                className="font-mono"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-9 shrink-0"
                aria-label={show ? "隐藏" : "显示"}
                onClick={() => setShow(!show)}
              >
                {show ? (
                  <EyeOff className="size-4" />
                ) : (
                  <Eye className="size-4" />
                )}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-9 shrink-0"
                aria-label="重新生成"
                onClick={() =>
                  setForm({ ...form, password: generatePassword() })
                }
              >
                <Dices className="size-4" />
              </Button>
            </div>
          </div>
        </div>

        <div className="space-y-1.5">
          <Label>身份</Label>
          <div className="grid grid-cols-3 gap-2">
            {(["admin", "super", "system"] as const).map((value) => {
              const tone = TIER_TONE[value];
              const Icon = tone.icon;
              const disabled = value !== "admin" && !isSystem;
              return (
                <button
                  key={value}
                  type="button"
                  disabled={disabled}
                  onClick={() => setTier(value)}
                  className={cn(
                    "flex items-center gap-2 rounded-xl border px-3 py-2.5 text-left text-sm transition-colors",
                    tier === value
                      ? "border-primary/50 bg-primary/5 ring-1 ring-primary/30"
                      : "hover:bg-accent/50",
                    disabled && "cursor-not-allowed opacity-40",
                  )}
                >
                  <span
                    className={cn(
                      "grid size-7 place-items-center rounded-lg",
                      tone.plate,
                    )}
                  >
                    <Icon className="size-3.5" />
                  </span>
                  {TIER_LABEL[value]}
                </button>
              );
            })}
          </div>
        </div>

        {tier === "admin" && (
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>初始角色</Label>
              <Select value={roleKey} onValueChange={setRoleKey}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>暂不授权</SelectItem>
                  <SelectGroup>
                    <SelectLabel>平台级</SelectLabel>
                    {roles
                      .filter((r) => r.scope === "global")
                      .map((r) => (
                        <SelectItem key={r.key} value={r.key}>
                          {r.name}
                        </SelectItem>
                      ))}
                  </SelectGroup>
                  <SelectGroup>
                    <SelectLabel>应用级</SelectLabel>
                    {roles
                      .filter((r) => r.scope === "app")
                      .map((r) => (
                        <SelectItem key={r.key} value={r.key}>
                          {r.name}
                        </SelectItem>
                      ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            {needsApp && (
              <div className="space-y-1.5">
                <Label>生效应用</Label>
                <AppPicker value={app} onChange={setApp} />
              </div>
            )}
          </div>
        )}
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={() => onOpenChange(false)}>
          取消
        </Button>
        <Button onClick={submit} disabled={!valid || mutation.isPending}>
          {mutation.isPending ? "创建中" : "创建"}
        </Button>
      </DialogFooter>
    </>
  );
}
