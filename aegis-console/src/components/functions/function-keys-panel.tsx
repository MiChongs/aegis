"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Clipboard, Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import {
  createAppFunctionKey,
  listAppFunctionKeys,
  revokeAppFunctionKey,
  type CreatedAppFunctionKey
} from "@/lib/api/app-functions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : error instanceof Error
      ? error.message
      : "操作失败";
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

/** 明文密钥只会出现一次，用独立弹窗强制用户当场保存 */
function SecretDialog({
  created,
  onClose
}: {
  created: CreatedAppFunctionKey | null;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);

  return (
    <Dialog open={Boolean(created)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>函数调用密钥</DialogTitle>
          <DialogDescription>
            密钥仅显示一次，关闭后无法再次查看；服务端仅保存 SHA-256 摘要。
          </DialogDescription>
        </DialogHeader>
        {created ? (
          <div className="overflow-x-auto rounded-lg border bg-muted/40 p-3">
            <code className="font-mono text-xs">{created.secret}</code>
          </div>
        ) : null}
        <DialogFooter>
          <Button
            onClick={async () => {
              if (!created) return;
              try {
                await navigator.clipboard.writeText(created.secret);
                setCopied(true);
                toast.success("已复制到剪贴板");
              } catch {
                toast.error("剪贴板不可用，请手动选中复制");
              }
            }}
          >
            {copied ? <Check className="size-4" /> : <Clipboard className="size-4" />}
            复制密钥
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function FunctionKeysPanel({ appKey }: { appKey?: string | null }) {
  const token = useAdminToken();
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedAppFunctionKey | null>(null);

  const keysQuery = useQuery({
    queryKey: ["app-function-keys", token, appKey],
    queryFn: () => listAppFunctionKeys(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });

  if (!appKey) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          请先选择应用。
        </CardContent>
      </Card>
    );
  }

  const keys = keysQuery.data || [];

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>函数调用密钥</CardTitle>
          <CardDescription>
            接入应用的服务端通过 <code className="font-mono">X-Aegis-Function-Key</code>{" "}
            请求头调用远程函数。密钥以 <code className="font-mono">afk_</code> 开头，
            仅可保存在服务端或 Worker Secret，不得嵌入网页与客户端安装包。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex max-w-lg gap-2">
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="production-backend"
            />
            <Button
              disabled={!name.trim() || creating}
              onClick={async () => {
                if (!token || !appKey) return;
                setCreating(true);
                try {
                  const result = await createAppFunctionKey(token, appKey, name.trim());
                  setCreated(result);
                  setName("");
                  await keysQuery.refetch();
                } catch (error) {
                  toast.error(errorMessage(error));
                } finally {
                  setCreating(false);
                }
              }}
            >
              {creating ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              创建密钥
            </Button>
          </div>

          {keysQuery.isLoading ? (
            <div className="flex min-h-32 items-center justify-center">
              <Loader2 className="size-4 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>前缀</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最后使用</TableHead>
                  <TableHead>创建时间</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell className="font-medium">{key.name}</TableCell>
                    <TableCell className="font-mono text-xs">{key.keyPrefix}</TableCell>
                    <TableCell>
                      <Badge variant={key.status === "active" ? "default" : "outline"}>{key.status}</Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatTime(key.lastUsedAt)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatTime(key.createdAt)}
                    </TableCell>
                    <TableCell className="text-right">
                      {key.status === "active" ? (
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`撤销密钥 ${key.name}`}
                          onClick={async () => {
                            if (!token || !appKey) return;
                            try {
                              await revokeAppFunctionKey(token, appKey, key.id);
                              await keysQuery.refetch();
                              toast.success("密钥已撤销");
                            } catch (error) {
                              toast.error(errorMessage(error));
                            }
                          }}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
                {!keys.length ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                      暂无调用密钥
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <SecretDialog created={created} onClose={() => setCreated(null)} />
    </>
  );
}
