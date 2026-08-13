import {
  AppWindow,
  BarChart3,
  BellDot,
  ShieldAlert,
  Building2,
  ClipboardList,
  CloudCog,
  Code2,
  Database,
  FileCheck2,
  GalleryHorizontalEnd,
  Gavel,
  KeyRound,
  LayoutDashboard,
  LifeBuoy,
  MailOpen,
  Puzzle,
  PackageCheck,
  Settings2,
  ShieldCheck,
  Smartphone,
  Users2,
  Wallet,
  Workflow
} from "lucide-react";
import { appSections } from "@/lib/app-sections";

type NavIcon = typeof LayoutDashboard;

/**
 * 页内子项（三级）。
 *
 * 仅用于「Tab 状态同步到 URL」的页面（`?tab=xxx`），点击后可直达对应面板。
 * 若某页面的 Tab 仅存在于组件内部 state，则不要在此登记，否则链接不会生效。
 */
export type NavigationChild = {
  title: string;
  /** 页面 Tab 值，最终链接为 `${item.href}?tab=${tab}`；数组首项即该页默认 Tab */
  tab: string;
};

/** 导航页面（二级） */
export type NavigationItem = {
  title: string;
  href: string;
  icon: NavIcon;
  summary: string;
  /** 需要的权限代码（空 = 所有管理员可见） */
  permission?: string;
  /** 是否要求超管 */
  superAdmin?: boolean;
  /** 页内子项，权限继承自本项 */
  children?: NavigationChild[];
  /**
   * 子项渲染在**下级路由**上，页面自身（`href`）不是其中任何一个。
   *
   * `/apps` 就是这种：它是应用列表，各个区块在 `/apps/{appKey}` 上。
   * 不声明的话，停在列表页时侧边栏会高亮「基本信息」、面包屑也会多出一截，
   * 指向一个当前根本没有打开的东西。
   */
  childrenOnSubRoute?: boolean;
};

/** 导航分组（一级） */
export type NavigationGroup = {
  key: string;
  title: string;
  /** 分组用途说明，用于折叠态提示 */
  summary: string;
  items: NavigationItem[];
};

export const navigationGroups: NavigationGroup[] = [
  {
    key: "insight",
    title: "概览",
    summary: "平台运行态势",
    items: [
      {
        title: "总览",
        href: "/overview",
        icon: LayoutDashboard,
        summary: "核心指标"
      },
      {
        title: "报表分析",
        href: "/reports",
        icon: BarChart3,
        summary: "数据报表与分析",
        children: [
          { title: "注册", tab: "registration" },
          { title: "登录", tab: "login" },
          { title: "留存", tab: "retention" },
          { title: "活跃", tab: "active" },
          { title: "设备", tab: "device" },
          { title: "地区", tab: "region" },
          { title: "渠道", tab: "channel" },
          { title: "支付", tab: "payment" },
          { title: "通知", tab: "notification" },
          { title: "风控", tab: "risk" },
          { title: "活动", tab: "activity" },
          { title: "漏斗", tab: "funnel" },
          { title: "导出", tab: "export" }
        ]
      }
    ]
  },
  {
    key: "product",
    title: "应用与内容",
    summary: "应用接入与内容投放",
    items: [
      {
        // 应用级配置的唯一归属地。列表在 /apps，配置在 /apps/{appKey}。
        // 平台级配置在 /configuration，两边不再各有一半。
        title: "应用",
        href: "/apps",
        icon: AppWindow,
        summary: "应用列表与全部配置",
        // 子项直接由区块目录派生，避免「侧边栏有这一项、详情页没这个区块」的漂移。
        // 链接是不带 appKey 的 `/apps?tab=xxx`，由列表页转交给最近打开的那个应用。
        children: appSections.map((section) => ({ title: section.title, tab: section.key })),
        childrenOnSubRoute: true
      },
      {
        title: "内容",
        href: "/content",
        icon: BellDot,
        summary: "Banner、公告与法律文本",
        // 首项必须是该页默认 Tab，否则无 `?tab=` 时高亮会错位
        children: [
          { title: "Banner", tab: "banners" },
          { title: "应用公告", tab: "notices" },
          { title: "系统公告", tab: "announcements" },
          { title: "法律文本", tab: "legal" }
        ]
      },
      {
        title: "平台横幅",
        href: "/platform-banners",
        icon: GalleryHorizontalEnd,
        summary: "总览页轮播 Banner",
        superAdmin: true
      },
      {
        title: "模板",
        href: "/templates",
        icon: MailOpen,
        summary: "消息模板管理",
        superAdmin: true
      },
      {
        title: "发布",
        href: "/releases",
        icon: PackageCheck,
        summary: "版本与渠道"
      },
      {
        // 与 /apps?tab=payment 的分工是「运营 vs 配置」：那边配渠道密钥与限额，
        // 这里看已经发生的钱。同一件事只有一个入口，两边不重复。
        title: "交易",
        href: "/commerce",
        icon: Wallet,
        summary: "订单、退款、钱包与凭证",
        children: [
          { title: "概览", tab: "overview" },
          { title: "订单", tab: "orders" },
          { title: "退款", tab: "refunds" },
          { title: "钱包流水", tab: "wallet" },
          { title: "会员", tab: "vip" }
        ]
      }
    ]
  },
  {
    key: "developer",
    title: "开发者",
    summary: "开放能力与接入文档",
    items: [
      {
        title: "远程函数",
        href: "/functions",
        icon: Code2,
        summary: "服务端脚本、版本与调用密钥",
        children: [
          { title: "函数", tab: "functions" },
          { title: "键值存储", tab: "kv" },
          { title: "调用密钥", tab: "keys" }
        ]
      },
      {
        // 公开门户（免登录），位于 console 路由组之外，点击会离开控制台 Shell
        title: "开发者门户",
        href: "/developers",
        icon: CloudCog,
        summary: "快速接入与接口文档"
      }
    ]
  },
  {
    key: "identity",
    title: "用户与权限",
    summary: "账号、组织与授权",
    items: [
      {
        title: "用户",
        href: "/users",
        icon: Users2,
        summary: "账号权限",
        children: [
          { title: "管理员", tab: "admins" },
          { title: "应用用户", tab: "app-users" },
          { title: "在线会话", tab: "online" },
          { title: "角色权限", tab: "roles" },
          { title: "用户主档", tab: "identities" },
          { title: "标签管理", tab: "tags" },
          { title: "分群管理", tab: "segments" },
          { title: "黑白名单", tab: "lists" },
          { title: "风险用户", tab: "risk" },
          { title: "用户申诉", tab: "appeals" },
          { title: "注销管理", tab: "deactivation" }
        ]
      },
      {
        // 不挂 permission：组织可见范围由后端按「组织成员身份 + 权限集」推导，
        // 只是某个组织普通成员的管理员没有任何平台级 org:* 权限点，但必须能看到入口
        title: "组织",
        href: "/organization",
        icon: Building2,
        summary: "组织 / 部门 / 成员 / 权限",
        children: [
          { title: "概览", tab: "overview" },
          { title: "组织结构", tab: "structure" },
          { title: "成员", tab: "members" },
          { title: "架构图", tab: "chart" },
          { title: "岗位", tab: "positions" },
          { title: "角色权限", tab: "roles" },
          { title: "审批", tab: "approvals" },
          { title: "邀请", tab: "invitations" },
          { title: "资源", tab: "resources" },
          { title: "导入导出", tab: "data" },
          { title: "操作日志", tab: "activity" },
          { title: "设置", tab: "settings" }
        ]
      },
      {
        title: "角色",
        href: "/roles",
        icon: KeyRound,
        summary: "RBAC 权限管理",
        superAdmin: true
      },
      {
        title: "审核",
        href: "/reviews",
        icon: FileCheck2,
        summary: "站点与角色申请"
      },
      {
        // 工单可见范围由后端按「权限点 + 应用作用域 + 处理组归属」推导，
        // 因此这里不挂 permission：处理组成员即使没有任何 ticket:* 权限点，
        // 也必须能看到入口去处理自己名下的工单。
        title: "工单",
        href: "/tickets",
        icon: LifeBuoy,
        summary: "服务台与通知出口",
        children: [
          { title: "工单台", tab: "inbox" },
          { title: "我的待办", tab: "mine" },
          { title: "统计分析", tab: "analytics" },
          { title: "工单配置", tab: "settings" },
          { title: "通知出口", tab: "notify" }
        ]
      }
    ]
  },
  {
    key: "governance",
    title: "平台治理",
    summary: "跨应用的强制管控",
    items: [
      {
        // 全局作用域：这里没有顶部应用选择器，一次面对全站所有应用。
        // 与 /apps 的分工是「谁说了算」——/apps 是应用自治的配置，
        // 这里是平台强制的结论，应用管理员改不动。
        title: "治理台",
        href: "/platform",
        icon: Gavel,
        summary: "冻结、封禁与申诉",
        permission: "platform:app:read",
        children: [
          { title: "应用", tab: "apps" },
          { title: "申诉", tab: "appeals" },
          { title: "治理流水", tab: "actions" }
        ]
      }
    ]
  },
  {
    key: "security",
    title: "安全与风控",
    summary: "防护、风控与留痕",
    items: [
      {
        // 只管运行态（发生了什么）；平台安全的配置在 /configuration
        title: "安全",
        href: "/security",
        icon: ShieldCheck,
        summary: "安全运行态与留痕",
        superAdmin: true,
        children: [
          { title: "账户安全", tab: "account" },
          { title: "应用概览", tab: "overview" },
          { title: "防火墙", tab: "firewall" },
          { title: "地理风控", tab: "geo-intel" }
        ]
      },
      {
        title: "风控中心",
        href: "/risk-control",
        icon: ShieldAlert,
        summary: "风险规则与评估",
        // 首项必须是该页默认 Tab，否则无 ?tab= 时高亮会错位
        children: [
          { title: "风控大盘", tab: "dashboard" },
          { title: "评估记录", tab: "assessments" },
          { title: "人工复核", tab: "reviews" },
          { title: "风险规则", tab: "rules" },
          { title: "处置策略", tab: "actions" },
          { title: "规则模拟", tab: "simulator" },
          { title: "设备指纹", tab: "devices" },
          { title: "IP 风险库", tab: "ips" }
        ]
      },
      {
        title: "审计",
        href: "/audit",
        icon: ClipboardList,
        summary: "操作日志追溯",
        superAdmin: true
      }
    ]
  },
  {
    key: "platform",
    title: "平台运维",
    summary: "配置、存储与扩展",
    items: [
      {
        // 平台级配置（对所有应用生效）；应用自身的配置在 /apps
        title: "配置",
        href: "/configuration",
        icon: Settings2,
        summary: "平台级配置",
        superAdmin: true,
        children: [
          { title: "品牌与外观", tab: "branding" },
          { title: "防火墙与限流", tab: "firewall" },
          { title: "出海代理", tab: "egress" },
          { title: "安全模块", tab: "security" },
          { title: "LDAP", tab: "ldap" },
          { title: "OIDC", tab: "oidc" },
          { title: "SAML", tab: "saml" },
          { title: "管理员验证码", tab: "admin-captcha" }
        ]
      },
      {
        title: "存储管理",
        href: "/storage",
        icon: Database,
        summary: "对象存储配置",
        children: [
          { title: "全局配置", tab: "global" },
          { title: "应用级配置", tab: "app" },
          { title: "文件管理", tab: "files" },
          { title: "上传规则", tab: "rules" },
          { title: "CDN 与安全", tab: "cdn" },
          { title: "用量统计", tab: "usage" }
        ]
      },
      {
        title: "工作流",
        href: "/workflows",
        icon: Workflow,
        summary: "任务编排"
      },
      {
        title: "插件",
        href: "/plugins",
        icon: Puzzle,
        summary: "扩展与钩子",
        superAdmin: true
      },
      {
        title: "设备字典",
        href: "/device-marketing",
        icon: Smartphone,
        summary: "iOS/Android 标识 → 营销名",
        superAdmin: true
      }
    ]
  }
];

/** 扁平化的全部导航页面，供路由预加载、面包屑等按 href 查找 */
export const navigationItems: NavigationItem[] = navigationGroups.flatMap((group) => group.items);

/**
 * 一个可跳转目标 —— 把「分组 → 页面 → 页内子项」三级压平成一维。
 *
 * 命令面板、收藏与最近访问都只认这一维：它们要回答的是「去哪儿」，
 * 而"页面"和"页内面板"对使用者来说是同一件事（都是一次跳转）。
 */
export type NavigationTarget = {
  /** 唯一标识，同时就是链接本身（`/users` 或 `/users?tab=admins`） */
  key: string;
  /** 展示名：页面标题，或页内子项标题 */
  title: string;
  /** 所属页面标题（页面自身则与 title 相同） */
  itemTitle: string;
  groupKey: string;
  groupTitle: string;
  icon: NavIcon;
  summary?: string;
  /** 是否为页内子项（三级） */
  isChild: boolean;
  /** 鉴权继承自所属页面 */
  permission?: string;
  superAdmin?: boolean;
};

/** 全部可跳转目标（页面 + 页内子项），顺序与侧边栏一致 */
export const navigationTargets: NavigationTarget[] = navigationGroups.flatMap((group) =>
  group.items.flatMap((item) => {
    const base = {
      groupKey: group.key,
      groupTitle: group.title,
      icon: item.icon,
      permission: item.permission,
      superAdmin: item.superAdmin,
      itemTitle: item.title
    };
    const self: NavigationTarget = {
      ...base,
      key: item.href,
      title: item.title,
      summary: item.summary,
      isChild: false
    };
    const children = (item.children ?? []).map<NavigationTarget>((child) => ({
      ...base,
      key: childHref(item, child),
      title: child.title,
      isChild: true
    }));
    return [self, ...children];
  })
);

/**
 * 当前路径 + `?tab=` 对应的目标 key，用于「收藏当前页」与最近访问打点。
 *
 * 复用 `matchNavigation`，因此 `/apps/{appKey}?tab=oauth` 这类下级路由
 * 会正确归到 `/apps?tab=oauth` 上 —— 收藏的是"哪个区块"，不是"哪个应用的区块"。
 */
export function currentTargetKey(pathname: string, tab: string | null): string | null {
  const match = matchNavigation(pathname, tab);
  if (!match) return null;
  return match.child ? childHref(match.item, match.child) : match.item.href;
}

/** 拼接页内子项链接 */
export function childHref(item: NavigationItem, child: NavigationChild): string {
  return `${item.href}?tab=${child.tab}`;
}

/**
 * 当前是否停在页面自身而非它的下级路由。
 *
 * 子项在下级路由上的页面（`childrenOnSubRoute`）停在自身时，
 * 不应该有任何子项被判定为「当前」。
 */
export function isItemRoot(pathname: string, item: NavigationItem): boolean {
  return pathname === item.href;
}

/** 当前生效的页内子项 tab，`null` 表示没有任何子项处于打开状态 */
export function activeChildTab(
  pathname: string,
  item: NavigationItem,
  activeTab: string | null
): string | null {
  const children = item.children;
  if (!children?.length) return null;
  if (children.some((child) => child.tab === activeTab)) return activeTab;
  // 无 ?tab= 时页面渲染的是首个子项 —— 但列表页（子项在下级路由）什么子项都没打开
  if (item.childrenOnSubRoute && isItemRoot(pathname, item)) return null;
  return children[0].tab;
}

/** 当前路径是否命中某个导航页面（含 `/users/app-users/...` 这类下级路由） */
export function isItemActive(pathname: string, item: NavigationItem): boolean {
  return pathname === item.href || pathname.startsWith(`${item.href}/`);
}

export type NavigationMatch = {
  group: NavigationGroup;
  item: NavigationItem;
  /** 仅当停留在页面自身（非下级路由）且该页登记了子项时才有值 */
  child?: NavigationChild;
};

/** 按路径 + `?tab=` 反查导航层级，用于面包屑与高亮 */
export function matchNavigation(pathname: string, tab?: string | null): NavigationMatch | null {
  for (const group of navigationGroups) {
    for (const item of group.items) {
      if (!isItemActive(pathname, item)) continue;
      if (!item.children?.length) return { group, item };
      // 普通页面只有停在自身时才有「当前子项」；子项在下级路由的页面（/apps）正好相反：
      // 停在列表页不属于任何子项，进了详情页 `/apps/{appKey}` 才按 ?tab= 定位
      const atRoot = isItemRoot(pathname, item);
      const hasChild = item.childrenOnSubRoute ? !atRoot : atRoot;
      if (!hasChild) return { group, item };
      const activeTabKey = activeChildTab(pathname, item, tab ?? null);
      const child = item.children.find((c) => c.tab === activeTabKey);
      return child ? { group, item, child } : { group, item };
    }
  }
  return null;
}
