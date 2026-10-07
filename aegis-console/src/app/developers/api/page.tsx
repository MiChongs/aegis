"use client";

import { Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, FileJson, KeyRound, ListTree, RotateCw } from "lucide-react";
import { OperationDetail } from "@/components/developers/operation-detail";
import { OperationNav } from "@/components/developers/operation-nav";
import {
  flattenOperations,
  getOpenAPISpec,
  groupByTag,
  resolveTagName,
  type FlatOperation,
  type OpenAPISpec
} from "@/lib/api/openapi";
import { CREDENTIAL_FIELDS, securitySchemeNames } from "@/lib/api/openapi-request";
import { useCredentials, updateCredentials } from "@/lib/developer-credentials";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";

function Stat({ value, label }: { value: string | number; label: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="font-mono text-[15px] font-semibold tabular-nums">{value}</span>
      <span className="text-[12.5px] text-muted-foreground">{label}</span>
    </div>
  );
}

/** 与真实布局同尺寸的骨架，规范拉取期间页面不跳动 */
function ReferenceSkeleton() {
  return (
    <div className="mx-auto w-full max-w-[1680px] px-4 md:px-6" aria-busy aria-label="正在加载接口文档">
      <div className="space-y-3 border-b py-8">
        <Skeleton className="h-3.5 w-28" />
        <Skeleton className="h-8 w-44" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      <div className="gap-10 py-8 xl:grid xl:grid-cols-[264px_minmax(0,1fr)]">
        <div className="space-y-3 max-xl:hidden">
          <Skeleton className="h-9 w-full" />
          {Array.from({ length: 9 }, (_, index) => (
            <Skeleton key={index} className="h-7" style={{ width: `${88 - (index % 4) * 12}%` }} />
          ))}
        </div>
        <div className="space-y-5">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-8 w-72 max-w-full" />
          <Skeleton className="h-12 w-full max-w-3xl" />
          <Skeleton className="h-4 w-full max-w-2xl" />
          <Skeleton className="h-4 w-2/3 max-w-xl" />
          <Skeleton className="mt-8 h-40 w-full max-w-3xl" />
        </div>
      </div>
    </div>
  );
}

function ApiReferenceInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const tagParam = searchParams.get("tag") || "";
  const opParam = searchParams.get("op") || "";
  const [keyword, setKeyword] = useState("");
  const [credentialsOpen, setCredentialsOpen] = useState(false);
  const [navOpen, setNavOpen] = useState(false);
  const credentials = useCredentials();
  const searchRef = useRef<HTMLInputElement>(null);

  // 同源部署时 apiBaseUrl 为空串，回落到当前页面 origin（服务端渲染阶段为空）
  const pageOrigin = useOrigin();
  const [baseUrl, setBaseUrl] = useState("");
  const effectiveBase = baseUrl || appConfig.apiBaseUrl || pageOrigin;

  const specQuery = useQuery<OpenAPISpec>({
    queryKey: ["openapi-spec"],
    queryFn: getOpenAPISpec,
    staleTime: 5 * 60_000
  });

  const operations = useMemo(() => flattenOperations(specQuery.data ?? null), [specQuery.data]);
  const groups = useMemo(() => groupByTag(operations), [operations]);
  // 上一个 / 下一个按目录的展示顺序走，而不是规范里的原始顺序
  const ordered = useMemo(() => groups.flatMap((group) => group.items), [groups]);
  const securitySchemes = specQuery.data?.components?.securitySchemes || {};
  const securedCount = useMemo(
    () => operations.filter((operation) => securitySchemeNames(operation).length > 0).length,
    [operations]
  );
  const filledCredentials = CREDENTIAL_FIELDS.filter((field) => credentials[field.key]).length;

  // tagParam 可能来自后端 /docs/tags/:slug 的 302，形如 admin-system，需按 slug 反查
  const activeTag = useMemo(() => {
    const names = groups.map((group) => group.name);
    return resolveTagName(tagParam, names) || names[0] || "";
  }, [tagParam, groups]);

  // ?op= 指向的接口优先（分享链接），否则取当前分组第一个
  const selected = useMemo(() => {
    const byParam = operations.find((operation) => operation.key === opParam);
    if (byParam) return byParam;
    return groups.find((group) => group.name === activeTag)?.items[0] ?? null;
  }, [operations, opParam, groups, activeTag]);

  const selectedIndex = selected ? ordered.findIndex((item) => item.key === selected.key) : -1;

  // 「/」聚焦搜索，与多数文档站一致；输入框内按下时不拦截
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable=true]")) return;
      event.preventDefault();
      if (window.matchMedia("(min-width: 1280px)").matches) searchRef.current?.focus();
      else setNavOpen(true);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  function selectOperation(operation: FlatOperation) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("op", operation.key);
    params.set("tag", operation.tag);
    router.replace(`/developers/api?${params.toString()}`, { scroll: false });
    setNavOpen(false);
    // 已滚动到正文下方时回到接口标题处，目录侧栏本身是吸顶的不受影响
    const anchor = document.getElementById("operation-top");
    if (anchor && anchor.getBoundingClientRect().top < 0) {
      anchor.scrollIntoView({ block: "start" });
    }
  }

  if (specQuery.isLoading) return <ReferenceSkeleton />;

  if (specQuery.isError) {
    return (
      <div className="mx-auto flex max-w-md flex-col items-center px-4 py-24 text-center">
        <span className="inline-flex size-12 items-center justify-center rounded-full bg-destructive/10">
          <AlertCircle className="size-6 text-destructive" />
        </span>
        <h2 className="mt-4 text-lg font-semibold">无法加载接口文档</h2>
        <p className="mt-1.5 text-sm text-muted-foreground">
          拉取 <code className="font-mono">/openapi.json</code> 失败，请确认后端服务可达。
        </p>
        <Button
          variant="outline"
          size="sm"
          className="mt-5"
          disabled={specQuery.isFetching}
          onClick={() => void specQuery.refetch()}
        >
          <RotateCw className={specQuery.isFetching ? "animate-spin" : undefined} />
          重试
        </Button>
      </div>
    );
  }

  // 搜索框的 ref 只交给常驻侧栏：抽屉里的实例卸载时会把 ref 置空
  const renderNav = (withSearchRef: boolean) => (
    <OperationNav
      groups={groups}
      activeTag={selected?.tag || activeTag}
      selectedKey={selected?.key}
      keyword={keyword}
      onKeywordChange={setKeyword}
      onSelect={selectOperation}
      searchRef={withSearchRef ? searchRef : undefined}
      className="h-full"
    />
  );

  return (
    <div className="mx-auto w-full max-w-[1680px] px-4 md:px-6">
      <div className="flex flex-col gap-5 border-b py-8 lg:flex-row lg:items-end lg:justify-between">
        <div className="min-w-0">
          <p className="text-xs font-medium tracking-[0.18em] text-muted-foreground uppercase">
            {specQuery.data?.info?.title || "OpenAPI"}
          </p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight">接口文档</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            由后端路由实时生成，可直接在页面中调试。
          </p>
          <div className="mt-4 flex flex-wrap items-center gap-x-6 gap-y-2">
            <Stat value={specQuery.data?.info?.version || "—"} label="版本" />
            <Stat value={operations.length} label="个端点" />
            <Stat value={groups.length} label="个分组" />
            <Stat value={securedCount} label="个需认证" />
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" className="xl:hidden" onClick={() => setNavOpen(true)}>
            <ListTree />
            接口目录
          </Button>
          <Button variant="outline" size="sm" asChild>
            <a href={`${appConfig.apiBaseUrl}/openapi.json`} target="_blank" rel="noreferrer">
              <FileJson />
              openapi.json
            </a>
          </Button>
          <Button size="sm" onClick={() => setCredentialsOpen(true)}>
            <KeyRound />
            调试凭据
            {filledCredentials ? (
              <span className="ml-0.5 rounded-full bg-primary-foreground/20 px-1.5 font-mono text-[10.5px] tabular-nums">
                {filledCredentials}
              </span>
            ) : null}
          </Button>
        </div>
      </div>

      <div className="gap-10 xl:grid xl:grid-cols-[264px_minmax(0,1fr)]">
        <nav aria-label="接口目录" className="max-xl:hidden">
          <div className="sticky top-14 h-[calc(100vh-3.5rem)] py-6">{renderNav(true)}</div>
        </nav>

        <div id="operation-top" className="min-w-0 scroll-mt-20 py-8">
          {selected ? (
            <OperationDetail
              // key 保证切换接口时调试面板的表单与响应完全重置
              key={selected.key}
              operation={selected}
              baseUrl={effectiveBase}
              credentials={credentials}
              securitySchemes={securitySchemes}
              previous={selectedIndex > 0 ? ordered[selectedIndex - 1] : undefined}
              next={selectedIndex >= 0 ? ordered[selectedIndex + 1] : undefined}
              onSelect={selectOperation}
            />
          ) : (
            <div className="rounded-xl border border-dashed py-24 text-center text-sm text-muted-foreground">
              当前规范中没有可展示的接口
            </div>
          )}
        </div>
      </div>

      <Sheet open={navOpen} onOpenChange={setNavOpen}>
        <SheetContent side="left" className="w-[320px] gap-0 p-0 sm:max-w-[320px]">
          <SheetHeader className="border-b">
            <SheetTitle>接口目录</SheetTitle>
            <SheetDescription className="sr-only">按分组浏览或搜索全部接口</SheetDescription>
          </SheetHeader>
          <div className="min-h-0 flex-1 p-4">{renderNav(false)}</div>
        </SheetContent>
      </Sheet>

      <Sheet open={credentialsOpen} onOpenChange={setCredentialsOpen}>
        <SheetContent side="right" className="w-full gap-0 sm:max-w-md">
          <SheetHeader className="border-b">
            <SheetTitle>调试凭据</SheetTitle>
            <SheetDescription>
              按各接口声明的认证方式自动附加到请求头，仅保存在本浏览器中。请勿填入生产环境的长期凭据。
            </SheetDescription>
          </SheetHeader>
          <div className="flex-1 space-y-5 overflow-y-auto p-4">
            <div className="space-y-1.5">
              <label className="text-[13px] font-medium" htmlFor="cred-base">
                Base URL
              </label>
              <Input
                id="cred-base"
                value={baseUrl}
                placeholder={appConfig.apiBaseUrl || pageOrigin || "https://api.example.com"}
                className="font-mono text-xs"
                onChange={(event) => setBaseUrl(event.target.value.trim())}
              />
              <p className="text-xs leading-relaxed text-muted-foreground">
                留空则使用当前站点，经同源代理转发到后端。跨域时需要目标服务允许本页来源。
              </p>
            </div>
            {CREDENTIAL_FIELDS.map((field) => (
              <div key={field.key} className="space-y-1.5">
                <label
                  className="flex items-center gap-2 text-[13px] font-medium"
                  htmlFor={`cred-${field.key}`}
                >
                  {field.label}
                  {credentials[field.key] ? (
                    <span className="size-1.5 rounded-full bg-emerald-500" aria-label="已填写" />
                  ) : null}
                </label>
                <Input
                  id={`cred-${field.key}`}
                  type="password"
                  autoComplete="off"
                  value={credentials[field.key]}
                  className="font-mono text-xs"
                  onChange={(event) => updateCredentials({ [field.key]: event.target.value })}
                />
                <p className="text-xs leading-relaxed text-muted-foreground">{field.hint}</p>
              </div>
            ))}
          </div>
          {filledCredentials ? (
            <div className="border-t p-4">
              <Button
                variant="ghost"
                size="sm"
                className="w-full text-muted-foreground"
                onClick={() =>
                  updateCredentials(
                    Object.fromEntries(CREDENTIAL_FIELDS.map((field) => [field.key, ""]))
                  )
                }
              >
                清空全部凭据
              </Button>
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
    </div>
  );
}

export default function ApiReferencePage() {
  return (
    <Suspense fallback={<ReferenceSkeleton />}>
      <ApiReferenceInner />
    </Suspense>
  );
}
