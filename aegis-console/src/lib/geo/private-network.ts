// 内网 / 特殊用途地址识别。
//
// 为什么前端也要判一次：后端 `isPrivate` 只在解析过位置的接口上有
// （会话详情、单用户登录审计），而同一个 IP 会出现在没走 GeoIP 的地方；
// 且后端那一位是 Go `netip` 的粗粒度结论（loopback / private / link-local 合成一位），
// 界面上要区分「本机回环」和「局域网」才能说清这条记录到底是什么。
//
// 判据来自 RFC 1918 / 4193 / 3927 / 6598 / 5735，与后端 isPrivateAddr 对齐；
// 唯一刻意不同的是 CGNAT（100.64/10）：Go 的 IsPrivate 不认它，它也确实不是内网，
// 而是运营商级 NAT —— 归成「服务器地址」会把一批真实的移动网络用户错误地画到机房里。

export type IpScope =
  | "public" // 公网
  | "loopback" // 127.0.0.0/8、::1
  | "private" // RFC1918 / RFC4193 ULA
  | "linkLocal" // 169.254/16、fe80::/10
  | "cgnat" // 100.64/10 运营商级 NAT
  | "unspecified" // 0.0.0.0、::
  | "unknown"; // 解析不出来（空串 / 畸形）

export type IpClass = {
  scope: IpScope;
  /** 是否应归入「服务器地址」节点：这类地址没有地理含义，位置就是服务端自己 */
  server: boolean;
  /** 展示名 */
  label: string;
  /** 网段说明，用于 tooltip 里解释「为什么它在这儿」 */
  detail: string;
};

const CLASSES: Record<IpScope, Omit<IpClass, "scope">> = {
  public: { server: false, label: "公网地址", detail: "" },
  loopback: { server: true, label: "服务器地址", detail: "本机回环（127.0.0.0/8 · ::1）" },
  private: { server: true, label: "服务器地址", detail: "内网网段（RFC 1918 · ULA）" },
  linkLocal: { server: true, label: "服务器地址", detail: "链路本地（169.254/16 · fe80::/10）" },
  cgnat: { server: false, label: "运营商 NAT", detail: "CGNAT 共享网段（100.64/10）" },
  unspecified: { server: true, label: "服务器地址", detail: "未指定地址（0.0.0.0 · ::）" },
  unknown: { server: false, label: "未知来源", detail: "" }
};

/** 去掉 IPv6 方括号、zone id 与 IPv4 的端口后缀 */
function normalize(raw: string): string {
  let ip = raw.trim();
  if (ip.startsWith("[")) ip = ip.slice(1, ip.indexOf("]") > 0 ? ip.indexOf("]") : undefined);
  const zone = ip.indexOf("%");
  if (zone >= 0) ip = ip.slice(0, zone);
  // "1.2.3.4:8080" —— 仅当只有一个冒号时才当端口（IPv6 冒号必然多于一个）
  if (ip.split(":").length === 2 && ip.includes(".")) ip = ip.slice(0, ip.indexOf(":"));
  return ip.toLowerCase();
}

function parseV4(ip: string): number[] | null {
  const parts = ip.split(".");
  if (parts.length !== 4) return null;
  const nums: number[] = [];
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p)) return null;
    const n = Number(p);
    if (n > 255) return null;
    nums.push(n);
  }
  return nums;
}

function scopeOfV4([a, b]: number[]): IpScope {
  if (a === 0) return "unspecified";
  if (a === 127) return "loopback";
  if (a === 10) return "private";
  if (a === 172 && b >= 16 && b <= 31) return "private";
  if (a === 192 && b === 168) return "private";
  if (a === 169 && b === 254) return "linkLocal";
  if (a === 100 && b >= 64 && b <= 127) return "cgnat";
  return "public";
}

function scopeOfV6(ip: string): IpScope {
  if (ip === "::" || ip === "::0") return "unspecified";
  if (ip === "::1") return "loopback";
  // ::ffff:1.2.3.4 / ::1.2.3.4 —— IPv4 映射地址按 IPv4 判
  const mapped = ip.match(/^::(?:ffff:)?(\d{1,3}(?:\.\d{1,3}){3})$/);
  if (mapped) {
    const v4 = parseV4(mapped[1]);
    if (v4) return scopeOfV4(v4);
  }
  const head = ip.split(":")[0];
  if (!head) return "public";
  const first = Number.parseInt(head.padStart(4, "0").slice(0, 2), 16);
  if (Number.isNaN(first)) return "public";
  if (first === 0xfc || first === 0xfd) return "private"; // fc00::/7 ULA
  if (first === 0xfe) {
    const second = Number.parseInt(head.padStart(4, "0").slice(2, 3), 16);
    if (second >= 0x8 && second <= 0xb) return "linkLocal"; // fe80::/10
  }
  return "public";
}

/** 判定一个 IP 的作用域。空串 / 畸形返回 unknown。 */
export function classifyIp(ip?: string | null): IpClass {
  const raw = (ip ?? "").trim();
  if (!raw) return { scope: "unknown", ...CLASSES.unknown };
  const norm = normalize(raw);
  const v4 = parseV4(norm);
  const scope = v4 ? scopeOfV4(v4) : norm.includes(":") ? scopeOfV6(norm) : "unknown";
  return { scope, ...CLASSES[scope] };
}

/**
 * 这条记录是否应画在「服务器地址」节点上。
 *
 * 后端的 `isPrivate` 优先（它拿到的是原始 IP，且与 GeoIP 解析同源），
 * 前端分类作为兜底：字段可能来自没做位置解析的接口，或旧版本响应。
 */
export function isServerSideAddress(ip?: string | null, backendPrivate?: boolean): boolean {
  if (backendPrivate) return true;
  return classifyIp(ip).server;
}
