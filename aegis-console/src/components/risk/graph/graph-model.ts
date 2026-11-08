/**
 * 关系图的数据层：把风控平台返回的子图整理成「给人看」的样子。
 *
 * - 折叠：只被一个人用过的 IP（比如手机网络三个月里换过的几十个地址）合并成一个「N 个 IP」点，点开再展开；
 *   被多人用过的 IP 单独保留——它们才是有信息量的关联。
 * - 归纳：从图里算出「谁和 TA 有关联、为什么」，按关联强弱排序，供叙述式摘要和关联人列表用。
 * - 文案：节点、关系的说明一律转成人话（「被 3 个账号使用过」「多人共用的出口」），不直接摆字段名。
 */
import type { GraphLink, GraphNode, GraphNodeKind, Subgraph } from "@/lib/api/risk";
import { formatDateTime, formatRelative } from "@/lib/datetime";

export type ViewKind = GraphNodeKind | "ip_group";

export interface ViewNode {
  id: string;
  kind: ViewKind;
  label: string;
  /** 原始节点（折叠点没有） */
  node?: GraphNode;
  /** 折叠点里的 IP */
  members?: GraphNode[];
  depth: number;
  center: boolean;
  banned: boolean;
  weak: boolean;
  /** 节点上的小标记（已封禁、模拟器、共享出口……） */
  badges: string[];
}

export interface ViewEdge {
  id: string;
  source: string;
  target: string;
  kind: string;
  count: number;
  firstSeen: number;
  lastSeen: number;
  weak: boolean;
}

export const KIND_LABEL: Record<ViewKind, string> = {
  user: "用户", account: "登录名", device: "设备", ip: "IP", subnet: "网段", phone: "手机号", email: "邮箱",
  identity: "第三方账号", payer: "付款账号", passkey: "通行密钥", client: "授权应用", ip_group: "IP 历史",
};

export const KIND_COLOR: Record<ViewKind, string> = {
  user: "#6366f1", account: "#8b5cf6", device: "#0ea5e9", ip: "#10b981", subnet: "#14b8a6", phone: "#f59e0b",
  email: "#ec4899", identity: "#2563eb", payer: "#d97706", passkey: "#059669", client: "#7c3aed", ip_group: "#34d399",
};

/** 图例与筛选的顺序 */
export const KIND_ORDER: ViewKind[] = ["user", "account", "device", "ip", "ip_group", "identity", "phone", "email", "payer", "passkey", "client", "subnet"];

export const WEAK_COLOR = "#94a3b8";

/** 关系的说法：「A 使用设备 B」 */
export const EDGE_VERB: Record<string, string> = {
  uses_device: "使用过设备", uses_ip: "用过 IP", in_subnet: "属于网段", has_phone: "绑定手机号", has_email: "绑定邮箱",
  account_of: "的登录名是", invited: "邀请了", targets: "互动过", registered_from: "注册时的 IP 是", registered_on: "注册时用的设备是",
  bound_identity: "绑定了第三方账号", paid_with: "付款账号是", has_passkey: "登记了通行密钥", authorized: "授权过应用",
};

export function isBanned(n?: GraphNode) {
  return !!n && (n.props?.banned === true || n.props?.status === "banned");
}

/** 共享出口（多人共用）或机房、代理出口的 IP：弱关系，不作为关联依据 */
export function isWeakIP(n?: GraphNode) {
  return !!n && n.kind === "ip" && (n.props?.shared === true || n.props?.hosting === true);
}

function num(v: unknown) {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function str(v: unknown) {
  return typeof v === "string" ? v.trim() : "";
}

export function seen(ts: number) {
  return ts > 0 ? formatRelative(new Date(ts * 1000).toISOString()) : "";
}

export function seenAt(ts: number) {
  return ts > 0 ? formatDateTime(new Date(ts * 1000).toISOString()) : "";
}

const PLATFORM_LABEL: Record<string, string> = {
  android: "Android", ios: "iOS", windows: "Windows", macos: "macOS", linux: "Linux", harmonyos: "鸿蒙", web: "网页",
};

const PROVIDER_LABEL: Record<string, string> = { qq: "QQ", google: "Google", wechat: "微信", weixin: "微信", github: "GitHub", apple: "Apple" };

export function providerName(p: string) {
  return PROVIDER_LABEL[p.toLowerCase()] ?? (p || "第三方");
}

function badgesOf(n: GraphNode): string[] {
  const p = n.props ?? {};
  const out: string[] = [];
  if (isBanned(n)) out.push("已封禁");
  if (n.kind === "ip") {
    if (p.shared === true) out.push(`共享 ${num(p.users)}`);
    else if (p.hosting === true) out.push("机房");
    if (p.vpn === true || p.proxy === true) out.push("代理");
  }
  if (n.kind === "device") {
    if (p.emulator === true) out.push("模拟器");
    if (p.rooted === true) out.push("Root");
  }
  return out;
}

/** 节点在图上显示的名字（比原始 label 更好认） */
export function displayName(n: GraphNode): string {
  const p = n.props ?? {};
  switch (n.kind) {
    case "user":
      return str(p.nickname) || str(p.name) || n.label;
    case "identity":
      return str(p.nickname) ? `${providerName(str(p.provider))} ${str(p.nickname)}` : `${providerName(str(p.provider))} 账号`;
    case "device": {
      const name = str(p.name);
      const model = [str(p.brand), str(p.model)].filter(Boolean).join(" ");
      return model || name || n.label;
    }
    default:
      return n.label || n.id;
  }
}

/**
 * 整理成可画的点与边。hidden：图例里关掉的类型；expanded：已展开的 IP 历史（键是主人的点 ID）。
 */
export function buildView(g: Subgraph, hidden: ReadonlySet<ViewKind>, expanded: ReadonlySet<string>): { nodes: ViewNode[]; edges: ViewEdge[]; groups: number } {
  const byId = new Map(g.nodes.map((n) => [n.id, n]));
  // 每个 IP 的「主人」：除网段外只连着一个用户 / 登录名
  const neighbors = new Map<string, Set<string>>();
  const touch = (a: string, b: string) => {
    if (!neighbors.has(a)) neighbors.set(a, new Set());
    neighbors.get(a)!.add(b);
  };
  for (const l of g.links) {
    touch(l.source, l.target);
    touch(l.target, l.source);
  }
  const owned = new Map<string, GraphNode[]>();
  for (const n of g.nodes) {
    if (n.kind !== "ip" || n.id === g.center || isWeakIP(n)) continue;
    const people = [...(neighbors.get(n.id) ?? [])].filter((id) => {
      const k = byId.get(id)?.kind;
      return k === "user" || k === "account";
    });
    if (people.length !== 1) continue;
    const owner = people[0];
    if (!owned.has(owner)) owned.set(owner, []);
    owned.get(owner)!.push(n);
  }
  const groupOf = new Map<string, string>();
  const groupNodes: ViewNode[] = [];
  for (const [owner, ips] of owned) {
    if (ips.length < 4 || expanded.has(owner)) continue;
    const gid = `grp:${owner}`;
    for (const ip of ips) groupOf.set(ip.id, gid);
    const ownerNode = byId.get(owner);
    groupNodes.push({
      id: gid, kind: "ip_group", label: `${ips.length} 个 IP`, members: ips, depth: (ownerNode?.depth ?? 0) + 1,
      center: false, banned: ips.some(isBanned), weak: false, badges: ips.some(isBanned) ? ["含已封禁"] : [],
    });
  }

  const visible = (n: GraphNode) => n.id === g.center || !hidden.has(n.kind);
  const nodes: ViewNode[] = [];
  for (const n of g.nodes) {
    if (groupOf.has(n.id) || !visible(n)) continue;
    nodes.push({
      id: n.id, kind: n.kind, label: displayName(n), node: n, depth: n.depth, center: n.id === g.center,
      banned: isBanned(n), weak: isWeakIP(n) && n.id !== g.center, badges: badgesOf(n),
    });
  }
  if (!hidden.has("ip_group") && !hidden.has("ip")) nodes.push(...groupNodes);
  const shown = new Set(nodes.map((n) => n.id));

  const merged = new Map<string, ViewEdge>();
  for (const l of g.links) {
    const s = groupOf.get(l.source) ?? l.source;
    const t = groupOf.get(l.target) ?? l.target;
    if (s === t || !shown.has(s) || !shown.has(t)) continue;
    const id = `${s}>${t}:${l.kind}`;
    const weak = isWeakIP(byId.get(l.source)) || isWeakIP(byId.get(l.target));
    const prev = merged.get(id);
    if (prev) {
      prev.count += l.count;
      prev.lastSeen = Math.max(prev.lastSeen, l.lastSeen);
      prev.firstSeen = prev.firstSeen && l.firstSeen ? Math.min(prev.firstSeen, l.firstSeen) : prev.firstSeen || l.firstSeen;
    } else {
      merged.set(id, { id, source: s, target: t, kind: l.kind, count: l.count, firstSeen: l.firstSeen, lastSeen: l.lastSeen, weak });
    }
  }
  return { nodes, edges: [...merged.values()], groups: groupNodes.length };
}

// ── 关联归纳 ──

export type ReasonKind = "device" | "phone" | "identity" | "payer" | "invited_by" | "invited" | "email" | "ip" | "target" | "account";

export interface Reason {
  kind: ReasonKind;
  /** 经由的设备 / IP / 第三方账号…… */
  via?: GraphNode;
  lastSeen: number;
}

export interface Relation {
  user: GraphNode;
  reasons: Reason[];
  strength: number;
}

const REASON_WEIGHT: Record<ReasonKind, number> = {
  device: 100, phone: 95, identity: 90, payer: 85, invited_by: 70, invited: 70, email: 60, account: 55, ip: 40, target: 30,
};

export const REASON_LABEL: Record<ReasonKind, string> = {
  device: "同一台设备", phone: "同一个手机号", identity: "同一个第三方账号", payer: "同一个付款账号", invited_by: "邀请了 TA",
  invited: "TA 邀请的", email: "同一个邮箱", account: "同一个登录名", ip: "同一个 IP", target: "互动过",
};

const VIA_REASON: Partial<Record<GraphNodeKind, ReasonKind>> = {
  device: "device", phone: "phone", identity: "identity", payer: "payer", email: "email", ip: "ip", account: "account",
};

export interface RelationStats {
  devices: GraphNode[];
  ips: GraphNode[];
  weakIPs: GraphNode[];
  identities: GraphNode[];
  payers: GraphNode[];
  relations: Relation[];
  byReason: Map<ReasonKind, number>;
}

/** 以中心点为主体，归纳与其他用户的关联 */
export function analyze(g: Subgraph): RelationStats {
  const byId = new Map(g.nodes.map((n) => [n.id, n]));
  const adj = new Map<string, GraphLink[]>();
  for (const l of g.links) {
    for (const end of [l.source, l.target]) {
      if (!adj.has(end)) adj.set(end, []);
      adj.get(end)!.push(l);
    }
  }
  const other = (l: GraphLink, me: string) => (l.source === me ? l.target : l.source);
  const center = g.center;
  const mine = (kind: GraphNodeKind) => (adj.get(center) ?? []).map((l) => byId.get(other(l, center))).filter((n): n is GraphNode => !!n && n.kind === kind);
  const uniq = (xs: GraphNode[]) => [...new Map(xs.map((x) => [x.id, x])).values()];

  const rel = new Map<string, Relation>();
  const add = (userId: string, r: Reason) => {
    const u = byId.get(userId);
    if (!u || u.kind !== "user" || u.id === center) return;
    const cur = rel.get(userId) ?? { user: u, reasons: [], strength: 0 };
    if (!cur.reasons.some((x) => x.kind === r.kind && x.via?.id === r.via?.id)) cur.reasons.push(r);
    cur.strength = Math.max(cur.strength, REASON_WEIGHT[r.kind]);
    rel.set(userId, cur);
  };

  for (const l of adj.get(center) ?? []) {
    const x = byId.get(other(l, center));
    if (!x) continue;
    if (x.kind === "user") {
      if (l.kind === "invited") add(x.id, { kind: l.source === center ? "invited" : "invited_by", lastSeen: l.lastSeen });
      else add(x.id, { kind: "target", lastSeen: l.lastSeen });
      continue;
    }
    const reason = VIA_REASON[x.kind];
    if (!reason || isWeakIP(x)) continue;
    for (const l2 of adj.get(x.id) ?? []) {
      const y = other(l2, x.id);
      if (y !== center) add(y, { kind: reason, via: x, lastSeen: Math.min(l.lastSeen || l2.lastSeen, l2.lastSeen || l.lastSeen) });
    }
  }
  const relations = [...rel.values()].sort((a, b) => b.strength - a.strength || b.reasons.length - a.reasons.length);
  const byReason = new Map<ReasonKind, number>();
  for (const r of relations) {
    for (const k of new Set(r.reasons.map((x) => x.kind))) byReason.set(k, (byReason.get(k) ?? 0) + 1);
  }
  const ips = uniq(mine("ip"));
  return {
    devices: uniq(mine("device")), ips, weakIPs: ips.filter(isWeakIP), identities: uniq(mine("identity")),
    payers: uniq(mine("payer")), relations, byReason,
  };
}

/** 叙述式摘要的一句话 */
export function narrative(name: string, days: number | null, s: RelationStats): string {
  const span = days ? `近 ${days} 天，` : "";
  const parts: string[] = [];
  if (s.devices.length) parts.push(`${s.devices.length} 台设备`);
  if (s.ips.length) parts.push(`${s.ips.length} 个 IP 地址`);
  let head = parts.length ? `${span}${name} 用过 ${parts.join("、")}` : `${span}${name} 没有留下设备与网络记录`;
  if (s.identities.length) head += `，绑定了 ${[...new Set(s.identities.map((i) => providerName(str(i.props?.provider))))].join("、")} 账号`;
  head += "。";
  if (!s.relations.length) {
    return head + "没有发现与其他用户的关联。" + weakNote(s);
  }
  const order: ReasonKind[] = ["device", "phone", "identity", "payer", "invited", "invited_by", "email", "account", "ip", "target"];
  const phrase: Record<ReasonKind, (n: number) => string> = {
    device: (n) => `${n} 位在同一台设备上登录过`, phone: (n) => `${n} 位绑定了同一个手机号`, identity: (n) => `${n} 位绑定了同一个第三方账号`,
    payer: (n) => `${n} 位用同一个账号付过款`, invited: (n) => `${n} 位是 TA 邀请来的`, invited_by: (n) => (n === 1 ? "1 位是 TA 的邀请人" : `${n} 位邀请过 TA`),
    email: (n) => `${n} 位用同一个邮箱`, account: (n) => `${n} 位用过同一个登录名`, ip: (n) => `${n} 位用过同一个 IP`, target: (n) => `${n} 位和 TA 有过互动`,
  };
  const bits = order.filter((k) => s.byReason.get(k)).map((k) => phrase[k](s.byReason.get(k)!));
  return `${head}与 ${s.relations.length} 位用户存在关联：${bits.join("，")}。${weakNote(s)}`;
}

function weakNote(s: RelationStats) {
  return s.weakIPs.length ? `另有 ${s.weakIPs.length} 个 IP 是多人共用的出口或机房、代理地址，已从关联中排除。` : "";
}

// ── 节点说明 ──

export interface Fact {
  label: string;
  value: string;
  tone?: "danger" | "warning" | "muted";
}

/** 节点的说明：标题、一句副标题、若干条事实 */
export function describe(n: GraphNode, degree: number): { title: string; subtitle: string; facts: Fact[] } {
  const p = n.props ?? {};
  const facts: Fact[] = [];
  const push = (label: string, value: string, tone?: Fact["tone"]) => {
    if (value) facts.push({ label, value, tone });
  };
  const first = num(p.first_seen);
  const last = num(p.last_seen);
  let subtitle = KIND_LABEL[n.kind];
  switch (n.kind) {
    case "user": {
      const uid = num(p.uid) || Number(n.id.split(":")[2] ?? 0);
      subtitle = `用户 #${uid}`;
      const nick = str(p.nickname);
      const acc = str(p.name);
      if (nick && acc && nick !== acc) push("登录名", acc);
      const status = isBanned(n) ? "已封禁" : str(p.status) === "disabled" ? "已禁用" : "正常";
      push("账号状态", status, status === "正常" ? undefined : "danger");
      if (num(p.registered_at)) push("注册于", seenAt(num(p.registered_at)));
      if (str(p.last_action)) push("最近一次风控结论", str(p.last_action));
      break;
    }
    case "device": {
      const platform = PLATFORM_LABEL[str(p.platform)] ?? str(p.platform);
      subtitle = [platform, str(p.os_version)].filter(Boolean).join(" ") || "设备";
      if (str(p.name) && str(p.name) !== displayName(n)) push("名称", str(p.name));
      push("客户端版本", str(p.app_version));
      if (p.emulator === true) push("运行环境", "模拟器", "warning");
      if (p.rooted === true) push("系统权限", "已 Root", "warning");
      if (isBanned(n)) push("状态", "在设备黑名单里", "danger");
      if (degree > 1) push("使用过的账号", `${degree} 个`, degree >= 3 ? "warning" : undefined);
      break;
    }
    case "ip": {
      const where = [str(p.country), str(p.province), str(p.city)].filter(Boolean).join(" ");
      subtitle = where || "IP 地址";
      push("运营商", [str(p.isp), num(p.asn) ? `AS${num(p.asn)}` : ""].filter(Boolean).join("，"));
      if (num(p.users)) push("用过它的用户", `${num(p.users)} 位`, num(p.users) >= 5 ? "warning" : undefined);
      if (p.shared === true) push("判断", "多人共用的出口，不作为关联依据", "muted");
      else if (p.hosting === true) push("判断", "机房或代理出口，不作为关联依据", "muted");
      if (p.vpn === true) push("网络", "VPN", "warning");
      else if (p.proxy === true) push("网络", "代理", "warning");
      if (p.tor === true) push("网络", "Tor 出口", "danger");
      if (isBanned(n)) push("状态", "在 IP 黑名单里", "danger");
      break;
    }
    case "identity":
      subtitle = `${providerName(str(p.provider))} 第三方账号`;
      push("地区", str(p.region));
      if (degree > 1) push("绑定过的用户", `${degree} 位`, "warning");
      break;
    case "payer":
      subtitle = `${str(p.channel) === "afdian" ? "爱发电" : str(p.channel)} 付款账号`;
      if (degree > 1) push("用它付过款的用户", `${degree} 位`, "warning");
      break;
    case "phone":
      subtitle = "手机号";
      push("号段", str(p.prefix));
      if (degree > 1) push("绑定过的用户", `${degree} 位`, "warning");
      break;
    case "email":
      subtitle = "邮箱";
      push("域名", str(p.domain));
      if (degree > 1) push("绑定过的用户", `${degree} 位`, "warning");
      break;
    case "subnet":
      subtitle = "网段";
      break;
    case "account":
      subtitle = "登录名";
      break;
    case "passkey":
      subtitle = "通行密钥";
      break;
    case "client":
      subtitle = "授权过的第三方应用";
      push("客户端 ID", str(p.client_id));
      break;
  }
  if (first) push("第一次出现", `${seen(first)}（${seenAt(first)}）`);
  if (last && last !== first) push("最近一次出现", `${seen(last)}（${seenAt(last)}）`);
  return { title: displayName(n), subtitle, facts };
}

/** 关系的说明：「川意 使用过设备 小米 24031PN0DC，12 次，最近 3 天前」 */
export function describeEdge(e: ViewEdge, from?: string, to?: string): string {
  const verb = EDGE_VERB[e.kind] ?? "关联";
  const times = e.count > 1 ? `，共 ${e.count} 次` : "";
  const when = e.lastSeen ? `，最近一次 ${seen(e.lastSeen)}` : "";
  const weak = e.weak ? "（多人共用的出口，不作为关联依据）" : "";
  return `${from ?? e.source} ${verb} ${to ?? e.target}${times}${when}${weak}`;
}

/** 每个点连着多少个用户 / 登录名（设备、手机号等「被几个人用过」） */
export function peopleDegree(g: Subgraph): Map<string, number> {
  const kind = new Map(g.nodes.map((n) => [n.id, n.kind]));
  const people = new Map<string, Set<string>>();
  for (const l of g.links) {
    for (const [a, b] of [[l.source, l.target], [l.target, l.source]] as const) {
      const k = kind.get(b);
      if (k === "user") {
        if (!people.has(a)) people.set(a, new Set());
        people.get(a)!.add(b);
      }
    }
  }
  return new Map([...people].map(([k, v]) => [k, v.size]));
}
