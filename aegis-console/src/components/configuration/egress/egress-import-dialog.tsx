"use client";

import { useRef, useState } from "react";
import { CloudDownload, FileUp, Link2, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { edgeId, newId } from "@/components/apps/item-grant/item-grant-graph";
import { ToggleRow } from "@/components/configuration/egress/egress-fields";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type { EgressGraph, EgressImportOptions } from "@/lib/api/egress";
import { ApiError } from "@/lib/api/client";
import { useImportEgressGraphMutation } from "@/lib/egress-hooks";
import { cn } from "@/lib/utils";

/**
 * 导入 mihomo / Clash 配置：粘贴 YAML 或分享链接（ss / vmess / vless / trojan / hy2 / tuic …，整段 base64 也行），
 * 或者填订阅地址——推荐作为「订阅」节点由核心定时更新，也可以一次性拉下来展开成节点。
 * 导入只改画布，保存或应用之前不影响线上。
 */

type Source = "paste" | "subscription";

export function EgressImportDialog({
  open,
  onOpenChange,
  graph,
  onImported
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  graph: () => EgressGraph;
  onImported: (graph: EgressGraph, from: string) => void;
}) {
  const importer = useImportEgressGraphMutation();
  const [source, setSource] = useState<Source>("paste");
  const [content, setContent] = useState("");
  const [url, setUrl] = useState("");
  const [name, setName] = useState("机场");
  const [asProvider, setAsProvider] = useState(true);
  const [options, setOptions] = useState<EgressImportOptions>({ mode: "merge", routeFallback: false });
  const fileRef = useRef<HTMLInputElement>(null);

  const finish = (next: EgressGraph, from: string, notes?: string[] | null) => {
    onImported(next, from);
    onOpenChange(false);
    setContent("");
    if (notes?.length) toast.message("导入完成", { description: notes.slice(0, 4).join("；"), duration: 8000 });
    else toast.success("导入完成，检查后保存或应用");
  };

  const submit = () => {
    if (source === "subscription" && asProvider) {
      finish(addProvider(graph(), name.trim() || "订阅", url.trim(), options.routeFallback), `订阅「${name.trim() || "订阅"}」`);
      return;
    }
    importer.mutate(
      { graph: graph(), content: source === "paste" ? content : undefined, url: source === "subscription" ? url.trim() : undefined, options },
      {
        onSuccess: (data) => {
          const added = data.added;
          const parts = [
            added.proxies && `${added.proxies} 个节点`,
            added.groups && `${added.groups} 个策略组`,
            added.providers && `${added.providers} 个订阅`,
            added.mihomoRules && `${added.mihomoRules} 条分流`
          ].filter(Boolean);
          finish(data.graph, `导入的${data.format === "uri" ? "分享链接" : "配置"}（${parts.join("、")}）`, data.notes);
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "导入失败")
      }
    );
  };

  const ready = source === "paste" ? content.trim().length > 0 : /^https?:\/\/.+/.test(url.trim());

  return (
    <Dialog open={open} onOpenChange={(next) => !importer.isPending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>导入 mihomo 配置</DialogTitle>
          <DialogDescription>导入只改画布，保存或应用之前不影响线上。入站、控制器、TUN 这些由网关托管的设置会被忽略。</DialogDescription>
        </DialogHeader>
        <Tabs value={source} onValueChange={(value) => setSource(value as Source)} className="gap-3">
          <TabsList className="w-full">
            <TabsTrigger value="paste" className="flex-1 text-xs">
              <FileUp /> 粘贴或上传
            </TabsTrigger>
            <TabsTrigger value="subscription" className="flex-1 text-xs">
              <Link2 /> 订阅地址
            </TabsTrigger>
          </TabsList>
          <TabsContent value="paste" className="space-y-2">
            <Textarea
              rows={12}
              className="min-h-0 font-mono text-xs leading-5"
              spellCheck={false}
              placeholder={"proxies:\n  - {name: 香港, type: vless, server: hk.example.com, port: 443, …}\nproxy-groups: …\nrules: …\n\n或一行一条分享链接：\nvless://…\nhy2://…"}
              value={content}
              onChange={(event) => setContent(event.target.value)}
            />
            <div className="flex items-center justify-between gap-2">
              <p className="text-[11px] leading-4 text-muted-foreground">支持 Clash / mihomo YAML，以及 ss、ssr、vmess、vless、trojan、hysteria、hy2、tuic、anytls、socks、http 链接。</p>
              <input
                ref={fileRef}
                type="file"
                accept=".yaml,.yml,.txt,.conf"
                className="hidden"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  if (file) setContent(await file.text());
                  event.target.value = "";
                }}
              />
              <Button size="sm" variant="outline" className="h-7 shrink-0 text-xs" onClick={() => fileRef.current?.click()}>
                选择文件
              </Button>
            </div>
          </TabsContent>
          <TabsContent value="subscription" className="space-y-3">
            <div className="space-y-1.5">
              <Label className="text-xs">订阅地址</Label>
              <Input className="h-8 font-mono text-xs" placeholder="https://…" value={url} onChange={(event) => setUrl(event.target.value)} />
            </div>
            <div className="grid grid-cols-2 gap-2">
              {[
                { value: true, title: "作为订阅（推荐）", hint: "核心定时更新节点，附带一个自动测速组" },
                { value: false, title: "一次性导入", hint: "现在下载并展开成节点与策略组" }
              ].map((item) => (
                <button
                  key={String(item.value)}
                  type="button"
                  onClick={() => setAsProvider(item.value)}
                  className={cn("rounded-xl border px-3 py-2 text-left transition-colors", asProvider === item.value ? "border-primary bg-primary/8" : "hover:bg-accent/40")}
                >
                  <div className="text-xs font-medium">{item.title}</div>
                  <div className="text-[11px] leading-4 text-muted-foreground">{item.hint}</div>
                </button>
              ))}
            </div>
            {asProvider ? (
              <div className="space-y-1.5">
                <Label className="text-xs">订阅名称</Label>
                <Input className="h-8 text-xs" value={name} onChange={(event) => setName(event.target.value)} />
              </div>
            ) : (
              <p className="text-[11px] leading-4 text-muted-foreground">由服务器经出海网关下载（带 clash.meta 标识，多数机场会下发 Clash 格式），不接受内网地址。</p>
            )}
          </TabsContent>
        </Tabs>

        <div className="space-y-2 rounded-xl border p-3">
          {!(source === "subscription" && asProvider) ? (
            <div className="grid grid-cols-2 gap-2">
              {[
                { value: "merge" as const, title: "并入画布", hint: "保留现有节点，重名的加序号" },
                { value: "replace" as const, title: "替换 mihomo 部分", hint: "网关规则按名字接回新节点" }
              ].map((item) => (
                <button
                  key={item.value}
                  type="button"
                  onClick={() => setOptions({ ...options, mode: item.value })}
                  className={cn("rounded-xl border px-3 py-2 text-left transition-colors", options.mode === item.value ? "border-primary bg-primary/8" : "hover:bg-accent/40")}
                >
                  <div className="text-xs font-medium">{item.title}</div>
                  <div className="text-[11px] leading-4 text-muted-foreground">{item.hint}</div>
                </button>
              ))}
            </div>
          ) : null}
          <ToggleRow
            label="未命中规则的出站交给 mihomo"
            hint="会把网关的「未命中」改成走代理并接到导入的分流或第一个策略组，影响全部出站，请谨慎"
            checked={options.routeFallback}
            onChange={(routeFallback) => setOptions({ ...options, routeFallback })}
          />
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={importer.isPending}>
            取消
          </Button>
          <Button onClick={submit} disabled={!ready || importer.isPending}>
            {importer.isPending ? <Loader2 className="animate-spin" /> : <CloudDownload />}
            导入
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 作为订阅导入：一个订阅节点 + 一个用它的自动测速组，放在现有节点右侧。 */
function addProvider(graph: EgressGraph, name: string, url: string, routeFallback: boolean): EgressGraph {
  const right = Math.max(0, ...(graph.nodes ?? []).map((node) => node.position.x)) + 420;
  const providerId = newId("m_provider");
  const groupId = newId("m_group");
  const nodes = [
    ...(graph.nodes ?? []),
    {
      id: groupId,
      type: "m_group" as const,
      position: { x: right, y: 0 },
      data: { group: { name: `${name} 自动`, type: "url-test", url: "https://www.gstatic.com/generate_204", interval: 300, tolerance: 50, lazy: true }, order: [providerId] }
    },
    {
      id: providerId,
      type: "m_provider" as const,
      position: { x: right + 380, y: 0 },
      data: {
        name,
        provider: {
          type: "http",
          url,
          interval: 3600,
          "exclude-filter": "(?i)过期|剩余|流量|官网|重置",
          "health-check": { enable: true, url: "https://www.gstatic.com/generate_204", interval: 600, lazy: true }
        }
      }
    }
  ];
  const edges = [...(graph.edges ?? []), { id: edgeId(groupId, "members", providerId), source: groupId, sourceHandle: "members", target: providerId }];
  if (routeFallback) {
    const gateway = nodes.find((node) => node.type === "gateway");
    if (gateway) {
      gateway.data = { ...gateway.data, defaultAction: "proxy" };
      edges.push({ id: edgeId(gateway.id, "fallback", groupId), source: gateway.id, sourceHandle: "fallback", target: groupId });
    }
  }
  return { ...graph, nodes, edges };
}
