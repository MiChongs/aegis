"use client";

import { useState } from "react";
import { FileText, Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAccessRolesQuery, useUpdateRoleMutation } from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { DATA_SCOPE_LABEL, type AccessRole } from "@/lib/api/access";
import { formatDateTime } from "@/lib/datetime";
import { CodeText, Pane } from "./access-kit";

const NO_BASE = "__none__";

/** 角色「基本信息」页签：自定义角色可编辑名称、说明、级别、排序、继承与状态；内置角色只读。 */
export function RoleInfoTab({
  role,
  canEdit,
}: {
  role: AccessRole;
  canEdit: boolean;
}) {
  const rolesQuery = useAccessRolesQuery();
  const mutation = useUpdateRoleMutation();
  const editable = canEdit && role.isCustom;
  const [form, setForm] = useState({
    name: role.name,
    description: role.description ?? "",
    level: role.level,
    sortOrder: role.sortOrder ?? 0,
    baseRole: role.baseRole ?? "",
    active: role.status !== "disabled",
  });
  const dirty =
    form.name !== role.name ||
    form.description !== (role.description ?? "") ||
    form.level !== role.level ||
    form.sortOrder !== (role.sortOrder ?? 0) ||
    form.baseRole !== (role.baseRole ?? "") ||
    form.active !== (role.status !== "disabled");

  const parents = (rolesQuery.data ?? []).filter(
    (item) =>
      item.key !== role.key &&
      item.key !== "super_admin" &&
      item.scope === role.scope,
  );

  const save = async () => {
    if (!form.name.trim()) {
      toast.error("请填写角色名称");
      return;
    }
    try {
      await mutation.mutateAsync({
        roleKey: role.key,
        name: form.name.trim(),
        description: form.description.trim(),
        level: form.level,
        sortOrder: form.sortOrder,
        baseRole: form.baseRole,
        status: form.active ? "active" : "disabled",
        permissions: role.allow,
        deny: role.deny,
        dataScope: role.dataScope,
        dataAppIds: role.dataAppIds,
      });
      toast.success("角色信息已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  if (!editable) {
    return (
      <Pane title="角色信息" icon={FileText}>
        <dl className="grid gap-x-8 gap-y-4 p-5 text-sm sm:grid-cols-2">
          <Fact label="名称">{role.name}</Fact>
          <Fact label="权限字符">
            <CodeText className="text-sm text-foreground">{role.key}</CodeText>
          </Fact>
          <Fact label="类型">{role.isCustom ? "自定义角色" : "内置角色"}</Fact>
          <Fact label="作用域">
            {role.scope === "global" ? "平台级" : "应用级"}
          </Fact>
          <Fact label="级别">{role.level}</Fact>
          <Fact label="数据权限">
            {role.scope === "global"
              ? "全部"
              : DATA_SCOPE_LABEL[role.dataScope]}
          </Fact>
          <Fact label="状态">
            {role.status === "disabled" ? "已停用" : "启用"}
          </Fact>
          {role.updatedAt && (
            <Fact label="更新时间">{formatDateTime(role.updatedAt)}</Fact>
          )}
          <div className="sm:col-span-2">
            <Fact label="说明">{role.description || "—"}</Fact>
          </div>
        </dl>
      </Pane>
    );
  }

  return (
    <Pane
      title="角色信息"
      icon={FileText}
      aside={
        <Button
          size="sm"
          className="h-8 gap-1.5"
          disabled={!dirty || mutation.isPending}
          onClick={save}
        >
          <Save className="size-3.5" />
          保存
        </Button>
      }
    >
      <div className="grid gap-5 p-5 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="role-name">名称</Label>
          <Input
            id="role-name"
            value={form.name}
            maxLength={64}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </div>
        <div className="space-y-1.5">
          <Label>权限字符</Label>
          <Input value={role.key} disabled className="font-mono text-xs" />
        </div>
        <div className="space-y-1.5 sm:col-span-2">
          <Label htmlFor="role-desc">说明</Label>
          <Textarea
            id="role-desc"
            rows={2}
            value={form.description}
            maxLength={300}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </div>
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <Label>级别</Label>
            <span className="text-sm font-semibold tabular-nums">
              {form.level}
            </span>
          </div>
          <Slider
            min={1}
            max={19}
            step={1}
            value={[form.level]}
            onValueChange={([value]) =>
              setForm({ ...form, level: value ?? form.level })
            }
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="role-sort">排序</Label>
          <Input
            id="role-sort"
            type="number"
            min={0}
            max={9999}
            value={form.sortOrder}
            onChange={(e) =>
              setForm({ ...form, sortOrder: Number(e.target.value) || 0 })
            }
          />
        </div>
        <div className="space-y-1.5">
          <Label>继承角色</Label>
          <Select
            value={form.baseRole || NO_BASE}
            onValueChange={(value) =>
              setForm({ ...form, baseRole: value === NO_BASE ? "" : value })
            }
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_BASE}>不继承</SelectItem>
              {parents.map((item) => (
                <SelectItem key={item.key} value={item.key}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center justify-between rounded-xl border px-4 py-3">
          <div>
            <div className="text-sm font-medium">启用</div>
            <div className="text-xs text-muted-foreground">
              停用后成员保留，权限不再生效
            </div>
          </div>
          <Switch
            checked={form.active}
            onCheckedChange={(active) => setForm({ ...form, active })}
          />
        </div>
      </div>
    </Pane>
  );
}

function Fact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 truncate">{children}</dd>
    </div>
  );
}
