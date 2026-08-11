"use client";

import { FormEvent, useState } from "react";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useCreateAdminAppMutation } from "@/lib/admin-hooks";
import type { AppSummary } from "@/lib/api/types";

type FormState = { name: string; status: string; registerStatus: string; loginStatus: string };
const defaultForm: FormState = { name: "", status: "enabled", registerStatus: "enabled", loginStatus: "enabled" };

/**
 * `onCreated` 拿到的是后端刚生成的应用（含 appKey）。
 * 列表页据此直接跳进新应用的配置页 —— 新建之后接下来必然是去配它，
 * 让人回到列表里再找一遍是白走一趟。
 */
export function AppCreateDialog({ onCreated }: { onCreated?: (app: AppSummary) => void }) {
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>(defaultForm);
  const [error, setError] = useState<string | null>(null);
  const mutation = useCreateAdminAppMutation();

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const created = await mutation.mutateAsync({
        name: form.name,
        status: form.status === "enabled",
        registerStatus: form.registerStatus === "enabled",
        loginStatus: form.loginStatus === "enabled"
      });
      setForm(defaultForm);
      setOpen(false);
      toast.success("应用已创建");
      if (created?.appKey) onCreated?.(created);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建失败");
    }
  }

  const set = (k: keyof FormState, v: string) => setForm((s) => ({ ...s, [k]: v }));

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" className="h-8 gap-1 text-xs"><Plus className="size-3.5" />新建应用</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle className="text-sm">新建应用</DialogTitle></DialogHeader>
        <form className="grid gap-3 sm:grid-cols-2" onSubmit={handleSubmit}>
          <div className="sm:col-span-2">
            <F label="应用名称" required><Input placeholder="我的应用" value={form.name} onChange={(e) => set("name", e.target.value)} required /></F>
          </div>
          <F label="应用状态"><StatusSelect value={form.status} onChange={(v) => set("status", v)} /></F>
          <F label="注册状态"><StatusSelect value={form.registerStatus} onChange={(v) => set("registerStatus", v)} labels={["开放", "关闭"]} /></F>
          <F label="登录状态"><StatusSelect value={form.loginStatus} onChange={(v) => set("loginStatus", v)} labels={["开放", "关闭"]} /></F>
          <div className="sm:col-span-2">
            <p className="text-[11px] text-muted-foreground">应用 ID 和 AppKey 将在创建后自动生成，AppKey 不可更改。</p>
          </div>
          {error && <p className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300 sm:col-span-2">{error}</p>}
          <DialogFooter className="sm:col-span-2">
            <Button size="sm" disabled={mutation.isPending} type="submit">{mutation.isPending ? "创建中..." : "创建"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function F({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}{required && <span className="text-destructive"> *</span>}</Label>{children}</div>;
}

function StatusSelect({ value, onChange, labels = ["启用", "停用"] }: { value: string; onChange: (v: string) => void; labels?: [string, string] }) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="text-sm"><SelectValue /></SelectTrigger>
      <SelectContent>
        <SelectItem value="enabled">{labels[0]}</SelectItem>
        <SelectItem value="disabled">{labels[1]}</SelectItem>
      </SelectContent>
    </Select>
  );
}
