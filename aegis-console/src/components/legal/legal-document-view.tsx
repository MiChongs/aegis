"use client";

import { useCallback, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { AnimatePresence, m, LazyMotion, domAnimation, useReducedMotion } from "motion/react";
import { ArrowLeft, FileText, Info, Languages, Printer, ShieldCheck } from "lucide-react";
import { getLegalDocument, type LegalDocType } from "@/lib/api/legal";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];

/** 图标而已；标题、版本、生效日期一律来自服务端 */
const DOC_ICON: Record<LegalDocType, typeof FileText> = {
  terms: FileText,
  privacy: ShieldCheck,
};

/**
 * 一份法律文本的完整展示。
 *
 * 正文是服务端下发的、**已在写入端净化过**的 HTML，这里直接注入 ——
 * 在读取端再净化一次意味着每个消费方都要记得做，而漏掉的那个就是漏洞入口；
 * 净化只在服务端写入时做一次（`sanitizeRichText`）。
 *
 * 语言不由前端判断：把 `?locale=` 作为**偏好**发给服务端，服务端结合
 * Accept-Language 协商后告诉我们真正给的是哪一份。两端各挑各的，
 * 会出现同一页里标题和正文不是同一种语言。
 */
export function LegalDocumentView({
  docType,
  initialLocale,
}: {
  docType: LegalDocType;
  initialLocale?: string;
}) {
  const reduced = useReducedMotion();
  const [locale, setLocale] = useState<string | undefined>(initialLocale);

  const query = useQuery({
    queryKey: ["legal-document", docType, locale ?? ""],
    queryFn: () => getLegalDocument(docType, locale),
    staleTime: 5 * 60_000,
  });

  const doc = query.data;
  const Icon = DOC_ICON[docType];

  const effectiveText = formatEffective(doc?.effectiveAt);

  const handlePrint = useCallback(() => window.print(), []);

  // 请求的语言和实际给的不一致 = 发生了回落，必须说出来。
  // 不说的结果是读者以为自己读的就是本地化版本。
  const fellBack = Boolean(
    doc && locale && doc.locale.toLowerCase() !== locale.toLowerCase()
  );

  return (
    <LazyMotion features={domAnimation}>
      <div className="min-h-svh bg-background">
        {/* 顶栏：打印时隐藏 —— 打印出来的条款上不该有一排按钮 */}
        <header className="sticky top-0 z-30 border-b bg-background/85 backdrop-blur print:hidden">
          <div className="mx-auto flex h-14 w-full max-w-4xl items-center gap-3 px-5 sm:px-8">
            <Link href="/" className="flex shrink-0 items-center gap-2" aria-label="Aegis">
              <AegisMark className="size-5 text-foreground" />
              <span className="text-sm font-semibold tracking-tight">Aegis</span>
            </Link>

            <div className="ml-auto flex items-center gap-2">
              {doc && doc.locales.length > 1 ? (
                <Select value={doc.locale} onValueChange={setLocale}>
                  <SelectTrigger size="sm" className="h-8 w-auto gap-1.5 text-xs" aria-label="选择语言">
                    <Languages className="size-3.5 text-muted-foreground" />
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    {doc.locales.map((option) => (
                      <SelectItem key={option.locale} value={option.locale} className="text-xs">
                        {option.nativeName || option.locale}
                        {option.source === "default" ? (
                          <span className="ml-1.5 text-[10px] text-muted-foreground">内置</span>
                        ) : null}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}

              <Button variant="ghost" size="icon-sm" aria-label="打印" onClick={handlePrint}>
                <Printer className="size-4" />
              </Button>
            </div>
          </div>
        </header>

        <main className="mx-auto w-full max-w-4xl px-5 pt-10 pb-24 sm:px-8">
          {query.isPending ? (
            <LoadingSkeleton />
          ) : query.isError || !doc ? (
            <EmptyState
              title="暂时读取不到这份文本"
              description="服务端未返回内容。请稍后重试，或联系平台管理员。"
            />
          ) : (
            <m.article
              initial={reduced ? false : { opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.45, ease: EASE }}
            >
              <div className="flex flex-col gap-4">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Icon className="size-3.5" />
                  <span>法律文本</span>
                </div>

                <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{doc.title}</h1>

                <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-foreground">
                  {doc.version ? <Badge variant="secondary" size="sm">版本 {doc.version}</Badge> : null}
                  {effectiveText ? <span>生效日期：{effectiveText}</span> : null}
                  {doc.source === "default" ? (
                    <Badge variant="outline" size="sm" className="font-normal">
                      平台内置文本
                    </Badge>
                  ) : null}
                </div>
              </div>

              {/* 译文声明。十种语言是同一份条款的十个译本，出现歧义时以准据版为准 ——
                  不说明的话，读者会以为自己读的这一版与原文同等效力。 */}
              {doc.locale !== doc.authoritativeLocale ? (
                <Alert className="mt-6 py-2.5">
                  <Languages />
                  <AlertDescription>
                    本页为译文，仅供阅读方便。与
                    {doc.locales.find((item) => item.authoritative)?.nativeName || doc.authoritativeLocale}
                    版本存在歧义时，以后者为准。
                  </AlertDescription>
                </Alert>
              ) : null}

              <AnimatePresence initial={false}>
                {fellBack ? (
                  <m.div
                    initial={reduced ? false : { opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={reduced ? undefined : { opacity: 0, height: 0 }}
                    transition={{ duration: 0.25, ease: EASE }}
                    className="overflow-hidden"
                  >
                    <Alert className="mt-6 py-2.5">
                      <Info />
                      <AlertDescription>
                        暂无所选语言的版本，以下为 {doc.locales.find((item) => item.locale === doc.locale)?.nativeName || doc.locale} 版。
                      </AlertDescription>
                    </Alert>
                  </m.div>
                ) : null}
              </AnimatePresence>

              <Separator className="my-8" />

              {/* 正文。服务端已净化，此处直接渲染。 */}
              <div
                className={cn(
                  "legal-prose",
                  // 打印时把颜色压回黑白，深色主题下打印出来是一整页黑底
                  "print:text-black"
                )}
                dangerouslySetInnerHTML={{ __html: doc.body }}
              />

              <Separator className="my-10" />

              <div className="flex flex-wrap items-center justify-between gap-4 text-xs text-muted-foreground print:hidden">
                <Link
                  href={docType === "terms" ? "/legal/privacy" : "/legal/terms"}
                  className="inline-flex items-center gap-1.5 font-medium text-foreground underline-offset-4 hover:underline"
                >
                  {docType === "terms" ? "阅读隐私政策" : "阅读用户服务协议"}
                </Link>
                <Link
                  href="/login"
                  className="inline-flex items-center gap-1.5 transition-colors hover:text-foreground"
                >
                  <ArrowLeft className="size-3.5" />
                  返回登录
                </Link>
              </div>
            </m.article>
          )}
        </main>
      </div>
    </LazyMotion>
  );
}

/** 生效日期。纯计算，不值得一个 useMemo —— 一个日期格式化每帧跑一次也没有代价。 */
function formatEffective(value?: string | null) {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "long", day: "numeric" });
}

function LoadingSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-4 w-20" />
      <Skeleton className="h-10 w-2/3" />
      <Skeleton className="h-4 w-40" />
      <Separator className="my-6" />
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="flex flex-col gap-2">
          <Skeleton className="h-5 w-1/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-11/12" />
          <Skeleton className="mb-4 h-4 w-4/5" />
        </div>
      ))}
    </div>
  );
}
