import { apiRequest } from "./client";

/**
 * Cloud Mail（docs.skymail.ink）邮局代管。
 *
 * 一条 provider=skymail 的邮件通道背后是一整套自建邮局。后端把文档里的全部接口收成两条路由：
 * `query`（只读：收件箱、邮箱账号、用户、全部邮件、注册码……）与 `action`（写：发信、删除、加用户……），
 * 资源与动作在服务端白名单校验。这里按作用域选路由，平台级用 RESTful，应用级沿用 email-config 命名空间。
 *
 * 时间字段已由后端从 Cloud Mail 的 UTC `yyyy-MM-dd HH:mm:ss` 转成 RFC 3339，直接交给 datetime 工具显示。
 */

export type SkymailRole = {
  roleId?: number;
  name: string;
  sendCount: number;
  sendType?: "count" | "day" | string;
  accountCount: number;
  isDefault?: number;
};

export type SkymailProfile = {
  userId: number;
  email: string;
  name: string;
  /** 权限身份 id，管理员为 0 */
  type: number;
  sendCount: number;
  permKeys: string[];
  account: { accountId: number; email: string; name: string };
  role: SkymailRole;
};

export type SkymailAccount = {
  accountId: number;
  email: string;
  name: string;
  userId: number;
  allReceive: number;
  sort: number;
  isDel: number;
  createTime: string;
  addVerifyOpen?: boolean;
};

export type SkymailOverview = {
  baseUrl: string;
  profile: SkymailProfile;
  accounts: SkymailAccount[];
  senderAccountId: number;
  senderAddress: string;
  isAdmin: boolean;
  senderError?: string;
};

export type SkymailAttachment = { attId: number; filename: string; mimeType: string; size: number };

export type SkymailEmail = {
  emailId: number;
  sendEmail: string;
  name: string;
  subject: string;
  toEmail: string;
  toName?: string;
  userEmail?: string;
  accountId: number;
  userId: number;
  /** 0 收件，1 发件 */
  type: number;
  status: number;
  /** 0 未读，1 已读 */
  unread: number;
  isDel: number;
  isStar: number;
  content?: string;
  text?: string;
  cc?: string;
  bcc?: string;
  createTime: string;
  attList: SkymailAttachment[];
};

export type SkymailEmailPage = {
  total: number;
  latestEmail?: { emailId: number; accountId: number; userId: number } | null;
  list: SkymailEmail[];
};

export type SkymailUser = {
  userId: number;
  email: string;
  type: number;
  status: number;
  isDel: number;
  sendCount: number;
  createTime: string;
  activeTime: string;
  createIp: string;
  activeIp: string;
  os: string;
  browser: string;
  device: string;
  receiveEmailCount: number;
  sendEmailCount: number;
  accountCount: number;
  delReceiveEmailCount: number;
  delSendEmailCount: number;
  delAccountCount: number;
  username: string | null;
  name: string | null;
  avatar: string | null;
  platform: string | null;
  trustLevel: number | null;
  sendAction: { hasPerm: boolean; sendType: string; sendCount: number };
};

export type SkymailUserPage = { total: number; list: SkymailUser[] };

export type SkymailRegKey = {
  regKeyId: number;
  code: string;
  count: number;
  roleId: number;
  roleName: string;
  userId: number;
  /** null 表示已过期 */
  expireTime: string | null;
  createTime: string;
};

export type SkymailRegKeyUse = { email: string; createTime: string };

/** 资源名 → 参数与返回值。 */
export type SkymailQueries = {
  overview: { params: Record<string, never>; result: SkymailOverview };
  emails: {
    params: {
      accountId?: number;
      type?: 0 | 1;
      emailId?: number;
      size?: number;
      timeSort?: 0 | 1;
      allReceive?: 0 | 1;
      full?: 0 | 1;
    };
    result: SkymailEmailPage;
  };
  email: { params: { accountId?: number; type?: 0 | 1; emailId: number }; result: SkymailEmail };
  accounts: { params: { size?: number; accountId?: number; lastSort?: number }; result: SkymailAccount[] };
  users: {
    params: { num?: number; size?: number; email?: string; timeSort?: 0 | 1; status?: -1 | 0 | 1; isDel?: 0 | 1 };
    result: SkymailUserPage;
  };
  allEmails: {
    params: {
      emailId?: number;
      size?: number;
      timeSort?: 0 | 1;
      type?: "all" | "receive" | "send" | "delete" | "noone";
      name?: string;
      subject?: string;
      userEmail?: string;
      accountEmail?: string;
      full?: 0 | 1;
    };
    result: SkymailEmailPage;
  };
  allEmail: { params: { emailId: number }; result: SkymailEmail };
  regKeys: { params: { code?: string }; result: SkymailRegKey[] };
  regKeyHistory: { params: { regKeyId: number }; result: SkymailRegKeyUse[] };
  roles: { params: Record<string, never>; result: SkymailRole[] };
};

export type SkymailSendAttachment = { filename: string; content: string; contentType?: string };

export type SkymailActions = {
  send: {
    accountId?: number;
    receiveEmail: string[];
    subject: string;
    content: string;
    text?: string;
    name?: string;
    sendType?: "" | "reply" | "forward";
    emailId?: number;
    attachments?: SkymailSendAttachment[];
  };
  deleteEmails: { emailIds: number[] };
  addAccount: { email: string; token?: string };
  deleteAccount: { accountId: number };
  addUser: { email: string; password: string };
  setUserStatus: { userId: number; status: 0 | 1 };
  deleteUsers: { userIds: number[] };
  deleteAllEmails: { emailIds: number[] };
  addRegKey: { code: string; roleId: number; count: number; expireTime: string };
  deleteRegKeys: { regKeyIds: number[] };
};

export type SkymailActionResult = { ok: boolean; emails?: SkymailEmail[]; account?: SkymailAccount };

/** appId 为 null 时走平台级路由。 */
export function skymailQuery<R extends keyof SkymailQueries>(
  token: string,
  appId: number | null,
  configId: number,
  resource: R,
  params: SkymailQueries[R]["params"]
) {
  if (appId === null) {
    return apiRequest<SkymailQueries[R]["result"]>(`/api/admin/system/email/configs/${configId}/skymail/query`, {
      method: "POST",
      token,
      body: JSON.stringify({ resource, params })
    });
  }
  return apiRequest<SkymailQueries[R]["result"]>("/api/admin/app/email-config/skymail/query", {
    method: "POST",
    token,
    body: JSON.stringify({ appid: appId, config_id: configId, resource, params })
  });
}

export function skymailAction<A extends keyof SkymailActions>(
  token: string,
  appId: number | null,
  configId: number,
  action: A,
  params: SkymailActions[A]
) {
  if (appId === null) {
    return apiRequest<SkymailActionResult>(`/api/admin/system/email/configs/${configId}/skymail/action`, {
      method: "POST",
      token,
      body: JSON.stringify({ action, params })
    });
  }
  return apiRequest<SkymailActionResult>("/api/admin/app/email-config/skymail/action", {
    method: "POST",
    token,
    body: JSON.stringify({ appid: appId, config_id: configId, action, params })
  });
}

/** Cloud Mail 的邮件状态码。 */
export const SKYMAIL_STATUS: Record<number, { label: string; tone: "success" | "warning" | "danger" | "info" | "outline" | "secondary" }> = {
  0: { label: "已收到", tone: "secondary" },
  1: { label: "已发送", tone: "info" },
  2: { label: "已送达", tone: "success" },
  3: { label: "已退信", tone: "danger" },
  4: { label: "被投诉", tone: "warning" },
  5: { label: "延迟中", tone: "warning" },
  6: { label: "保存中", tone: "secondary" },
  7: { label: "无人收件", tone: "danger" },
  8: { label: "发送失败", tone: "danger" }
};
