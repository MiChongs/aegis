/**
 * 在浏览器里解析安装包，提取发布表单需要的全部信息。文件不离开本机。
 *
 *   APK   清单（包名、版本、SDK、权限）、资源表（应用名、图标）、lib/ 下的 ABI、v2/v3 签名证书
 *   AAB   protobuf 清单、base/lib/ 下的 ABI（应用名需要编译后的资源表，取不到时用包名）
 *   IPA   Info.plist（Bundle ID、版本、最低系统、应用名）
 *   HAP   module.json（包名、版本、API 版本）、libs/ 下的 ABI
 *   其他  exe、msi、dmg、AppImage、deb、rpm 等按扩展名判断平台，从文件名里找版本号
 */

import { ResourceTable, attrInt, attrString, findChildren, parseBinaryXml, type XmlElement } from "./android";
import { parsePlist, parseProtoXml, readApkSigner, type PbElement, type PlistValue, type SignerInfo } from "./formats";
import { vectorToSvg } from "./vector";
import { ZipReader } from "./zip";

export type PackageKind = "apk" | "aab" | "ipa" | "hap" | "desktop" | "unknown";

export type PackageIcon = {
  /** 图标或自适应图标前景的 object URL */
  src?: string;
  /** 自适应图标的背景色（CSS） */
  background?: string;
  /** 自适应图标：前景需要放大到 108/72 才与系统显示一致 */
  adaptive?: boolean;
};

export type PackageInfo = {
  kind: PackageKind;
  platform: string;
  fileName: string;
  fileSize: number;
  appName?: string;
  packageName?: string;
  versionName?: string;
  versionCode?: number;
  minSdk?: number;
  targetSdk?: number;
  compileSdk?: number;
  /** 给人看的最低系统要求，如 Android 7.0 */
  minOsLabel?: string;
  /** 包里带的原生库架构；空数组表示没有原生库（任何架构都能装） */
  nativeAbis: string[];
  /** 建议的安装包类型（填进 abi 字段） */
  abi: string;
  icon?: PackageIcon;
  permissions: string[];
  features: string[];
  signer?: SignerInfo | null;
  /** 解析过程中值得提醒的事项 */
  warnings: string[];
};

const ANDROID_VERSIONS: Record<number, string> = {
  21: "5.0", 22: "5.1", 23: "6.0", 24: "7.0", 25: "7.1", 26: "8.0", 27: "8.1", 28: "9", 29: "10",
  30: "11", 31: "12", 32: "12L", 33: "13", 34: "14", 35: "15", 36: "16", 37: "17"
};

export function androidVersionLabel(api?: number) {
  if (!api) return undefined;
  const name = ANDROID_VERSIONS[api];
  return name ? `Android ${name}（API ${api}）` : `API ${api}`;
}

const KNOWN_ABIS = ["arm64-v8a", "armeabi-v7a", "armeabi", "x86_64", "x86", "riscv64", "mips", "mips64"];

function extOf(name: string) {
  const lower = name.toLowerCase();
  if (lower.endsWith(".appimage")) return ".appimage";
  const dot = lower.lastIndexOf(".");
  return dot >= 0 ? lower.slice(dot) : "";
}

/** 从文件名里猜版本号与架构：Voyage-1.4.2-x64-setup.exe → 1.4.2、x64 */
function fromFileName(name: string) {
  const version = name.match(/(?:^|[^\d])(\d+\.\d+(?:\.\d+){0,2})(?:[^\d]|$)/)?.[1];
  const lower = name.toLowerCase();
  const arch =
    ["arm64-v8a", "armeabi-v7a", "x86_64", "universal"].find((a) => lower.includes(a)) ??
    (/(?:^|[-_.])(arm64|aarch64)(?:[-_.]|$)/.test(lower)
      ? "arm64"
      : /(?:^|[-_.])(x64|amd64|x86-64)(?:[-_.]|$)/.test(lower)
        ? "x64"
        : /(?:^|[-_.])(x86|i386|i686|win32)(?:[-_.]|$)/.test(lower)
          ? "x86"
          : undefined);
  return { version, arch };
}

function abiForAndroid(nativeAbis: string[]) {
  // 只带一种架构才是分架构包；没有原生库或多架构合包都算通用包
  return nativeAbis.length === 1 && nativeAbis[0] !== "armeabi" ? nativeAbis[0] : "universal";
}

function collectAbis(zip: ZipReader, prefix: string) {
  const abis = new Set<string>();
  for (const name of zip.names()) {
    if (!name.startsWith(prefix)) continue;
    const abi = name.slice(prefix.length).split("/")[0];
    if (abi && name.endsWith(".so")) abis.add(abi);
  }
  return KNOWN_ABIS.filter((abi) => abis.has(abi)).concat([...abis].filter((abi) => !KNOWN_ABIS.includes(abi)));
}

function blobUrl(bytes: Uint8Array, path: string) {
  const type = path.endsWith(".webp") ? "image/webp" : path.endsWith(".jpg") || path.endsWith(".jpeg") ? "image/jpeg" : "image/png";
  return URL.createObjectURL(new Blob([bytes as Uint8Array<ArrayBuffer>], { type }));
}

function argbToCss(argb: number) {
  const a = ((argb >>> 24) & 0xff) / 255;
  return `rgba(${(argb >>> 16) & 0xff}, ${(argb >>> 8) & 0xff}, ${argb & 0xff}, ${a.toFixed(3)})`;
}

const BITMAP_RE = /\.(png|webp|jpe?g)$/i;

type IconLayer = { url?: string; color?: string };

function svgUrl(svg: string) {
  return URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
}

/** 一个 drawable 资源 → 可显示的图层：颜色、位图或矢量图（转 SVG）。 */
async function resolveLayer(zip: ZipReader, table: ResourceTable, ref: number, depth = 0): Promise<IconLayer | undefined> {
  if (depth > 3) return undefined;
  const color = table.resolveColor(ref);
  if (color !== undefined) return { color: argbToCss(color) };
  const files = table.resolveFiles(ref).filter((f) => zip.has(f.path));
  const bitmap = files.find((f) => BITMAP_RE.test(f.path));
  if (bitmap) {
    const bytes = await zip.read(bitmap.path, 4 * 1024 * 1024);
    if (bytes) return { url: blobUrl(bytes, bitmap.path) };
  }
  for (const file of files.filter((f) => f.path.endsWith(".xml"))) {
    const bytes = await zip.read(file.path, 1024 * 1024);
    const root = bytes ? parseBinaryXml(bytes) : null;
    if (!root) continue;
    if (root.name === "vector") {
      const svg = await vectorToSvg(zip, table, root);
      if (svg) return { url: svgUrl(svg) };
    }
    // <bitmap android:src="@drawable/x"/>、<inset android:drawable="..."/> 之类的包装
    const inner = root.attrs.src ?? root.attrs.drawable;
    if (inner?.kind === "ref") {
      const layer = await resolveLayer(zip, table, inner.value, depth + 1);
      if (layer) return layer;
    }
  }
  return undefined;
}

/**
 * 从图标资源 ID 解析出可显示的图片。
 *
 * 先看自适应图标（Android 8 起系统显示的就是它）：前景、背景各自可能是位图、矢量图或颜色；
 * 没有自适应图标再取最高密度的位图，最后按资源名找同名位图兜底。
 */
async function resolveAndroidIcon(zip: ZipReader, table: ResourceTable | null, iconRef: number | undefined): Promise<PackageIcon | undefined> {
  if (!table || !iconRef) return undefined;
  const files = table.resolveFiles(iconRef).filter((f) => zip.has(f.path));
  for (const file of files.filter((f) => f.path.endsWith(".xml"))) {
    const bytes = await zip.read(file.path, 1024 * 1024);
    const root = bytes ? parseBinaryXml(bytes) : null;
    if (root?.name !== "adaptive-icon") continue;
    const ref = (tag: string) => {
      const v = findChildren(root, tag)[0]?.attrs.drawable;
      return v?.kind === "ref" ? v.value : undefined;
    };
    const fgRef = ref("foreground");
    const bgRef = ref("background");
    const fg = fgRef ? await resolveLayer(zip, table, fgRef) : undefined;
    const bg = bgRef ? await resolveLayer(zip, table, bgRef) : undefined;
    if (fg?.url || bg?.url) {
      const background = bg?.color ?? (bg?.url ? `center / cover no-repeat url("${bg.url}")` : undefined);
      return { src: fg?.url, background, adaptive: true };
    }
  }
  const layer = await resolveLayer(zip, table, iconRef);
  if (layer?.url) return { src: layer.url };
  const key = table.keyName(iconRef);
  if (key) {
    const candidates = zip
      .names()
      .filter((n) => n.startsWith("res/") && BITMAP_RE.test(n) && n.split("/").pop()?.replace(BITMAP_RE, "") === key);
    const best = candidates.sort((a, b) => (zip.entries.get(b)?.size ?? 0) - (zip.entries.get(a)?.size ?? 0))[0];
    if (best) {
      const bytes = await zip.read(best, 4 * 1024 * 1024);
      if (bytes) return { src: blobUrl(bytes, best) };
    }
  }
  return undefined;
}

async function inspectApk(zip: ZipReader, info: PackageInfo) {
  const manifestBytes = await zip.read("AndroidManifest.xml");
  const manifest = manifestBytes ? parseBinaryXml(manifestBytes) : null;
  if (!manifest) {
    info.warnings.push("没有找到可解析的 AndroidManifest.xml");
    return;
  }
  let table: ResourceTable | null = null;
  try {
    const arsc = await zip.read("resources.arsc", 128 * 1024 * 1024);
    table = arsc ? ResourceTable.parse(arsc) : null;
  } catch {
    info.warnings.push("资源表解析失败，应用名与图标可能缺失");
  }
  applyAndroidManifest(manifest, info, (ref) => table?.resolveString(ref));
  const app = findChildren(manifest, "application")[0];
  const iconValue = app?.attrs.icon ?? app?.attrs.roundIcon;
  info.icon = await resolveAndroidIcon(zip, table, iconValue?.kind === "ref" ? iconValue.value : undefined);
  info.nativeAbis = collectAbis(zip, "lib/");
  info.abi = abiForAndroid(info.nativeAbis);
  try {
    info.signer = await readApkSigner(zip);
    if (!info.signer) info.warnings.push(zip.names().some((n) => /^META-INF\/.+\.(RSA|DSA|EC)$/i.test(n)) ? "只有 v1 签名，Android 11 起部分设备会拒绝安装" : "安装包未签名，无法安装");
  } catch {
    info.signer = null;
  }
  if (attrString(app, "debuggable") === "true") info.warnings.push("这是可调试（debuggable）版本");
  if (app?.attrs.testOnly && (app.attrs.testOnly as { value: unknown }).value === true) info.warnings.push("这是 testOnly 安装包，普通方式无法安装");
}

function applyAndroidManifest(manifest: XmlElement, info: PackageInfo, resolve: (ref: number) => string | undefined) {
  info.packageName = attrString(manifest, "package");
  info.versionCode = attrInt(manifest, "versionCode");
  const versionName = manifest.attrs.versionName;
  info.versionName = versionName?.kind === "ref" ? resolve(versionName.value) : attrString(manifest, "versionName");
  info.compileSdk = attrInt(manifest, "compileSdkVersion");
  const sdk = findChildren(manifest, "uses-sdk")[0];
  info.minSdk = attrInt(sdk, "minSdkVersion") ?? 1;
  info.targetSdk = attrInt(sdk, "targetSdkVersion") ?? info.minSdk;
  info.minOsLabel = androidVersionLabel(info.minSdk);
  const app = findChildren(manifest, "application")[0];
  const label = app?.attrs.label;
  info.appName = label?.kind === "ref" ? resolve(label.value) : label?.kind === "string" ? label.value : undefined;
  info.permissions = findChildren(manifest, "uses-permission")
    .concat(findChildren(manifest, "uses-permission-sdk-23"))
    .map((el) => attrString(el, "name") ?? "")
    .filter(Boolean);
  const features = findChildren(manifest, "uses-feature");
  info.features = features.map((el) => attrString(el, "name") ?? "").filter(Boolean);
  if (features.some((el) => attrString(el, "name") === LEANBACK && attrString(el, "required") !== "false")) {
    info.platform = "android_tv";
  }
}

/**
 * 电视专用包的标志：清单里必需（required 不为 false）的 android.software.leanback。手机包要么不声明，要么声明成非必需；
 * 这样的包平台填 android_tv，检测时只发给报 android_tv 的电视版，不会被当成手机升级包。
 */
const LEANBACK = "android.software.leanback";

function pbChildren(el: PbElement | undefined, name: string) {
  return el?.children.filter((c) => c.name === name) ?? [];
}

async function inspectAab(zip: ZipReader, info: PackageInfo) {
  const bytes = await zip.read("base/manifest/AndroidManifest.xml");
  const manifest = bytes ? parseProtoXml(bytes) : null;
  if (!manifest) {
    info.warnings.push("没有找到可解析的清单");
    return;
  }
  const num = (v: unknown) => (typeof v === "number" ? v : typeof v === "string" && /^\d+$/.test(v) ? Number(v) : undefined);
  info.packageName = String(manifest.attrs.package ?? "") || undefined;
  info.versionCode = num(manifest.attrs.versionCode);
  info.versionName = typeof manifest.attrs.versionName === "string" ? manifest.attrs.versionName : undefined;
  const sdk = pbChildren(manifest, "uses-sdk")[0];
  info.minSdk = num(sdk?.attrs.minSdkVersion);
  info.targetSdk = num(sdk?.attrs.targetSdkVersion);
  info.minOsLabel = androidVersionLabel(info.minSdk);
  const label = pbChildren(manifest, "application")[0]?.attrs.label;
  if (typeof label === "string" && label && !label.startsWith("@")) info.appName = label;
  info.permissions = pbChildren(manifest, "uses-permission").map((el) => String(el.attrs.name ?? "")).filter(Boolean);
  const features = pbChildren(manifest, "uses-feature");
  info.features = features.map((el) => String(el.attrs.name ?? "")).filter(Boolean);
  if (features.some((el) => el.attrs.name === LEANBACK && el.attrs.required !== "false" && el.attrs.required !== 0)) {
    info.platform = "android_tv";
  }
  info.nativeAbis = collectAbis(zip, "base/lib/");
  info.abi = "universal";
  info.warnings.push("AAB 需由应用商店生成安装包，手机无法直接安装");
}

async function inspectIpa(zip: ZipReader, info: PackageInfo) {
  const plistPath = zip.names().find((n) => /^Payload\/[^/]+\.app\/Info\.plist$/.test(n));
  if (!plistPath) {
    info.warnings.push("没有找到 Info.plist");
    return;
  }
  const bytes = await zip.read(plistPath);
  const plist = (bytes ? parsePlist(bytes) : null) as Record<string, PlistValue> | null;
  if (!plist) return;
  const str = (key: string) => (typeof plist[key] === "string" ? (plist[key] as string) : undefined);
  info.packageName = str("CFBundleIdentifier");
  info.appName = str("CFBundleDisplayName") ?? str("CFBundleName");
  info.versionName = str("CFBundleShortVersionString");
  const build = str("CFBundleVersion");
  if (build && /^\d+$/.test(build)) info.versionCode = Number(build);
  else if (build) info.warnings.push(`构建号「${build}」不是整数，请手动填写版本码`);
  const minOs = str("MinimumOSVersion");
  if (minOs) info.minOsLabel = `iOS ${minOs}`;
  info.abi = "universal";
  // IPA 里的 PNG 是苹果私有压缩格式（CgBI），浏览器无法直接显示，不取图标
}

async function inspectHap(zip: ZipReader, info: PackageInfo) {
  const text = (await zip.readText("module.json")) ?? (await zip.readText("config.json"));
  if (!text) {
    info.warnings.push("没有找到 module.json");
    return;
  }
  try {
    const json = JSON.parse(text) as { app?: Record<string, unknown> };
    const app = json.app ?? {};
    const version = (app.version ?? {}) as Record<string, unknown>;
    info.packageName = typeof app.bundleName === "string" ? app.bundleName : undefined;
    info.versionName = typeof app.versionName === "string" ? app.versionName : typeof version.name === "string" ? version.name : undefined;
    const code = app.versionCode ?? version.code;
    info.versionCode = typeof code === "number" ? code : undefined;
    const minApi = app.minAPIVersion ?? (app.apiVersion as Record<string, unknown> | undefined)?.compatible;
    if (typeof minApi === "number") {
      info.minSdk = minApi;
      info.minOsLabel = `HarmonyOS API ${minApi}`;
    }
    if (typeof app.targetAPIVersion === "number") info.targetSdk = app.targetAPIVersion;
    if (typeof app.label === "string" && !app.label.startsWith("$")) info.appName = app.label;
  } catch {
    info.warnings.push("module.json 格式错误");
  }
  info.nativeAbis = collectAbis(zip, "libs/");
  info.abi = abiForAndroid(info.nativeAbis);
}

const DESKTOP: Record<string, string> = {
  ".exe": "windows",
  ".msi": "windows",
  ".msix": "windows",
  ".appx": "windows",
  ".dmg": "macos",
  ".pkg": "macos",
  ".appimage": "linux",
  ".deb": "linux",
  ".rpm": "linux"
};

/** 解析一个安装包。抛出的错误都带可读的中文说明。 */
export async function inspectPackage(file: File): Promise<PackageInfo> {
  const ext = extOf(file.name);
  const fromName = fromFileName(file.name);
  const info: PackageInfo = {
    kind: "unknown",
    platform: "android",
    fileName: file.name,
    fileSize: file.size,
    nativeAbis: [],
    abi: fromName.arch ?? "universal",
    permissions: [],
    features: [],
    warnings: []
  };

  if (DESKTOP[ext]) {
    info.kind = "desktop";
    info.platform = DESKTOP[ext];
    info.versionName = fromName.version;
    if (!fromName.version) info.warnings.push("桌面安装包无法读取内部版本信息，请确认版本号");
    return info;
  }

  if ([".apk", ".apks", ".aab", ".ipa", ".hap", ".zip"].includes(ext)) {
    let zip: ZipReader;
    try {
      zip = await ZipReader.open(file);
    } catch {
      throw new Error("文件不是有效的安装包（无法按 ZIP 读取）");
    }
    if (ext === ".aab" || zip.has("base/manifest/AndroidManifest.xml")) {
      info.kind = "aab";
      await inspectAab(zip, info);
    } else if (zip.has("AndroidManifest.xml")) {
      info.kind = "apk";
      await inspectApk(zip, info);
    } else if (ext === ".ipa" || zip.names().some((n) => n.startsWith("Payload/"))) {
      info.kind = "ipa";
      info.platform = "ios";
      await inspectIpa(zip, info);
    } else if (ext === ".hap" || zip.has("module.json")) {
      info.kind = "hap";
      info.platform = "harmony";
      await inspectHap(zip, info);
    } else {
      info.versionName = fromName.version;
      info.platform = "all";
    }
    return info;
  }

  info.versionName = fromName.version;
  info.platform = "all";
  info.warnings.push("无法识别的文件类型，只记录文件本身");
  return info;
}

/** 释放解析时为图标创建的 object URL。 */
export function releasePackageInfo(info?: PackageInfo | null) {
  if (info?.icon?.src?.startsWith("blob:")) URL.revokeObjectURL(info.icon.src);
  const bg = info?.icon?.background?.match(/url\("(blob:[^"]+)"\)/)?.[1];
  if (bg) URL.revokeObjectURL(bg);
}
