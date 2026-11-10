/**
 * 必应每日一图（登录 / 注册页背景）。
 *
 * 只在服务端取元数据：HPImageArchive 不带跨域头，浏览器直接请求会被拦；放在服务端还能让登录页
 * 首屏就带上图片地址，不必等客户端再问一次。图片本身由浏览器直接从必应拉 ——
 * `th?id=…` 带 `Access-Control-Allow-Origin: *` 与 `Content-Length`，客户端据此显示真实下载进度。
 *
 * 返回的 `base` 是 UHD 原图（3840×2160）地址；客户端按屏幕像素追加 `w / h / rs / c / qlt` 取合适的尺寸，
 * 详见 `components/auth/auth-wallpaper.ts`。
 *
 * 只给服务端组件与路由处理器用。
 *
 * 缓存放在进程内存里而不是 fetch 数据缓存 / 页面 ISR：登录页若按 ISR 生成，镜像构建时连不上必应
 * 就会带着「无壁纸」上线、最长半小时后才补上。现在页面每次请求渲染，元数据成功的结果留 30 分钟，
 * 失败只记 1 分钟 —— 必应挂了也不会让每次打开登录页都等满超时。
 */

const ARCHIVE = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1&mkt=zh-CN&uhd=1";
const ORIGIN = "https://www.bing.com";
/** 元数据接口偶尔很慢；超时就按没有壁纸处理，登录页不为背景等待 */
const TIMEOUT_MS = 2500;
/** 一天一换；半小时刷新一次足够在换图后很快跟上 */
export const BING_WALLPAPER_REVALIDATE = 1800;
const FAILURE_TTL_MS = 60_000;

let memo: { value: BingWallpaper | null; expiresAt: number } | null = null;
let inflight: Promise<BingWallpaper | null> | null = null;

export type BingWallpaper = {
  /** UHD 原图地址，形如 https://www.bing.com/th?id=OHR.Xxx_ZH-CN123_UHD.jpg */
  base: string;
  /** 当日标题，如「惬意放松，尽享海岛时光」 */
  title: string;
  /** 图片说明，如「玫瑰岛航拍图，巴哈马」 */
  caption: string;
  /** 版权归属，如「Abstract Aerial Art/Getty Images」 */
  credit: string;
  /** 必应上关于这张图的搜索页 */
  link: string | null;
  /** 当日日期 YYYYMMDD */
  date: string;
};

type ArchiveImage = {
  urlbase?: string;
  title?: string;
  copyright?: string;
  copyrightlink?: string;
  startdate?: string;
};

/** urlbase 只认必应自家图库的形态，防止上游异常时拼出指向别处的地址 */
const URLBASE = /^\/th\?id=OHR\.[A-Za-z0-9_.-]+$/;
/** 「说明 (© 归属)」 */
const COPYRIGHT = /^(.*?)\s*[(（]\s*©\s*(.+?)\s*[)）]\s*$/;

export async function getBingWallpaper(): Promise<BingWallpaper | null> {
  if (memo && memo.expiresAt > Date.now()) return memo.value;
  // 并发的首个请求共用一次上游调用
  inflight ??= fetchWallpaper().then((value) => {
    memo = { value, expiresAt: Date.now() + (value ? BING_WALLPAPER_REVALIDATE * 1000 : FAILURE_TTL_MS) };
    inflight = null;
    return value;
  });
  return inflight;
}

async function fetchWallpaper(): Promise<BingWallpaper | null> {
  try {
    const response = await fetch(ARCHIVE, {
      cache: "no-store",
      headers: { "User-Agent": "Aegis Console/1.0" },
      signal: AbortSignal.timeout(TIMEOUT_MS)
    });
    if (!response.ok) return null;
    const payload = (await response.json()) as { images?: ArchiveImage[] };
    const image = payload.images?.[0];
    if (!image?.urlbase || !URLBASE.test(image.urlbase)) return null;

    const copyright = (image.copyright ?? "").trim();
    const match = COPYRIGHT.exec(copyright);
    const link = image.copyrightlink?.startsWith("https://www.bing.com/") ? image.copyrightlink : null;
    return {
      base: `${ORIGIN}${image.urlbase}_UHD.jpg`,
      title: (image.title ?? "").trim(),
      caption: (match ? match[1] : copyright).trim(),
      credit: match ? match[2] : "",
      link,
      date: image.startdate ?? ""
    };
  } catch {
    return null;
  }
}
