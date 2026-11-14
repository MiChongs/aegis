import type {
  PermissionCatalog,
  PermissionNode,
  PermissionScope,
} from "@/lib/api/access";

/**
 * 权限树的派生索引与判定（与后端 authz.Implies 同口径）。
 *
 * 落进策略的只有操作节点的代码；目录 / 菜单只是分组。一条授予可以是细分代码、旧的合并代码
 * （app:read）或前缀通配（app:*），这里统一展开成它覆盖的细分代码，编辑器只和细分代码打交道。
 */

export type TreeEntry = {
  node: PermissionNode;
  depth: number;
  parent?: string;
  /** 祖先名称（目录 / 菜单） */
  path: string[];
  /** 该节点（含后代）的全部操作代码 */
  leaves: string[];
};

export type TreeIndex = {
  roots: PermissionNode[];
  entries: Map<string, TreeEntry>;
  /** 先序遍历顺序 */
  order: string[];
  /** 全部操作代码（树序） */
  codes: string[];
  /** 代码 → 节点 key */
  codeKey: Map<string, string>;
  /** 旧合并代码 → 名下细分代码 */
  legacyChildren: Map<string, string[]>;
  legacyNames: Record<string, string>;
};

export function buildTreeIndex(catalog?: PermissionCatalog | null): TreeIndex {
  const entries = new Map<string, TreeEntry>();
  const order: string[] = [];
  const codes: string[] = [];
  const codeKey = new Map<string, string>();
  const legacyChildren = new Map<string, string[]>();

  const walk = (
    nodes: PermissionNode[],
    depth: number,
    parent: string | undefined,
    path: string[],
  ): string[] => {
    const collected: string[] = [];
    for (const node of nodes) {
      order.push(node.key);
      const entry: TreeEntry = { node, depth, parent, path, leaves: [] };
      entries.set(node.key, entry);
      if (node.kind === "action" && node.code) {
        entry.leaves = [node.code];
        codes.push(node.code);
        codeKey.set(node.code, node.key);
        if (node.legacy) {
          const list = legacyChildren.get(node.legacy) ?? [];
          list.push(node.code);
          legacyChildren.set(node.legacy, list);
        }
      } else {
        entry.leaves = walk(node.children ?? [], depth + 1, node.key, [
          ...path,
          node.name,
        ]);
      }
      collected.push(...entry.leaves);
    }
    return collected;
  };
  walk(catalog?.tree ?? [], 0, undefined, []);
  return {
    roots: catalog?.tree ?? [],
    entries,
    order,
    codes,
    codeKey,
    legacyChildren,
    legacyNames: catalog?.legacy ?? {},
  };
}

function patternMatches(pattern: string, code: string) {
  if (pattern === "*" || pattern === code) return true;
  if (pattern.endsWith("*")) return code.startsWith(pattern.slice(0, -1));
  return false;
}

/** 一条授予是否覆盖某个细分代码（前缀通配 + 细分 → 旧代码） */
export function implies(index: TreeIndex, granted: string, code: string) {
  if (patternMatches(granted, code)) return true;
  const entry = index.entries.get(index.codeKey.get(code) ?? "");
  const legacy = entry?.node.legacy;
  return Boolean(legacy && patternMatches(granted, legacy));
}

/** 把一组授予展开成细分代码集合 */
export function expandPatterns(
  index: TreeIndex,
  patterns: string[] | undefined,
): Set<string> {
  const result = new Set<string>();
  for (const raw of patterns ?? []) {
    const pattern = raw.trim();
    if (!pattern) continue;
    if (index.codeKey.has(pattern)) {
      result.add(pattern);
      continue;
    }
    for (const code of index.codes)
      if (implies(index, pattern, code)) result.add(code);
  }
  return result;
}

/** 代码的完整中文名「菜单：操作」 */
export function permissionLabel(index: TreeIndex, code: string) {
  const entry = index.entries.get(index.codeKey.get(code) ?? "");
  if (entry) {
    const menu = entry.path[entry.path.length - 1];
    return menu ? `${menu}：${entry.node.name}` : entry.node.name;
  }
  if (index.legacyNames[code]) return index.legacyNames[code];
  if (code === "*") return "全部权限";
  if (code.endsWith(":*")) return `「${code.slice(0, -2)}」下的全部权限`;
  return code;
}

/** 代码所在的完整路径（目录 / 菜单 / 操作） */
export function permissionPath(index: TreeIndex, code: string) {
  const entry = index.entries.get(index.codeKey.get(code) ?? "");
  return entry ? [...entry.path, entry.node.name] : [code];
}

/** 作用域对某类角色是否可生效：应用级角色只在应用域判定，平台级权限点对它没有意义 */
export function scopeApplies(
  scope: PermissionScope,
  roleScope: "global" | "app" | "any",
) {
  if (roleScope !== "app") return true;
  return scope !== "platform";
}

// ── 搜索与可见性 ──

export type TreeFilter = {
  keyword: string;
  onlySelected?: boolean;
  onlyDanger?: boolean;
  scope?: "all" | "platform" | "app";
};

/**
 * 计算可见节点：命中的节点连同祖先可见；目录 / 菜单命中时整枝可见。
 * 返回 null 表示不过滤（全部可见）。
 */
export function visibleKeys(
  index: TreeIndex,
  filter: TreeFilter,
  selected: (code: string) => boolean,
): Set<string> | null {
  const keyword = filter.keyword.trim().toLowerCase();
  const active =
    keyword !== "" ||
    filter.onlySelected ||
    filter.onlyDanger ||
    (filter.scope && filter.scope !== "all");
  if (!active) return null;
  const visible = new Set<string>();
  const addAncestors = (key: string) => {
    let current = index.entries.get(key)?.parent;
    while (current) {
      visible.add(current);
      current = index.entries.get(current)?.parent;
    }
  };
  const addSubtree = (key: string) => {
    visible.add(key);
    for (const child of index.entries.get(key)?.node.children ?? [])
      addSubtree(child.key);
  };
  const leafPasses = (node: PermissionNode) => {
    if (!node.code) return false;
    if (filter.onlySelected && !selected(node.code)) return false;
    if (filter.onlyDanger && !node.danger) return false;
    if (filter.scope === "platform" && node.scope === "app") return false;
    if (filter.scope === "app" && node.scope === "platform") return false;
    return true;
  };
  const matchesKeyword = (node: PermissionNode) =>
    node.name.toLowerCase().includes(keyword) ||
    (node.code ?? "").toLowerCase().includes(keyword) ||
    node.key.toLowerCase().includes(keyword);

  for (const key of index.order) {
    const entry = index.entries.get(key)!;
    const node = entry.node;
    if (keyword && node.kind !== "action" && matchesKeyword(node)) {
      // 分组命中：整枝里通过其余条件的操作都可见
      for (const code of entry.leaves) {
        const leafKey = index.codeKey.get(code)!;
        if (leafPasses(index.entries.get(leafKey)!.node)) {
          visible.add(leafKey);
          addAncestors(leafKey);
        }
      }
      if (!filter.onlySelected && !filter.onlyDanger) addSubtree(key);
      continue;
    }
    if (node.kind !== "action") continue;
    if (!leafPasses(node)) continue;
    if (
      keyword &&
      !matchesKeyword(node) &&
      !entry.path.some((name) => name.toLowerCase().includes(keyword))
    )
      continue;
    visible.add(key);
    addAncestors(key);
  }
  return visible;
}

// ── 三态统计 ──

export type NodeTally = {
  total: number;
  allow: number;
  deny: number;
  locked: number;
  applicable: number;
};

export function tallyNode(
  entry: TreeEntry,
  state: (code: string) => "allow" | "deny" | undefined,
  locked: Set<string>,
  applicable: (code: string) => boolean,
): NodeTally {
  const tally: NodeTally = {
    total: entry.leaves.length,
    allow: 0,
    deny: 0,
    locked: 0,
    applicable: 0,
  };
  for (const code of entry.leaves) {
    if (!applicable(code)) continue;
    tally.applicable++;
    const value = state(code);
    if (value === "deny") tally.deny++;
    else if (value === "allow" || locked.has(code)) tally.allow++;
    if (locked.has(code)) tally.locked++;
  }
  return tally;
}

// ── 方法与图标 ──

export const METHOD_TONE: Record<string, string> = {
  GET: "text-sky-600 dark:text-sky-400 bg-sky-500/10",
  HEAD: "text-sky-600 dark:text-sky-400 bg-sky-500/10",
  OPTIONS: "text-sky-600 dark:text-sky-400 bg-sky-500/10",
  POST: "text-emerald-600 dark:text-emerald-400 bg-emerald-500/10",
  PUT: "text-amber-600 dark:text-amber-400 bg-amber-500/10",
  PATCH: "text-violet-600 dark:text-violet-400 bg-violet-500/10",
  DELETE: "text-rose-600 dark:text-rose-400 bg-rose-500/10",
};
