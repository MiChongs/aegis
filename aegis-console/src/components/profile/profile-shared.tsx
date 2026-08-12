"use client";

/**
 * 个人资料页的共享目录、表单模型与原语。
 *
 * 三条约束写在这里，改这个页面前先读：
 *
 * 1. **服务端按「留空即不修改」处理。** 后端的更新语句是
 *    `SET email = COALESCE(NULLIF($3,''), email)`，每个标量字段都这样写 ——
 *    把邮箱框清空再保存，邮箱不会被清掉，原值原封不动。
 *    界面必须把这件事说出来，否则用户会以为自己删掉了一条联系方式，而它还在库里。
 *    `fieldNotices()` 生成这条提示，`changedFields()` 也据此把「清空」排除在
 *    变更计数之外 —— 它确实不会改变任何东西。
 *    唯一的例外是 `contacts`：整块 JSONB 覆盖写，可以真的删空。
 *
 * 2. **草稿不用 useEffect 同步**（与 `/apps` 配置面板同一条约束）：
 *    `form = draft ?? seedForm(server)`，保存成功后 `setDraft(null)` 让它重新从
 *    服务端派生。用 effect 同步既触发级联渲染、过不了
 *    `react-hooks/set-state-in-effect`，也会让后台刷新把正在编辑的内容冲掉。
 *
 * 3. **展示型原语一律复用 `users/detail/user-detail-shared`** 的 `Panel` / `Facts` /
 *    `Fact` / `StatTile`。在这里再抄一份，同一个"创建时间"就会在两个页面上
 *    长得不一样，而没有人说得清哪个才是对的。
 */

import * as React from "react";
import { AtSign, Check, Copy, Mail, MessageSquare, Phone } from "lucide-react";
import {
  SiDiscord,
  SiGithub,
  SiLine,
  SiQq,
  SiSignal,
  SiTelegram,
  SiWechat,
  SiWhatsapp,
  SiX
} from "@icons-pack/react-simple-icons";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { AdminAccount, AdminContactInfo } from "@/lib/api/types";

/* ------------------------------------------------------------------ */
/*  目录                                                               */
/* ------------------------------------------------------------------ */

/** id 用作 React key 与 Select value（全局唯一），code 为实际拨号前缀（会重复，如 +7） */
export type CountryEntry = { id: string; code: string; label: string; iso: string; pinyin: string };

export const countryCodes: CountryEntry[] = [
  // 东亚
  { id: "CN", code: "+86", label: "中国", iso: "CN", pinyin: "zhongguo china" },
  { id: "HK", code: "+852", label: "中国香港", iso: "HK", pinyin: "xianggang hongkong" },
  { id: "MO", code: "+853", label: "中国澳门", iso: "MO", pinyin: "aomen macao" },
  { id: "TW", code: "+886", label: "中国台湾", iso: "TW", pinyin: "taiwan" },
  { id: "JP", code: "+81", label: "日本", iso: "JP", pinyin: "riben japan" },
  { id: "KR", code: "+82", label: "韩国", iso: "KR", pinyin: "hanguo korea" },
  { id: "KP", code: "+850", label: "朝鲜", iso: "KP", pinyin: "chaoxian" },
  { id: "MN", code: "+976", label: "蒙古", iso: "MN", pinyin: "menggu mongolia" },
  // 东南亚
  { id: "SG", code: "+65", label: "新加坡", iso: "SG", pinyin: "xinjiapo singapore" },
  { id: "MY", code: "+60", label: "马来西亚", iso: "MY", pinyin: "malaixiya malaysia" },
  { id: "TH", code: "+66", label: "泰国", iso: "TH", pinyin: "taiguo thailand" },
  { id: "VN", code: "+84", label: "越南", iso: "VN", pinyin: "yuenan vietnam" },
  { id: "ID", code: "+62", label: "印度尼西亚", iso: "ID", pinyin: "yindunixiya indonesia" },
  { id: "PH", code: "+63", label: "菲律宾", iso: "PH", pinyin: "feilvbin philippines" },
  { id: "MM", code: "+95", label: "缅甸", iso: "MM", pinyin: "miandian myanmar" },
  { id: "KH", code: "+855", label: "柬埔寨", iso: "KH", pinyin: "jianpuzhai cambodia" },
  { id: "LA", code: "+856", label: "老挝", iso: "LA", pinyin: "laowo laos" },
  { id: "BN", code: "+673", label: "文莱", iso: "BN", pinyin: "wenlai brunei" },
  { id: "TL", code: "+670", label: "东帝汶", iso: "TL", pinyin: "dongdiwen timor" },
  // 南亚
  { id: "IN", code: "+91", label: "印度", iso: "IN", pinyin: "yindu india" },
  { id: "PK", code: "+92", label: "巴基斯坦", iso: "PK", pinyin: "bajisitan pakistan" },
  { id: "BD", code: "+880", label: "孟加拉国", iso: "BD", pinyin: "mengjialaguo bangladesh" },
  { id: "LK", code: "+94", label: "斯里兰卡", iso: "LK", pinyin: "sililanka srilanka" },
  { id: "NP", code: "+977", label: "尼泊尔", iso: "NP", pinyin: "niboer nepal" },
  { id: "BT", code: "+975", label: "不丹", iso: "BT", pinyin: "budan bhutan" },
  { id: "MV", code: "+960", label: "马尔代夫", iso: "MV", pinyin: "maerdaifu maldives" },
  { id: "AF", code: "+93", label: "阿富汗", iso: "AF", pinyin: "afuhan afghanistan" },
  // 中亚 / 西亚
  { id: "AE", code: "+971", label: "阿联酋", iso: "AE", pinyin: "alianqiu uae emirates" },
  { id: "SA", code: "+966", label: "沙特阿拉伯", iso: "SA", pinyin: "shate saudi" },
  { id: "TR", code: "+90", label: "土耳其", iso: "TR", pinyin: "tuerqi turkey" },
  { id: "IL", code: "+972", label: "以色列", iso: "IL", pinyin: "yiselie israel" },
  { id: "IR", code: "+98", label: "伊朗", iso: "IR", pinyin: "yilang iran" },
  { id: "IQ", code: "+964", label: "伊拉克", iso: "IQ", pinyin: "yilake iraq" },
  { id: "QA", code: "+974", label: "卡塔尔", iso: "QA", pinyin: "kataer qatar" },
  { id: "BH", code: "+973", label: "巴林", iso: "BH", pinyin: "balin bahrain" },
  { id: "OM", code: "+968", label: "阿曼", iso: "OM", pinyin: "aman oman" },
  { id: "KW", code: "+965", label: "科威特", iso: "KW", pinyin: "keweite kuwait" },
  { id: "JO", code: "+962", label: "约旦", iso: "JO", pinyin: "yuedan jordan" },
  { id: "LB", code: "+961", label: "黎巴嫩", iso: "LB", pinyin: "libanen lebanon" },
  { id: "SY", code: "+963", label: "叙利亚", iso: "SY", pinyin: "xuliya syria" },
  { id: "YE", code: "+967", label: "也门", iso: "YE", pinyin: "yemen" },
  { id: "AZ", code: "+994", label: "阿塞拜疆", iso: "AZ", pinyin: "asaibaijiang azerbaijan" },
  { id: "GE", code: "+995", label: "格鲁吉亚", iso: "GE", pinyin: "gelujiya georgia" },
  { id: "AM", code: "+374", label: "亚美尼亚", iso: "AM", pinyin: "yameiniya armenia" },
  { id: "KZ", code: "+7", label: "哈萨克斯坦", iso: "KZ", pinyin: "hasakesitan kazakhstan" },
  { id: "UZ", code: "+998", label: "乌兹别克斯坦", iso: "UZ", pinyin: "wuzibieke uzbekistan" },
  { id: "TM", code: "+993", label: "土库曼斯坦", iso: "TM", pinyin: "tukumansitan turkmenistan" },
  { id: "KG", code: "+996", label: "吉尔吉斯斯坦", iso: "KG", pinyin: "jierjisi kyrgyzstan" },
  { id: "TJ", code: "+992", label: "塔吉克斯坦", iso: "TJ", pinyin: "tajikesitan tajikistan" },
  // 北美
  { id: "US", code: "+1", label: "美国/加拿大", iso: "US", pinyin: "meiguo jianada usa canada" },
  { id: "MX", code: "+52", label: "墨西哥", iso: "MX", pinyin: "moxige mexico" },
  // 中美洲 / 加勒比
  { id: "GT", code: "+502", label: "危地马拉", iso: "GT", pinyin: "weidimala guatemala" },
  { id: "SV", code: "+503", label: "萨尔瓦多", iso: "SV", pinyin: "saerwaduo salvador" },
  { id: "HN", code: "+504", label: "洪都拉斯", iso: "HN", pinyin: "hongdulasi honduras" },
  { id: "NI", code: "+505", label: "尼加拉瓜", iso: "NI", pinyin: "nijialagua nicaragua" },
  { id: "CR", code: "+506", label: "哥斯达黎加", iso: "CR", pinyin: "gesidalijia costarica" },
  { id: "PA", code: "+507", label: "巴拿马", iso: "PA", pinyin: "banama panama" },
  { id: "CU", code: "+53", label: "古巴", iso: "CU", pinyin: "guba cuba" },
  { id: "HT", code: "+509", label: "海地", iso: "HT", pinyin: "haidi haiti" },
  { id: "JM", code: "+1876", label: "牙买加", iso: "JM", pinyin: "yamaijia jamaica" },
  { id: "TT", code: "+1868", label: "特立尼达", iso: "TT", pinyin: "telinida trinidad" },
  // 南美
  { id: "BR", code: "+55", label: "巴西", iso: "BR", pinyin: "baxi brazil" },
  { id: "AR", code: "+54", label: "阿根廷", iso: "AR", pinyin: "agenting argentina" },
  { id: "CL", code: "+56", label: "智利", iso: "CL", pinyin: "zhili chile" },
  { id: "CO", code: "+57", label: "哥伦比亚", iso: "CO", pinyin: "gelunbiya colombia" },
  { id: "VE", code: "+58", label: "委内瑞拉", iso: "VE", pinyin: "weineiruila venezuela" },
  { id: "PE", code: "+51", label: "秘鲁", iso: "PE", pinyin: "milu peru" },
  { id: "EC", code: "+593", label: "厄瓜多尔", iso: "EC", pinyin: "eguaduoer ecuador" },
  { id: "BO", code: "+591", label: "玻利维亚", iso: "BO", pinyin: "boliweiya bolivia" },
  { id: "PY", code: "+595", label: "巴拉圭", iso: "PY", pinyin: "balagui paraguay" },
  { id: "UY", code: "+598", label: "乌拉圭", iso: "UY", pinyin: "wulagui uruguay" },
  // 西欧
  { id: "GB", code: "+44", label: "英国", iso: "GB", pinyin: "yingguo uk britain" },
  { id: "FR", code: "+33", label: "法国", iso: "FR", pinyin: "faguo france" },
  { id: "DE", code: "+49", label: "德国", iso: "DE", pinyin: "deguo germany" },
  { id: "IT", code: "+39", label: "意大利", iso: "IT", pinyin: "yidali italy" },
  { id: "ES", code: "+34", label: "西班牙", iso: "ES", pinyin: "xibanya spain" },
  { id: "PT", code: "+351", label: "葡萄牙", iso: "PT", pinyin: "putaoya portugal" },
  { id: "NL", code: "+31", label: "荷兰", iso: "NL", pinyin: "helan netherlands" },
  { id: "BE", code: "+32", label: "比利时", iso: "BE", pinyin: "bilishi belgium" },
  { id: "CH", code: "+41", label: "瑞士", iso: "CH", pinyin: "ruishi switzerland" },
  { id: "AT", code: "+43", label: "奥地利", iso: "AT", pinyin: "aodili austria" },
  { id: "IE", code: "+353", label: "爱尔兰", iso: "IE", pinyin: "aierlan ireland" },
  { id: "LU", code: "+352", label: "卢森堡", iso: "LU", pinyin: "lusenbao luxembourg" },
  // 北欧
  { id: "SE", code: "+46", label: "瑞典", iso: "SE", pinyin: "ruidian sweden" },
  { id: "NO", code: "+47", label: "挪威", iso: "NO", pinyin: "nuowei norway" },
  { id: "DK", code: "+45", label: "丹麦", iso: "DK", pinyin: "danmai denmark" },
  { id: "FI", code: "+358", label: "芬兰", iso: "FI", pinyin: "fenlan finland" },
  { id: "IS", code: "+354", label: "冰岛", iso: "IS", pinyin: "bingdao iceland" },
  // 东欧
  { id: "RU", code: "+7", label: "俄罗斯", iso: "RU", pinyin: "eluosi russia" },
  { id: "UA", code: "+380", label: "乌克兰", iso: "UA", pinyin: "wukelan ukraine" },
  { id: "PL", code: "+48", label: "波兰", iso: "PL", pinyin: "bolan poland" },
  { id: "CZ", code: "+420", label: "捷克", iso: "CZ", pinyin: "jieke czech" },
  { id: "SK", code: "+421", label: "斯洛伐克", iso: "SK", pinyin: "siluofake slovakia" },
  { id: "HU", code: "+36", label: "匈牙利", iso: "HU", pinyin: "xiongyali hungary" },
  { id: "RO", code: "+40", label: "罗马尼亚", iso: "RO", pinyin: "luomaniya romania" },
  { id: "BG", code: "+359", label: "保加利亚", iso: "BG", pinyin: "baojialiya bulgaria" },
  { id: "RS", code: "+381", label: "塞尔维亚", iso: "RS", pinyin: "saierweiya serbia" },
  { id: "HR", code: "+385", label: "克罗地亚", iso: "HR", pinyin: "keluodiya croatia" },
  { id: "SI", code: "+386", label: "斯洛文尼亚", iso: "SI", pinyin: "siluoweiniya slovenia" },
  { id: "BA", code: "+387", label: "波黑", iso: "BA", pinyin: "bohei bosnia" },
  { id: "AL", code: "+355", label: "阿尔巴尼亚", iso: "AL", pinyin: "aerbaniya albania" },
  { id: "MK", code: "+389", label: "北马其顿", iso: "MK", pinyin: "maqidun macedonia" },
  { id: "ME", code: "+382", label: "黑山", iso: "ME", pinyin: "heishan montenegro" },
  { id: "LT", code: "+370", label: "立陶宛", iso: "LT", pinyin: "litaowan lithuania" },
  { id: "LV", code: "+371", label: "拉脱维亚", iso: "LV", pinyin: "latuoweiya latvia" },
  { id: "EE", code: "+372", label: "爱沙尼亚", iso: "EE", pinyin: "aishaniya estonia" },
  { id: "BY", code: "+375", label: "白俄罗斯", iso: "BY", pinyin: "baieluosi belarus" },
  { id: "MD", code: "+373", label: "摩尔多瓦", iso: "MD", pinyin: "moerduowa moldova" },
  { id: "GR", code: "+30", label: "希腊", iso: "GR", pinyin: "xila greece" },
  // 大洋洲
  { id: "AU", code: "+61", label: "澳大利亚", iso: "AU", pinyin: "aodaliya australia" },
  { id: "NZ", code: "+64", label: "新西兰", iso: "NZ", pinyin: "xinxilan newzealand" },
  { id: "PG", code: "+675", label: "巴布亚新几内亚", iso: "PG", pinyin: "babuya papua" },
  { id: "FJ", code: "+679", label: "斐济", iso: "FJ", pinyin: "feiji fiji" },
  { id: "WS", code: "+685", label: "萨摩亚", iso: "WS", pinyin: "samoya samoa" },
  { id: "TO", code: "+676", label: "汤加", iso: "TO", pinyin: "tangjia tonga" },
  // 非洲
  { id: "EG", code: "+20", label: "埃及", iso: "EG", pinyin: "aiji egypt" },
  { id: "ZA", code: "+27", label: "南非", iso: "ZA", pinyin: "nanfei southafrica" },
  { id: "NG", code: "+234", label: "尼日利亚", iso: "NG", pinyin: "nirliya nigeria" },
  { id: "KE", code: "+254", label: "肯尼亚", iso: "KE", pinyin: "kenniya kenya" },
  { id: "TZ", code: "+255", label: "坦桑尼亚", iso: "TZ", pinyin: "tansangniya tanzania" },
  { id: "UG", code: "+256", label: "乌干达", iso: "UG", pinyin: "wuganda uganda" },
  { id: "ET", code: "+251", label: "埃塞俄比亚", iso: "ET", pinyin: "aisaiebiya ethiopia" },
  { id: "GH", code: "+233", label: "加纳", iso: "GH", pinyin: "jiana ghana" },
  { id: "CI", code: "+225", label: "科特迪瓦", iso: "CI", pinyin: "ketediwa ivory" },
  { id: "SN", code: "+221", label: "塞内加尔", iso: "SN", pinyin: "saineijiaer senegal" },
  { id: "CM", code: "+237", label: "喀麦隆", iso: "CM", pinyin: "kamailong cameroon" },
  { id: "MA", code: "+212", label: "摩洛哥", iso: "MA", pinyin: "moluoge morocco" },
  { id: "DZ", code: "+213", label: "阿尔及利亚", iso: "DZ", pinyin: "aerjiliya algeria" },
  { id: "TN", code: "+216", label: "突尼斯", iso: "TN", pinyin: "tunisi tunisia" },
  { id: "LY", code: "+218", label: "利比亚", iso: "LY", pinyin: "libiya libya" },
  { id: "SD", code: "+249", label: "苏丹", iso: "SD", pinyin: "sudan" },
  { id: "ZM", code: "+260", label: "赞比亚", iso: "ZM", pinyin: "zanbiya zambia" },
  { id: "ZW", code: "+263", label: "津巴布韦", iso: "ZW", pinyin: "jinbabuwei zimbabwe" },
  { id: "MZ", code: "+258", label: "莫桑比克", iso: "MZ", pinyin: "mosangbike mozambique" },
  { id: "MG", code: "+261", label: "马达加斯加", iso: "MG", pinyin: "madajiasijia madagascar" },
  { id: "RW", code: "+250", label: "卢旺达", iso: "RW", pinyin: "luwangda rwanda" },
  { id: "CD", code: "+243", label: "刚果(金)", iso: "CD", pinyin: "gangguo congo" },
  { id: "CG", code: "+242", label: "刚果(布)", iso: "CG", pinyin: "gangguo congo" }
];

type IconComponent = React.ComponentType<{ className?: string }>;

/**
 * 联系方式平台目录。
 *
 * `placeholder` 逐平台不同不是装饰：微信填的是微信号、Telegram 填的是 @用户名、
 * GitHub 填的是用户名 —— 一个通用的「账号 / 号码」占位符等于什么都没说。
 * Slack 因商标原因未被 Simple Icons 收录，用中性图标兜底而不冒充品牌。
 */
export type ContactPlatform = {
  value: string;
  label: string;
  Icon: IconComponent;
  placeholder: string;
};

export const contactPlatforms: ContactPlatform[] = [
  { value: "wechat", label: "微信", Icon: SiWechat, placeholder: "微信号" },
  { value: "qq", label: "QQ", Icon: SiQq, placeholder: "QQ 号" },
  { value: "telegram", label: "Telegram", Icon: SiTelegram, placeholder: "@username" },
  { value: "discord", label: "Discord", Icon: SiDiscord, placeholder: "username" },
  { value: "twitter", label: "Twitter / X", Icon: SiX, placeholder: "@handle" },
  { value: "github", label: "GitHub", Icon: SiGithub, placeholder: "username" },
  { value: "whatsapp", label: "WhatsApp", Icon: SiWhatsapp, placeholder: "+86 138 0000 0000" },
  { value: "line", label: "LINE", Icon: SiLine, placeholder: "LINE ID" },
  { value: "signal", label: "Signal", Icon: SiSignal, placeholder: "+86 138 0000 0000" },
  { value: "slack", label: "Slack", Icon: MessageSquare, placeholder: "@member" },
  { value: "phone", label: "手机号", Icon: Phone, placeholder: "+86 138 0000 0000" },
  { value: "email", label: "邮箱", Icon: Mail, placeholder: "name@example.com" },
  { value: "other", label: "其他", Icon: AtSign, placeholder: "账号 / 号码" }
];

export function contactPlatformOf(value?: string): ContactPlatform {
  return contactPlatforms.find((item) => item.value === value) || contactPlatforms[contactPlatforms.length - 1];
}

/** 认证来源的中文名。本地密码之外的三种都由平台级 SSO 配置决定，个人改不了。 */
export function authSourceLabel(source?: string) {
  switch (source) {
    case "ldap":
      return "LDAP / AD 域";
    case "oidc":
      return "OIDC 单点登录";
    case "saml":
      return "SAML 单点登录";
    default:
      return "本地密码";
  }
}

/* ------------------------------------------------------------------ */
/*  表单模型                                                            */
/* ------------------------------------------------------------------ */

export const BIO_MAX = 200;
export const DISPLAY_NAME_MAX = 32;

export type ContactDraft = AdminContactInfo & { uid: string };

export type ProfileForm = {
  displayName: string;
  email: string;
  /** 区号所属国家的 id（不是拨号前缀，+7 被俄罗斯与哈萨克斯坦共用） */
  countryId: string;
  /** 号码正文，不含区号 */
  phone: string;
  /** yyyy-MM-dd */
  birthday: string;
  bio: string;
  contacts: ContactDraft[];
};

export type ProfileFieldKey = keyof Omit<ProfileForm, "contacts" | "countryId"> | "contacts";

let contactSeq = 0;
export function nextContactUid() {
  contactSeq += 1;
  return `contact-${contactSeq}`;
}

function parsePhone(full?: string): { countryId: string; phone: string } {
  if (!full) return { countryId: "CN", phone: "" };
  const trimmed = full.trim();
  const match = trimmed.match(/^(\+\d{1,4})\s*(.*)$/);
  if (!match) return { countryId: "CN", phone: trimmed };
  const found = countryCodes.find((item) => item.code === match[1]);
  return { countryId: found?.id || "CN", phone: match[2] };
}

export function composePhone(countryId: string, phone: string) {
  const digits = phone.trim();
  if (!digits) return "";
  const entry = countryCodes.find((item) => item.id === countryId);
  return `${entry?.code || "+86"} ${digits}`;
}

/** yyyy-MM-dd，按本地时区取，不能用 toISOString（那会因时区把生日挪一天） */
export function toDateInput(date: Date) {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function fromDateInput(value: string): Date | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined;
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(year, month - 1, day);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function normalizeBirthday(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  return toDateInput(date);
}

export function seedForm(account?: AdminAccount): ProfileForm {
  const parsed = parsePhone(account?.phone);
  return {
    displayName: account?.displayName || "",
    email: account?.email || "",
    countryId: parsed.countryId,
    phone: parsed.phone,
    birthday: normalizeBirthday(account?.birthday),
    bio: account?.bio || "",
    contacts: (account?.contacts || []).map((item) => ({
      uid: nextContactUid(),
      platform: item.platform,
      value: item.value,
      label: item.label || ""
    }))
  };
}

/** 提交用的载荷。空值的联系方式在这里被丢掉 —— 界面上对应行会先给出提示。 */
export function toPayload(form: ProfileForm) {
  return {
    displayName: form.displayName.trim(),
    email: form.email.trim(),
    phone: composePhone(form.countryId, form.phone),
    birthday: form.birthday,
    bio: form.bio.trim(),
    contacts: form.contacts
      .filter((item) => item.value.trim())
      .map((item) => ({
        platform: item.platform,
        value: item.value.trim(),
        label: item.label?.trim() || undefined
      }))
  };
}

/* ------------------------------------------------------------------ */
/*  校验与变更                                                          */
/* ------------------------------------------------------------------ */

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

export type ProfileIssues = {
  /** 会挡住保存 */
  errors: Partial<Record<ProfileFieldKey, string>>;
  /** 不挡保存，只是说清楚"这么做不会有你以为的效果" */
  notices: Partial<Record<ProfileFieldKey, string>>;
  /** uid → 该行的问题 */
  contactErrors: Record<string, string>;
};

export function validateForm(form: ProfileForm, server: ProfileForm): ProfileIssues {
  const errors: ProfileIssues["errors"] = {};
  const notices: ProfileIssues["notices"] = {};
  const contactErrors: ProfileIssues["contactErrors"] = {};

  if (form.displayName.trim().length > DISPLAY_NAME_MAX) {
    errors.displayName = `不能超过 ${DISPLAY_NAME_MAX} 个字符`;
  }
  if (form.email.trim() && !EMAIL_PATTERN.test(form.email.trim())) {
    errors.email = "邮箱格式不正确";
  }
  const digits = form.phone.replace(/[^\d]/g, "");
  if (form.phone.trim() && (digits.length < 5 || digits.length > 15)) {
    errors.phone = "号码位数看起来不对（5–15 位）";
  }
  if (form.birthday) {
    const date = fromDateInput(form.birthday);
    if (!date) errors.birthday = "日期格式不正确";
    else if (date.getTime() > Date.now()) errors.birthday = "出生日期不能晚于今天";
    else if (date.getFullYear() < 1900) errors.birthday = "年份看起来不对";
  }
  if (form.bio.trim().length > BIO_MAX) {
    errors.bio = `不能超过 ${BIO_MAX} 个字符`;
  }

  // 「留空即不修改」：清空一个原本有值的字段不会生效，必须说出来
  const clearable: Array<[Exclude<ProfileFieldKey, "contacts">, string]> = [
    ["displayName", "显示名称"],
    ["email", "邮箱"],
    ["phone", "手机号"],
    ["birthday", "出生日期"],
    ["bio", "个人简介"]
  ];
  for (const [key, label] of clearable) {
    if (!form[key].trim() && server[key].trim()) {
      notices[key] = `留空不会清除已有的${label}，保存后仍是原值`;
    }
  }

  const seen = new Map<string, string>();
  for (const item of form.contacts) {
    const value = item.value.trim();
    if (!value) {
      contactErrors[item.uid] = "留空的这条在保存时会被删除";
      continue;
    }
    const dedupeKey = `${item.platform}:${value.toLowerCase()}`;
    if (seen.has(dedupeKey)) contactErrors[item.uid] = "与上面某一条重复";
    else seen.set(dedupeKey, item.uid);
  }

  return { errors, notices, contactErrors };
}

export function hasBlockingError(issues: ProfileIssues) {
  return (
    Object.keys(issues.errors).length > 0 ||
    Object.values(issues.contactErrors).some((message) => message === "与上面某一条重复")
  );
}

function contactsSignature(contacts: ContactDraft[]) {
  return JSON.stringify(
    contacts
      .filter((item) => item.value.trim())
      .map((item) => [item.platform, item.value.trim(), item.label?.trim() || ""])
  );
}

/**
 * 真正会被写入的变更项。
 *
 * 「清空了一个原本有值的字段」**不算**变更 —— 服务端会原样保留，
 * 把它计进去等于告诉用户"有一处改动待保存"，而保存之后什么都没变。
 */
export function changedFields(form: ProfileForm, server: ProfileForm): string[] {
  const changed: string[] = [];
  const scalars: Array<[Exclude<ProfileFieldKey, "contacts">, string]> = [
    ["displayName", "显示名称"],
    ["email", "邮箱"],
    ["birthday", "出生日期"],
    ["bio", "个人简介"]
  ];
  for (const [key, label] of scalars) {
    const next = form[key].trim();
    if (next && next !== server[key].trim()) changed.push(label);
  }
  const nextPhone = composePhone(form.countryId, form.phone);
  const prevPhone = composePhone(server.countryId, server.phone);
  if (nextPhone && nextPhone !== prevPhone) changed.push("手机号");
  if (contactsSignature(form.contacts) !== contactsSignature(server.contacts)) changed.push("联系方式");
  return changed;
}

/* ------------------------------------------------------------------ */
/*  原语                                                               */
/* ------------------------------------------------------------------ */

/**
 * 表单字段外壳：标签 + 控件 + 一行说明。
 *
 * `error` 与 `notice` 都存在时只显示 error —— 同一行叠两条说明，
 * 人只会读到第一条，那不如只给最重要的那条。
 */
export function Field({
  label,
  htmlFor,
  icon,
  hint,
  error,
  notice,
  counter,
  children,
  className
}: {
  label: React.ReactNode;
  htmlFor?: string;
  icon?: React.ReactNode;
  hint?: React.ReactNode;
  error?: string;
  notice?: string;
  counter?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  const message = error || notice || hint;
  const tone = error ? "text-destructive" : notice ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground";

  return (
    <div className={cn("space-y-1.5", className)}>
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={htmlFor} className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {icon}
          {label}
        </Label>
        {counter}
      </div>
      {children}
      {message ? <p className={cn("text-[11px] leading-4", tone)}>{message}</p> : null}
    </div>
  );
}

/** 一键复制。ID、账号、Token 这类东西是拿来贴到别处的，不是拿来手抄的。 */
export function CopyButton({ value, label = "复制" }: { value: string; label?: string }) {
  const [copied, setCopied] = React.useState(false);
  const timer = React.useRef<number | null>(null);

  React.useEffect(() => () => {
    if (timer.current) window.clearTimeout(timer.current);
  }, []);

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={label}
          className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
          onClick={() => {
            void navigator.clipboard.writeText(value);
            setCopied(true);
            if (timer.current) window.clearTimeout(timer.current);
            timer.current = window.setTimeout(() => setCopied(false), 1400);
          }}
        >
          {copied ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{copied ? "已复制" : label}</TooltipContent>
    </Tooltip>
  );
}

/** 可复制的值：文本 + 悬浮才出现的复制按钮，常驻按钮会让每一行都挂着一个图标 */
export function CopyableValue({ value, mono = true }: { value: string; mono?: boolean }) {
  return (
    <span className="group/copy inline-flex items-center gap-1">
      <span className={cn("break-all", mono && "font-mono text-[13px]")}>{value}</span>
      <span className="opacity-0 transition-opacity group-hover/copy:opacity-100 focus-within:opacity-100">
        <CopyButton value={value} />
      </span>
    </span>
  );
}
