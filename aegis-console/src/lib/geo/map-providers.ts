// 底图供应商目录 —— 单一事实源
//
// 一条记录自述完这家供应商的全部事实：瓦片地址怎么拼、什么坐标系、
// 有没有原生深色版、注记能不能换语言、要不要密钥、版权怎么署。
// 渲染端（components/maps/maplibre-map.tsx）与选择器 UI 都只读这份目录，
// 任何一处再抄一份，就会出现「选得出来但画不上去」或者「能画但选不到」。
//
// ── 为什么要多家 ──
// 单一供应商在这个场景下必然有一半用户不可用：Carto / OSM / Esri 在中国大陆
// 时通时不通，高德 / 腾讯 / 天地图在境外又慢又缺注记。所以默认值跟着
// **浏览器语言**走（见 detectMapLocale），使用者仍可随时手动锁定某一家。
//
// ── 坐标系这件事必须写在目录里 ──
// 中国大陆的地图服务依法只提供 GCJ-02 偏移底图，与平台里 GeoIP 的 WGS-84
// 坐标相差 300–700 米。datum 字段驱动 gcj02-tiles.ts 的瓦片纠偏：
// 偏的是底图，业务数据一个坐标都不动。

// 同目录内用相对路径：这两个模块都不依赖打包器别名，
// 于是 scripts/map-providers.test.mjs 可以直接用 node 跑它们（见该文件开头）
import type { MapDatum } from "./datum";

export type MapTheme = "light" | "dark";
/** 注记语言。只有两档：供应商真正支持的也就这两档 */
export type MapLang = "zh-CN" | "en";
/** 优先哪一档供应商 —— 由浏览器语言推导，不是国界判断 */
export type MapRegionHint = "cn" | "global";
export type MapProviderGroup = "offline" | "global" | "china";

/** 一层瓦片。影像 + 注记这类组合会返回两层，按数组顺序自下而上叠放 */
export type MapTileLayer = {
  /** 同一供应商内唯一，用于拼 source / layer id */
  key: string;
  tiles: string[];
  tileSize: number;
  maxZoom: number;
  /** 腾讯等纵轴自下而上编号的服务 */
  scheme?: "tms";
  /** 透明注记层：叠在底图之上，且深色近似时不参与压暗 */
  overlay?: boolean;
};

export type MapProviderCredential = {
  /** 浏览器端环境变量名（必须是 NEXT_PUBLIC_ 前缀才能进产物） */
  env: string;
  /** 去哪儿申请 */
  apply: string;
};

export type MapProvider = {
  id: string;
  name: string;
  /** 控件上显示的短名 */
  short: string;
  description: string;
  group: MapProviderGroup;
  datum: MapDatum;
  attribution: string;
  /** 该供应商能给出哪些语言的注记 */
  langs: MapLang[];
  /** 供应商自带深色版。没有的话渲染端用栅格 paint 压暗近似（见 dimInDark） */
  nativeDark: boolean;
  credential?: MapProviderCredential;
  /** 解析出该主题 / 语言下要挂的瓦片层；离线简图返回空数组 */
  build(ctx: { theme: MapTheme; lang: MapLang; key: string }): MapTileLayer[];
};

// ──────────────────────────────────────
// 浏览器端密钥
// ──────────────────────────────────────

// NEXT_PUBLIC_* 必须写成字面量成员表达式，构建期才会被内联；
// 用变量名动态取值拿到的永远是 undefined。
const CREDENTIAL_VALUES: Record<string, string> = {
  NEXT_PUBLIC_MAP_TIANDITU_KEY: process.env.NEXT_PUBLIC_MAP_TIANDITU_KEY ?? "",
  NEXT_PUBLIC_MAP_MAPTILER_KEY: process.env.NEXT_PUBLIC_MAP_MAPTILER_KEY ?? "",
  NEXT_PUBLIC_MAP_STADIA_KEY: process.env.NEXT_PUBLIC_MAP_STADIA_KEY ?? ""
};

/** 该供应商的密钥；不需要密钥或未配置时返回空串 */
export function providerKey(provider: MapProvider): string {
  if (!provider.credential) return "";
  return CREDENTIAL_VALUES[provider.credential.env] ?? "";
}

/** 密钥是否已就位（不需要密钥的恒为 true） */
export function providerAvailable(provider: MapProvider): boolean {
  return !provider.credential || providerKey(provider).length > 0;
}

// ──────────────────────────────────────
// 目录
// ──────────────────────────────────────

/** 把 `{s}` 占位展开成多条地址：MapLibre 不认 `{s}`，只认 tiles 数组 */
function subdomains(template: string, hosts: readonly string[]): string[] {
  return hosts.map((h) => template.replace("{s}", h));
}

const ESRI = "https://server.arcgisonline.com/ArcGIS/rest/services";

export const MAP_PROVIDERS: MapProvider[] = [
  {
    id: "local",
    name: "本地简图",
    short: "简图",
    description: "内置国界矢量轮廓，不发起任何外部请求，断网与内网环境下恒可用",
    group: "offline",
    datum: "wgs84",
    attribution: "Natural Earth（公有领域）",
    langs: ["zh-CN", "en"],
    nativeDark: true,
    build: () => []
  },

  // ── 全球 ──
  {
    id: "carto",
    name: "CARTO Positron",
    short: "CARTO",
    description: "OpenStreetMap 数据的淡色 / 暗色底图，注记克制，最贴合控制台配色",
    group: "global",
    datum: "wgs84",
    attribution: "© OpenStreetMap 贡献者 · © CARTO",
    langs: ["en"],
    nativeDark: true,
    build: ({ theme }) => [
      {
        key: "base",
        tiles: subdomains(
          `https://{s}.basemaps.cartocdn.com/${theme === "dark" ? "dark_all" : "light_all"}/{z}/{x}/{y}@2x.png`,
          ["a", "b", "c", "d"]
        ),
        tileSize: 512,
        maxZoom: 19
      }
    ]
  },
  {
    id: "osm",
    name: "OpenStreetMap",
    short: "OSM",
    description: "OSM 官方标准图，注记最全；深色主题下由渲染端压暗近似",
    group: "global",
    datum: "wgs84",
    attribution: "© OpenStreetMap 贡献者",
    langs: ["en"],
    nativeDark: false,
    build: () => [
      { key: "base", tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"], tileSize: 256, maxZoom: 19 }
    ]
  },
  {
    id: "esri-gray",
    name: "Esri 灰底图",
    short: "Esri 灰",
    description: "Esri Canvas 灰阶底图，深浅两版原生分离；Carto 不通时的第二条线",
    group: "global",
    datum: "wgs84",
    attribution: "© Esri · HERE · Garmin · © OpenStreetMap 贡献者",
    langs: ["en"],
    nativeDark: true,
    build: ({ theme }) => {
      const flavor = theme === "dark" ? "Dark" : "Light";
      return [
        {
          key: "base",
          tiles: [`${ESRI}/Canvas/World_${flavor}_Gray_Base/MapServer/tile/{z}/{y}/{x}`],
          tileSize: 256,
          maxZoom: 16
        },
        {
          key: "label",
          tiles: [`${ESRI}/Canvas/World_${flavor}_Gray_Reference/MapServer/tile/{z}/{y}/{x}`],
          tileSize: 256,
          maxZoom: 16,
          overlay: true
        }
      ];
    }
  },
  {
    id: "esri-imagery",
    name: "Esri 卫星影像",
    short: "卫星",
    description: "全球高分辨率影像 + 半透明地名注记，用于确认某个坐标究竟落在什么地方",
    group: "global",
    datum: "wgs84",
    attribution: "© Esri · Maxar · Earthstar Geographics",
    langs: ["en"],
    nativeDark: true,
    build: () => [
      { key: "base", tiles: [`${ESRI}/World_Imagery/MapServer/tile/{z}/{y}/{x}`], tileSize: 256, maxZoom: 19 },
      {
        key: "label",
        tiles: [`${ESRI}/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}`],
        tileSize: 256,
        maxZoom: 19,
        overlay: true
      }
    ]
  },
  {
    id: "opentopo",
    name: "OpenTopoMap",
    short: "地形",
    description: "带等高线与地貌晕渲的地形图",
    group: "global",
    datum: "wgs84",
    attribution: "© OpenTopoMap（CC-BY-SA）· © OpenStreetMap 贡献者",
    langs: ["en"],
    nativeDark: false,
    build: () => [
      {
        key: "base",
        tiles: subdomains("https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png", ["a", "b", "c"]),
        tileSize: 256,
        maxZoom: 17
      }
    ]
  },
  {
    id: "maptiler",
    name: "MapTiler Streets",
    short: "MapTiler",
    description: "商用级街道图，深浅双版；免费额度足够管理端使用",
    group: "global",
    datum: "wgs84",
    attribution: "© MapTiler · © OpenStreetMap 贡献者",
    langs: ["en"],
    nativeDark: true,
    credential: { env: "NEXT_PUBLIC_MAP_MAPTILER_KEY", apply: "https://cloud.maptiler.com/account/keys/" },
    build: ({ theme, key }) => [
      {
        key: "base",
        tiles: [
          `https://api.maptiler.com/maps/streets-v2${theme === "dark" ? "-dark" : ""}/{z}/{x}/{y}@2x.png?key=${encodeURIComponent(key)}`
        ],
        tileSize: 512,
        maxZoom: 20
      }
    ]
  },
  {
    id: "stadia",
    name: "Stadia Alidade",
    short: "Stadia",
    description: "Stadia Maps 的极简底图，深浅双版，视觉上与 Carto 同一路数",
    group: "global",
    datum: "wgs84",
    attribution: "© Stadia Maps · © OpenMapTiles · © OpenStreetMap 贡献者",
    langs: ["en"],
    nativeDark: true,
    credential: { env: "NEXT_PUBLIC_MAP_STADIA_KEY", apply: "https://client.stadiamaps.com/dashboard/" },
    build: ({ theme, key }) => [
      {
        key: "base",
        tiles: [
          `https://tiles.stadiamaps.com/tiles/alidade_smooth${theme === "dark" ? "_dark" : ""}/{z}/{x}/{y}@2x.png?api_key=${encodeURIComponent(key)}`
        ],
        tileSize: 256,
        maxZoom: 20
      }
    ]
  },

  // ── 中国大陆 ──
  {
    id: "amap",
    name: "高德地图",
    short: "高德",
    description: "中国境内注记最全、访问最快的一档；中英文注记均可，坐标系 GCJ-02 由前端纠偏",
    group: "china",
    datum: "gcj02",
    attribution: "© 高德地图",
    langs: ["zh-CN", "en"],
    nativeDark: false,
    build: ({ lang }) => [
      {
        key: "base",
        tiles: subdomains(
          `https://webrd0{s}.is.autonavi.com/appmaptile?lang=${lang === "zh-CN" ? "zh_cn" : "en"}&size=1&style=8&x={x}&y={y}&z={z}`,
          ["1", "2", "3", "4"]
        ),
        tileSize: 256,
        maxZoom: 18
      }
    ]
  },
  {
    id: "amap-satellite",
    name: "高德卫星影像",
    short: "高德卫星",
    description: "高德影像底图 + 独立注记层，中国境内分辨率优于全球影像源",
    group: "china",
    datum: "gcj02",
    attribution: "© 高德地图",
    langs: ["zh-CN", "en"],
    nativeDark: true,
    build: ({ lang }) => {
      const l = lang === "zh-CN" ? "zh_cn" : "en";
      return [
        {
          key: "base",
          tiles: subdomains(
            `https://wprd0{s}.is.autonavi.com/appmaptile?x={x}&y={y}&z={z}&lang=${l}&size=1&scl=1&style=6`,
            ["1", "2", "3", "4"]
          ),
          tileSize: 256,
          maxZoom: 18
        },
        {
          key: "label",
          tiles: subdomains(
            `https://wprd0{s}.is.autonavi.com/appmaptile?x={x}&y={y}&z={z}&lang=${l}&size=1&scl=1&style=8`,
            ["1", "2", "3", "4"]
          ),
          tileSize: 256,
          maxZoom: 18,
          overlay: true
        }
      ];
    }
  },
  {
    id: "tencent",
    name: "腾讯地图",
    short: "腾讯",
    description: "高德之外的另一条境内线路，瓦片纵轴自下而上（TMS）；坐标系 GCJ-02 由前端纠偏",
    group: "china",
    datum: "gcj02",
    attribution: "© 腾讯地图",
    langs: ["zh-CN"],
    nativeDark: false,
    build: () => [
      {
        key: "base",
        tiles: subdomains("https://rt{s}.map.gtimg.com/tile?z={z}&x={x}&y={y}&styleid=1&scene=0", [
          "0",
          "1",
          "2",
          "3"
        ]),
        tileSize: 256,
        maxZoom: 18,
        scheme: "tms"
      }
    ]
  },
  {
    id: "tianditu",
    name: "天地图·矢量",
    short: "天地图",
    description: "国家地理信息公共服务平台。境内唯一 WGS-84 兼容（CGCS2000）的官方底图，无需纠偏，且注记层分中英文",
    group: "china",
    datum: "wgs84",
    attribution: "© 天地图 · 国家地理信息公共服务平台",
    langs: ["zh-CN", "en"],
    nativeDark: false,
    credential: { env: "NEXT_PUBLIC_MAP_TIANDITU_KEY", apply: "https://console.tianditu.gov.cn/api/key" },
    build: ({ lang, key }) => {
      const hosts = ["0", "1", "2", "3", "4", "5", "6", "7"];
      const tk = encodeURIComponent(key);
      return [
        {
          key: "base",
          tiles: subdomains(`https://t{s}.tianditu.gov.cn/DataServer?T=vec_w&x={x}&y={y}&l={z}&tk=${tk}`, hosts),
          tileSize: 256,
          maxZoom: 18
        },
        {
          key: "label",
          tiles: subdomains(
            `https://t{s}.tianditu.gov.cn/DataServer?T=${lang === "zh-CN" ? "cva" : "eva"}_w&x={x}&y={y}&l={z}&tk=${tk}`,
            hosts
          ),
          tileSize: 256,
          maxZoom: 18,
          overlay: true
        }
      ];
    }
  },
  {
    id: "tianditu-satellite",
    name: "天地图·影像",
    short: "天地图影像",
    description: "天地图影像底图 + 中英文注记层，同样是 WGS-84 兼容坐标系",
    group: "china",
    datum: "wgs84",
    attribution: "© 天地图 · 国家地理信息公共服务平台",
    langs: ["zh-CN", "en"],
    nativeDark: true,
    credential: { env: "NEXT_PUBLIC_MAP_TIANDITU_KEY", apply: "https://console.tianditu.gov.cn/api/key" },
    build: ({ lang, key }) => {
      const hosts = ["0", "1", "2", "3", "4", "5", "6", "7"];
      const tk = encodeURIComponent(key);
      return [
        {
          key: "base",
          tiles: subdomains(`https://t{s}.tianditu.gov.cn/DataServer?T=img_w&x={x}&y={y}&l={z}&tk=${tk}`, hosts),
          tileSize: 256,
          maxZoom: 18
        },
        {
          key: "label",
          tiles: subdomains(
            `https://t{s}.tianditu.gov.cn/DataServer?T=${lang === "zh-CN" ? "cia" : "eia"}_w&x={x}&y={y}&l={z}&tk=${tk}`,
            hosts
          ),
          tileSize: 256,
          maxZoom: 18,
          overlay: true
        }
      ];
    }
  }
];

export const PROVIDER_BY_ID = new Map(MAP_PROVIDERS.map((p) => [p.id, p]));

export const OFFLINE_PROVIDER = PROVIDER_BY_ID.get("local") as MapProvider;

export const GROUP_LABELS: Record<MapProviderGroup, string> = {
  offline: "离线",
  global: "全球",
  china: "中国大陆"
};

// ──────────────────────────────────────
// 浏览器语言 → 注记语言 + 供应商倾向
// ──────────────────────────────────────

export type MapLocale = { lang: MapLang; region: MapRegionHint; tag: string };

function browserLanguages(): readonly string[] {
  if (typeof navigator === "undefined") return [];
  if (navigator.languages?.length) return navigator.languages;
  return navigator.language ? [navigator.language] : [];
}

/**
 * 从浏览器语言推导注记语言与供应商倾向。
 *
 * 两个维度刻意分开：简体中文（zh-CN / zh-Hans / 裸 zh）几乎等同于「人在中国大陆」，
 * 走境内供应商；繁体中文（zh-TW / zh-HK）虽然也要中文注记，但走境内线路只会更慢，
 * 因此仍归到全球档。合并成一个维度必然要牺牲其中一边。
 *
 * `languages` 只为测试留的入口，运行时一律读浏览器。
 */
export function detectMapLocale(languages: readonly string[] = browserLanguages()): MapLocale {
  for (const item of languages) {
    const tag = item.toLowerCase();
    if (!tag) continue;
    if (tag.startsWith("zh")) {
      const mainland = tag === "zh" || tag.startsWith("zh-cn") || tag.startsWith("zh-hans");
      return { lang: "zh-CN", region: mainland ? "cn" : "global", tag: item };
    }
    // 非中文的第一条有效语言即可决定：浏览器已按用户偏好排好序
    if (/^[a-z]{2,3}(-|$)/.test(tag)) return { lang: "en", region: "global", tag: item };
  }
  return { lang: "en", region: "global", tag: "en" };
}

/** 自动档的优先级：第一家密钥就位的即为结果 */
const AUTO_PREFERENCE: Record<MapRegionHint, string[]> = {
  cn: ["amap", "tianditu", "carto", "local"],
  global: ["carto", "esri-gray", "osm", "local"]
};

export function resolveAutoProvider(region: MapRegionHint): MapProvider {
  for (const id of AUTO_PREFERENCE[region]) {
    const provider = PROVIDER_BY_ID.get(id);
    if (provider && providerAvailable(provider)) return provider;
  }
  return OFFLINE_PROVIDER;
}

/** 偏好值：`auto` 或某个供应商 id */
export type MapProviderPreference = string;
export const AUTO_PREFERENCE_VALUE = "auto";

/**
 * 偏好 → 实际供应商。
 *
 * 显式选择优先于语言推导；选中的那家缺密钥或已下架时退回自动档 ——
 * 界面上不能出现「选了却画不出来」的静默状态。
 */
export function resolveProvider(pref: MapProviderPreference, region: MapRegionHint): MapProvider {
  if (pref && pref !== AUTO_PREFERENCE_VALUE) {
    const explicit = PROVIDER_BY_ID.get(pref);
    if (explicit && providerAvailable(explicit)) return explicit;
  }
  return resolveAutoProvider(region);
}

/** 该供应商在这个语言下实际给出的注记语言（不支持时退回它自己的第一档） */
export function effectiveLang(provider: MapProvider, lang: MapLang): MapLang {
  return provider.langs.includes(lang) ? lang : provider.langs[0];
}
