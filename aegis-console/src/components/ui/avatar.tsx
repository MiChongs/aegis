"use client"

import * as React from "react"
import { Avatar as AvatarPrimitive } from "radix-ui"

import { LightboxOverlay } from "@/components/ui/image-lightbox"
import { cn } from "@/lib/utils"

/* ------------------------------------------------------------------ */
/*  全局图片 Blob 缓存（内存级，页面生命周期内有效）                        */
/* ------------------------------------------------------------------ */

const blobCache = new Map<string, string>()
const pendingFetches = new Map<string, Promise<string | null>>()
const MAX_ENTRIES = 200

function evictLRU() {
  if (blobCache.size <= MAX_ENTRIES) return
  const oldest = blobCache.keys().next().value
  if (oldest) {
    const u = blobCache.get(oldest)
    if (u) URL.revokeObjectURL(u)
    blobCache.delete(oldest)
  }
}

function loadImage(src: string): Promise<string | null> {
  const hit = blobCache.get(src)
  if (hit) return Promise.resolve(hit)

  const pending = pendingFetches.get(src)
  if (pending) return pending

  const p = fetch(src)
    .then((r) => (r.ok ? r.blob() : null))
    .then((blob) => {
      pendingFetches.delete(src)
      if (!blob) return null
      evictLRU()
      const objectUrl = URL.createObjectURL(blob)
      blobCache.set(src, objectUrl)
      return objectUrl
    })
    .catch(() => {
      pendingFetches.delete(src)
      return null
    })

  pendingFetches.set(src, p)
  return p
}

export function evictAvatarCache(prefix?: string) {
  for (const [key, url] of blobCache) {
    if (!prefix || key.startsWith(prefix)) {
      URL.revokeObjectURL(url)
      blobCache.delete(key)
    }
  }
}

function useCachedSrc(src?: string) {
  const srcStr = typeof src === "string" && src ? src : ""
  const syncHit = React.useMemo(
    () => (srcStr ? (blobCache.get(srcStr) ?? null) : null),
    [srcStr]
  )
  const [asyncHit, setAsyncHit] = React.useState<string | null>(null)

  React.useEffect(() => {
    if (!srcStr || syncHit) {
      setAsyncHit(null)
      return
    }

    let cancelled = false
    loadImage(srcStr).then((url) => {
      if (!cancelled) setAsyncHit(url)
    })
    return () => {
      cancelled = true
      setAsyncHit(null)
    }
  }, [srcStr, syncHit])

  return syncHit || asyncHit
}

type AvatarPreviewContextValue = {
  setPreviewData: (
    src?: string | null,
    alt?: string | null,
    cachedSrc?: string | null
  ) => void
}

const AvatarPreviewContext =
  React.createContext<AvatarPreviewContextValue | null>(null)

function Avatar({
  className,
  size = "default",
  preview = true,
  onClick,
  onKeyDown,
  tabIndex,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Root> & {
  size?: "default" | "sm" | "lg"
  /** 点击头像打开 lightbox 预览（项目扩展，非官方 API） */
  preview?: boolean
}) {
  const [open, setOpen] = React.useState(false)
  const [previewSrc, setPreviewSrc] = React.useState<string | null>(null)
  const [previewAlt, setPreviewAlt] = React.useState<string>("头像预览")
  // 优先使用缓存的 blob URL，即时打开 lightbox 无需二次加载
  const [cachedPreviewSrc, setCachedPreviewSrc] = React.useState<string | null>(
    null
  )

  const setPreviewData = React.useCallback(
    (src?: string | null, alt?: string | null, cached?: string | null) => {
      setPreviewSrc(src && src.trim() ? src : null)
      setPreviewAlt(alt && alt.trim() ? alt : "头像预览")
      setCachedPreviewSrc(cached && cached.trim() ? cached : null)
    },
    []
  )

  const canPreview = preview && Boolean(previewSrc)
  // lightbox 展示优先用缓存 blob，回退到原始 URL
  const lightboxSrc = cachedPreviewSrc || previewSrc

  const handleClick = React.useCallback<React.MouseEventHandler<HTMLSpanElement>>(
    (event) => {
      onClick?.(event)
      if (!event.defaultPrevented && canPreview) {
        event.preventDefault()
        event.stopPropagation()
        setOpen(true)
      }
    },
    [canPreview, onClick]
  )

  const handleKeyDown = React.useCallback<
    React.KeyboardEventHandler<HTMLSpanElement>
  >(
    (event) => {
      onKeyDown?.(event)
      if (event.defaultPrevented || !canPreview) return
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault()
        event.stopPropagation()
        setOpen(true)
      }
    },
    [canPreview, onKeyDown]
  )

  const slides = React.useMemo(
    () => (lightboxSrc ? [{ src: lightboxSrc, alt: previewAlt }] : []),
    [lightboxSrc, previewAlt]
  )

  return (
    <AvatarPreviewContext.Provider value={{ setPreviewData }}>
      <>
        <AvatarPrimitive.Root
          data-slot="avatar"
          data-size={size}
          className={cn(
            "group/avatar relative flex size-8 shrink-0 overflow-hidden rounded-full select-none data-[size=lg]:size-10 data-[size=sm]:size-6",
            canPreview &&
              "cursor-zoom-in transition focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:outline-none",
            className
          )}
          onClick={handleClick}
          onKeyDown={handleKeyDown}
          role={canPreview ? "button" : props.role}
          tabIndex={canPreview ? (tabIndex ?? 0) : tabIndex}
          aria-label={
            canPreview ? `${previewAlt}，点击预览头像` : props["aria-label"]
          }
          {...props}
        />
        {canPreview ? (
          <LightboxOverlay
            open={open}
            onClose={() => setOpen(false)}
            slides={slides}
          />
        ) : null}
      </>
    </AvatarPreviewContext.Provider>
  )
}

function AvatarImage({
  className,
  src,
  alt,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Image>) {
  const cached = useCachedSrc(src as string | undefined)
  const previewContext = React.useContext(AvatarPreviewContext)

  React.useEffect(() => {
    // 同步原始 URL + 缓存 blob URL，lightbox 优先使用缓存实现即时预览
    previewContext?.setPreviewData(
      src as string | undefined,
      alt ?? null,
      cached ?? null
    )
  }, [alt, cached, previewContext, src])

  return (
    <AvatarPrimitive.Image
      data-slot="avatar-image"
      className={cn("aspect-square size-full", className)}
      src={cached || undefined}
      alt={alt}
      {...props}
    />
  )
}

function AvatarFallback({
  className,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Fallback>) {
  return (
    <AvatarPrimitive.Fallback
      data-slot="avatar-fallback"
      className={cn(
        "flex size-full items-center justify-center rounded-full bg-muted text-sm text-muted-foreground group-data-[size=sm]/avatar:text-xs",
        className
      )}
      {...props}
    />
  )
}

function AvatarBadge({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="avatar-badge"
      className={cn(
        "absolute right-0 bottom-0 z-10 inline-flex items-center justify-center rounded-full bg-primary text-primary-foreground ring-2 ring-background select-none",
        "group-data-[size=sm]/avatar:size-2 group-data-[size=sm]/avatar:[&>svg]:hidden",
        "group-data-[size=default]/avatar:size-2.5 group-data-[size=default]/avatar:[&>svg]:size-2",
        "group-data-[size=lg]/avatar:size-3 group-data-[size=lg]/avatar:[&>svg]:size-2",
        className
      )}
      {...props}
    />
  )
}

function AvatarGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="avatar-group"
      className={cn(
        "group/avatar-group flex -space-x-2 *:data-[slot=avatar]:ring-2 *:data-[slot=avatar]:ring-background",
        className
      )}
      {...props}
    />
  )
}

function AvatarGroupCount({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="avatar-group-count"
      className={cn(
        "relative flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-sm text-muted-foreground ring-2 ring-background group-has-data-[size=lg]/avatar-group:size-10 group-has-data-[size=sm]/avatar-group:size-6 [&>svg]:size-4 group-has-data-[size=lg]/avatar-group:[&>svg]:size-5 group-has-data-[size=sm]/avatar-group:[&>svg]:size-3",
        className
      )}
      {...props}
    />
  )
}

export {
  Avatar,
  AvatarImage,
  AvatarFallback,
  AvatarBadge,
  AvatarGroup,
  AvatarGroupCount,
}
