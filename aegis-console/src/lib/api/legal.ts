import { apiRequest } from "./client";

/**
 * 法律文本（用户协议 / 隐私政策）。
 *
 * 正文**不在前端**：这两份文本此前是写死在组件里的两段 JSX，改一个字要发一次
 * 前端版本，加一种语言等于再抄一整份。现在由服务端下发，管理员可改，
 * 语言也由服务端协商 —— 前端只负责显示，以及把用户的语言偏好告诉服务端。
 */

export type LegalDocType = "terms" | "privacy";

export type LegalLocaleOption = {
  locale: string;
  nativeName: string;
  name: string;
  /** default = 系统内置版本，custom = 本部署管理员自己写的 */
  source: "default" | "custom";
  /** 是否是回落终点 */
  default: boolean;
  /** 是否是准据文本。多语言法律文本必须指定其中一版为准 */
  authoritative: boolean;
};

export type LegalDocument = {
  id?: number;
  docType: LegalDocType;
  locale: string;
  title: string;
  summary: string;
  /** 已由服务端净化的 HTML */
  body: string;
  version: string;
  effectiveAt?: string | null;
  published: boolean;
  source: "default" | "custom";
  updatedBy?: number | null;
  createdAt?: string | null;
  updatedAt?: string | null;
};

export type LegalDocumentView = LegalDocument & {
  locales: LegalLocaleOption[];
  /** 请求方要的语言。与 locale 不同即发生了回落 */
  requested?: string;
  /** 准据文本的语言。当前这份不是它时页面必须写明「本页为译文」 */
  authoritativeLocale: string;
};

export type LegalCatalogEntry = {
  docType: LegalDocType;
  title: string;
  locales: LegalLocaleOption[];
};

/* ── 公开读取（免登录） ── */

export function getLegalCatalog() {
  return apiRequest<{ items: LegalCatalogEntry[] }>("/api/legal/documents");
}

/**
 * 取一份法律文本。
 *
 * locale 是**偏好**不是断言：服务端拿它和 Accept-Language 一起协商，
 * 返回值里的 `locale` 才是真正给的那一份。前端不做任何语言判断 ——
 * 两端各挑各的，会出现同一页里标题和正文不是同一种语言。
 */
export function getLegalDocument(docType: LegalDocType, locale?: string) {
  const query = locale ? `?locale=${encodeURIComponent(locale)}` : "";
  return apiRequest<LegalDocumentView>(`/api/legal/documents/${docType}${query}`);
}

/* ── 管理端（超管） ── */

export type AdminLegalOverview = {
  items: LegalDocument[];
  /** 联系邮箱是否已配置。内置文本引用它，没配就会印出占位文字 */
  contactConfigured: boolean;
  /** 准据语言。译文与它有歧义时以它为准 */
  authoritativeLocale: string;
  /** 内置语言目录，「新增语言」选择器用它 —— 前端不另抄一份 */
  builtinLocales: LegalLocaleOption[];
};

export function getAdminLegalDocuments(token: string) {
  return apiRequest<AdminLegalOverview>("/api/admin/system/legal/documents", { token });
}

/**
 * 把草稿按当前部署的值渲染出来（替换 platformName / contactEmail 占位符）。
 *
 * 走服务端而不是前端自己替换：取值规则（品牌配置优先、未配置时印占位文字
 * 而不是生造地址）只在服务端实现一次，前端另写一套必然和公开页不一致 ——
 * 而预览的全部意义就是「所见即所得」。
 */
export function previewAdminLegalDocument(
  token: string,
  docType: LegalDocType,
  locale: string,
  body: string
) {
  return apiRequest<{ body: string }>(
    `/api/admin/system/legal/documents/${docType}/${encodeURIComponent(locale)}/preview`,
    { method: "POST", token, body: JSON.stringify({ body }) }
  );
}

export function getAdminLegalDocument(token: string, docType: LegalDocType, locale: string) {
  return apiRequest<LegalDocument>(
    `/api/admin/system/legal/documents/${docType}/${encodeURIComponent(locale)}`,
    { token }
  );
}

export type LegalDocumentPayload = {
  title: string;
  body: string;
  version?: string;
  /** YYYY-MM-DD 或 RFC3339，空串表示不设置 */
  effectiveAt?: string;
  published?: boolean;
};

export function saveAdminLegalDocument(
  token: string,
  docType: LegalDocType,
  locale: string,
  payload: LegalDocumentPayload
) {
  return apiRequest<LegalDocument>(
    `/api/admin/system/legal/documents/${docType}/${encodeURIComponent(locale)}`,
    { method: "PUT", token, body: JSON.stringify(payload) }
  );
}

/** 删除自定义版本，该语言回落到内置版本 */
export function deleteAdminLegalDocument(token: string, docType: LegalDocType, locale: string) {
  return apiRequest<{ deleted: boolean }>(
    `/api/admin/system/legal/documents/${docType}/${encodeURIComponent(locale)}`,
    { method: "DELETE", token }
  );
}
