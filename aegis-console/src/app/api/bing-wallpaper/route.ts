import { NextResponse } from "next/server";

const BING_ENDPOINT = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1&mkt=zh-CN";
const FALLBACK_COLORS = ["#0b1320", "#0d1b2a", "#111827"];

type BingArchiveResponse = {
  images?: Array<{
    url?: string;
    title?: string;
    copyright?: string;
  }>;
};

export const revalidate = 3600;

export async function GET() {
  try {
    const response = await fetch(BING_ENDPOINT, {
      next: { revalidate: 3600 },
      headers: {
        "User-Agent": "Aegis Console/1.0"
      }
    });

    if (!response.ok) {
      throw new Error(`bing upstream ${response.status}`);
    }

    const payload = (await response.json()) as BingArchiveResponse;
    const image = payload.images?.[0];

    if (!image?.url) {
      throw new Error("bing image missing");
    }

    const imageUrl = image.url.startsWith("http") ? image.url : `https://www.bing.com${image.url}`;

    return NextResponse.json({
      ok: true,
      imageUrl,
      title: image.title ?? null,
      copyright: image.copyright ?? null,
      fallbackColors: FALLBACK_COLORS
    });
  } catch {
    return NextResponse.json({
      ok: false,
      imageUrl: null,
      title: null,
      copyright: null,
      fallbackColors: FALLBACK_COLORS
    });
  }
}
