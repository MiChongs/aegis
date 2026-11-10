import { NextResponse } from "next/server";
import { BING_WALLPAPER_REVALIDATE, getBingWallpaper } from "@/lib/bing-wallpaper";

/**
 * 必应每日一图元数据。登录页在服务端直接调 `getBingWallpaper`，这里留给需要在客户端取的地方。
 * `imageUrl` 是 UHD 原图（3840×2160）；取其它尺寸见 `components/auth/auth-wallpaper.ts`。
 */
export const dynamic = "force-dynamic";

export async function GET() {
  const wallpaper = await getBingWallpaper();
  return NextResponse.json(
    wallpaper
      ? { ok: true, imageUrl: wallpaper.base, ...wallpaper, copyright: [wallpaper.caption, wallpaper.credit].filter(Boolean).join(" © ") }
      : { ok: false, imageUrl: null },
    { headers: { "Cache-Control": `public, max-age=${BING_WALLPAPER_REVALIDATE}` } }
  );
}
