"use client";

import { useState } from "react";
import { ChevronsUpDown, KeyRound, Timer } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useGrantTempPermissionMutation } from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { CodeText, Pane } from "./access-kit";
import { PermissionTreeEditor } from "./permission-tree-editor";
import { AppPicker } from "./pickers";
import { permissionPath, type TreeIndex } from "./tree-model";

/** 单选一个权限点：按钮显示已选项，点开是一棵可搜索的树。 */
export function PermissionPicker({
  index,
  value,
  onChange,
  placeholder = "选择权限点",
  className,
}: {
  index: TreeIndex;
  value: string | null;
  onChange: (code: string) => void;
  placeholder?: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className={cn(
          "h-9 w-full justify-between gap-2 px-3 font-normal",
          !value && "text-muted-foreground",
          className,
        )}
        onClick={() => setOpen(true)}
      >
        <span className="flex min-w-0 items-center gap-2">
          <KeyRound className="size-4 shrink-0 text-muted-foreground" />
          {value ? (
            <span className="min-w-0 truncate">
              {permissionPath(index, value).slice(-2).join(" / ")}
              <CodeText className="ml-2">{value}</CodeText>
            </span>
          ) : (
            placeholder
          )}
        </span>
        <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="gap-0 p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-5 py-4">
            <DialogTitle>选择权限点</DialogTitle>
          </DialogHeader>
          <PermissionTreeEditor
            index={index}
            mode="pick"
            picked={value}
            onPick={(code) => {
              onChange(code);
              setOpen(false);
            }}
            heightClassName="max-h-[60vh]"
            defaultExpandDepth={1}
          />
        </DialogContent>
      </Dialog>
    </>
  );
}

const DURATIONS = [
  { label: "1 小时", hours: 1 },
  { label: "4 小时", hours: 4 },
  { label: "1 天", hours: 24 },
  { label: "3 天", hours: 72 },
  { label: "7 天", hours: 168 },
];

const REASONS = ["临时协助", "值班排障", "数据核对", "审计抽查"];

/** 授予临时权限：到点自动失效，不进策略缓存。 */
export function TempPermissionGrantForm({
  adminId,
  index,
  onGranted,
}: {
  adminId: number;
  index: TreeIndex;
  onGranted?: () => void;
}) {
  const mutation = useGrantTempPermissionMutation();
  const [permission, setPermission] = useState<string | null>(null);
  const [app, setApp] = useState<number[]>([]);
  const [hours, setHours] = useState<number | null>(4);
  const [custom, setCustom] = useState(() =>
    toLocalInputValue(new Date(Date.now() + 4 * 3600_000)),
  );
  const [reason, setReason] = useState("");

  const expiresAt = () =>
    hours
      ? new Date(Date.now() + hours * 3600_000).toISOString()
      : fromLocalInputValue(custom);

  const submit = async () => {
    const at = expiresAt();
    if (!permission || !at) return;
    try {
      await mutation.mutateAsync({
        adminId,
        permission,
        appId: app[0] ?? null,
        reason: reason.trim(),
        expiresAt: at,
      });
      toast.success("临时权限已授予");
      setPermission(null);
      setReason("");
      onGranted?.();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "授予失败");
    }
  };

  return (
    <Pane title="授予临时权限" icon={Timer}>
      <div className="grid gap-4 p-4 sm:grid-cols-2">
        <div className="space-y-1.5 sm:col-span-2">
          <Label>权限点</Label>
          <PermissionPicker
            index={index}
            value={permission}
            onChange={setPermission}
          />
        </div>
        <div className="space-y-1.5">
          <Label>作用域</Label>
          <AppPicker
            value={app}
            onChange={setApp}
            allowAll
            allLabel="不限应用"
          />
        </div>
        <div className="space-y-1.5">
          <Label>有效期</Label>
          <div className="flex flex-wrap gap-1.5">
            {DURATIONS.map((item) => (
              <button
                key={item.hours}
                type="button"
                onClick={() => setHours(item.hours)}
                className={cn(
                  "h-8 rounded-lg border px-2.5 text-xs transition-colors",
                  hours === item.hours
                    ? "border-primary/40 bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent",
                )}
              >
                {item.label}
              </button>
            ))}
            <button
              type="button"
              onClick={() => setHours(null)}
              className={cn(
                "h-8 rounded-lg border px-2.5 text-xs transition-colors",
                hours === null
                  ? "border-primary/40 bg-primary/10 text-primary"
                  : "text-muted-foreground hover:bg-accent",
              )}
            >
              自定义
            </button>
          </div>
          {hours === null && (
            <Input
              type="datetime-local"
              value={custom}
              onChange={(e) => setCustom(e.target.value)}
              className="mt-1.5 h-8 text-xs"
            />
          )}
        </div>
        <div className="space-y-1.5 sm:col-span-2">
          <Label htmlFor="temp-reason">原因</Label>
          <div className="flex flex-wrap gap-1.5">
            {REASONS.map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => setReason(item)}
                className={cn(
                  "h-7 rounded-full border px-2.5 text-xs transition-colors",
                  reason === item
                    ? "border-primary/40 bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent",
                )}
              >
                {item}
              </button>
            ))}
          </div>
          <Input
            id="temp-reason"
            value={reason}
            maxLength={200}
            onChange={(e) => setReason(e.target.value)}
            className="h-8 text-xs"
          />
        </div>
      </div>
      <div className="flex justify-end border-t border-border/70 px-4 py-3">
        <Button
          size="sm"
          className="h-8"
          disabled={!permission || mutation.isPending}
          onClick={submit}
        >
          授予
        </Button>
      </div>
    </Pane>
  );
}
