"use client";

import { useCallback, useMemo, useSyncExternalStore } from "react";
import Image from "next/image";
import useEmblaCarousel from "embla-carousel-react";
import Autoplay from "embla-carousel-autoplay";
import type { EmblaCarouselType } from "embla-carousel";
import { ChevronLeft, ChevronRight, Eye } from "lucide-react";
import type { BannerItem } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { BannerThumbFallback, bannerPreviewSrc, bannerSlot, resolveSchedule } from "./content-shared";

/**
 * 投放预览。
 *
 * 它回答的是「用户此刻打开应用会看到什么」，而不是「库里有几条 Banner」——
 * 后者列表已经说了。因此这里只画**当前真正生效**的那几条，顺序与客户端一致，
 * 停用的、没到时间的、已过期的都不出现。少了它，管理员要靠三个字段
 * 加当前时间做心算才能知道自己刚配的东西到底露没露出来。
 */
/**
 * 当前停在第几张。
 *
 * 用 `useSyncExternalStore` 订阅 Embla，而不是 `useEffect` + `setState`：
 * 后者要在 effect 体里同步 setState 一次做初始对齐，既触发级联渲染，
 * 也过不了 `react-hooks/set-state-in-effect`。与 `use-client-value.ts` 同一约束。
 */
function useSelectedSnap(embla?: EmblaCarouselType) {
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (!embla) return () => {};
      embla.on("select", onStoreChange).on("reInit", onStoreChange);
      return () => {
        embla.off("select", onStoreChange).off("reInit", onStoreChange);
      };
    },
    [embla]
  );
  const getSnapshot = useCallback(() => (embla ? embla.selectedScrollSnap() : 0), [embla]);
  return useSyncExternalStore(subscribe, getSnapshot, () => 0);
}

export function BannerPreview({ items, slot }: { items: BannerItem[]; slot: string }) {
  const meta = useMemo(() => bannerSlot(slot), [slot]);
  const live = useMemo(
    () => items.filter((item) => resolveSchedule(item.status, item.startTime, item.endTime).state === "live"),
    [items]
  );

  const autoplay = useMemo(
    () => Autoplay({ delay: 4500, stopOnInteraction: false, stopOnMouseEnter: true }),
    []
  );
  const [emblaRef, embla] = useEmblaCarousel({ loop: live.length > 1, align: "start" }, [autoplay]);
  const selected = useSelectedSnap(embla);

  if (!live.length) {
    return (
      <div
        className="flex items-center justify-center rounded-xl border border-dashed bg-muted/30 text-sm text-muted-foreground"
        style={{ aspectRatio: meta.aspect, maxHeight: 260 }}
      >
        {meta.label}当前没有生效的素材
      </div>
    );
  }

  return (
    <div className="group relative overflow-hidden rounded-xl border bg-card">
      <div ref={emblaRef} className="overflow-hidden">
        <div className="flex touch-pan-y">
          {live.map((item) => {
            const src = bannerPreviewSrc(item);
            return (
              <div key={item.id} className="relative min-w-0 shrink-0 grow-0 basis-full">
                <div className="relative w-full overflow-hidden" style={{ aspectRatio: meta.aspect, maxHeight: 260 }}>
                  {src ? (
                    <Image src={src} alt={item.title || ""} fill unoptimized sizes="100vw" className="object-cover" />
                  ) : (
                    <BannerThumbFallback slot={meta} />
                  )}
                  <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/75 via-black/35 to-transparent px-4 pb-3 pt-10">
                    <div className="text-sm font-semibold text-white drop-shadow">{item.title}</div>
                    {item.content ? (
                      <div className="mt-0.5 line-clamp-1 text-xs text-white/80">{item.content}</div>
                    ) : null}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <Badge variant="secondary" size="sm" className="absolute left-3 top-3 gap-1 bg-black/55 text-white">
        <Eye className="size-3" />
        投放中 {live.length}
      </Badge>

      {live.length > 1 ? (
        <>
          <Button
            variant="secondary"
            size="icon"
            aria-label="上一张"
            onClick={() => embla?.scrollPrev()}
            className="absolute left-2 top-1/2 size-7 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100"
          >
            <ChevronLeft className="size-4" />
          </Button>
          <Button
            variant="secondary"
            size="icon"
            aria-label="下一张"
            onClick={() => embla?.scrollNext()}
            className="absolute right-2 top-1/2 size-7 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100"
          >
            <ChevronRight className="size-4" />
          </Button>
          <div className="absolute inset-x-0 bottom-2 flex justify-center gap-1.5">
            {live.map((item, index) => (
              <button
                key={item.id}
                type="button"
                aria-label={`第 ${index + 1} 张`}
                onClick={() => embla?.scrollTo(index)}
                className={cn(
                  "h-1 rounded-full transition-all",
                  index === selected ? "w-5 bg-white" : "w-1.5 bg-white/50"
                )}
              />
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
}
