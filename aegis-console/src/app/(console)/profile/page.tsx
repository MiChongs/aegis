"use client";

import { ChangeEvent, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  Calendar,
  Camera,
  CirclePlus,
  KeyRound,
  Loader2,
  MessageCircle,
  Phone,
  Save,
  Shield,
  User,
  X
} from "lucide-react";
import { toast } from "sonner";
import type { AdminContactInfo } from "@/lib/api-client";
import { ApiError } from "@/lib/api-client";
import {
  useAdminProfileQuery,
  useAdminSessionQuery,
  useAdminRolePermissionTreeQuery,
  useUpdateAdminProfileMutation,
  useUploadAdminAvatarMutation
} from "@/lib/admin-hooks";
import { avatarUrl, useAuthStore } from "@/lib/auth-store";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { CountryFlag } from "@/components/ui/country-flag";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { PermissionTree } from "@/components/users/permission-tree";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  常量                                                               */
/* ------------------------------------------------------------------ */

// id 用作 React key + Select value（全局唯一），code 为实际拨号前缀
const countryCodes: Array<{ id: string; code: string; label: string; iso: string }> = [
  // 东亚
  { id: "CN", code: "+86", label: "中国", iso: "CN" },
  { id: "HK", code: "+852", label: "中国香港", iso: "HK" },
  { id: "MO", code: "+853", label: "中国澳门", iso: "MO" },
  { id: "TW", code: "+886", label: "中国台湾", iso: "TW" },
  { id: "JP", code: "+81", label: "日本", iso: "JP" },
  { id: "KR", code: "+82", label: "韩国", iso: "KR" },
  { id: "KP", code: "+850", label: "朝鲜", iso: "KP" },
  { id: "MN", code: "+976", label: "蒙古", iso: "MN" },
  // 东南亚
  { id: "SG", code: "+65", label: "新加坡", iso: "SG" },
  { id: "MY", code: "+60", label: "马来西亚", iso: "MY" },
  { id: "TH", code: "+66", label: "泰国", iso: "TH" },
  { id: "VN", code: "+84", label: "越南", iso: "VN" },
  { id: "ID", code: "+62", label: "印度尼西亚", iso: "ID" },
  { id: "PH", code: "+63", label: "菲律宾", iso: "PH" },
  { id: "MM", code: "+95", label: "缅甸", iso: "MM" },
  { id: "KH", code: "+855", label: "柬埔寨", iso: "KH" },
  { id: "LA", code: "+856", label: "老挝", iso: "LA" },
  { id: "BN", code: "+673", label: "文莱", iso: "BN" },
  { id: "TL", code: "+670", label: "东帝汶", iso: "TL" },
  // 南亚
  { id: "IN", code: "+91", label: "印度", iso: "IN" },
  { id: "PK", code: "+92", label: "巴基斯坦", iso: "PK" },
  { id: "BD", code: "+880", label: "孟加拉国", iso: "BD" },
  { id: "LK", code: "+94", label: "斯里兰卡", iso: "LK" },
  { id: "NP", code: "+977", label: "尼泊尔", iso: "NP" },
  { id: "BT", code: "+975", label: "不丹", iso: "BT" },
  { id: "MV", code: "+960", label: "马尔代夫", iso: "MV" },
  { id: "AF", code: "+93", label: "阿富汗", iso: "AF" },
  // 中亚 / 西亚
  { id: "AE", code: "+971", label: "阿联酋", iso: "AE" },
  { id: "SA", code: "+966", label: "沙特阿拉伯", iso: "SA" },
  { id: "TR", code: "+90", label: "土耳其", iso: "TR" },
  { id: "IL", code: "+972", label: "以色列", iso: "IL" },
  { id: "IR", code: "+98", label: "伊朗", iso: "IR" },
  { id: "IQ", code: "+964", label: "伊拉克", iso: "IQ" },
  { id: "QA", code: "+974", label: "卡塔尔", iso: "QA" },
  { id: "BH", code: "+973", label: "巴林", iso: "BH" },
  { id: "OM", code: "+968", label: "阿曼", iso: "OM" },
  { id: "KW", code: "+965", label: "科威特", iso: "KW" },
  { id: "JO", code: "+962", label: "约旦", iso: "JO" },
  { id: "LB", code: "+961", label: "黎巴嫩", iso: "LB" },
  { id: "SY", code: "+963", label: "叙利亚", iso: "SY" },
  { id: "YE", code: "+967", label: "也门", iso: "YE" },
  { id: "AZ", code: "+994", label: "阿塞拜疆", iso: "AZ" },
  { id: "GE", code: "+995", label: "格鲁吉亚", iso: "GE" },
  { id: "AM", code: "+374", label: "亚美尼亚", iso: "AM" },
  { id: "KZ", code: "+7", label: "哈萨克斯坦", iso: "KZ" },
  { id: "UZ", code: "+998", label: "乌兹别克斯坦", iso: "UZ" },
  { id: "TM", code: "+993", label: "土库曼斯坦", iso: "TM" },
  { id: "KG", code: "+996", label: "吉尔吉斯斯坦", iso: "KG" },
  { id: "TJ", code: "+992", label: "塔吉克斯坦", iso: "TJ" },
  // 北美
  { id: "US", code: "+1", label: "美国/加拿大", iso: "US" },
  { id: "MX", code: "+52", label: "墨西哥", iso: "MX" },
  // 中美洲 / 加勒比
  { id: "GT", code: "+502", label: "危地马拉", iso: "GT" },
  { id: "SV", code: "+503", label: "萨尔瓦多", iso: "SV" },
  { id: "HN", code: "+504", label: "洪都拉斯", iso: "HN" },
  { id: "NI", code: "+505", label: "尼加拉瓜", iso: "NI" },
  { id: "CR", code: "+506", label: "哥斯达黎加", iso: "CR" },
  { id: "PA", code: "+507", label: "巴拿马", iso: "PA" },
  { id: "CU", code: "+53", label: "古巴", iso: "CU" },
  { id: "HT", code: "+509", label: "海地", iso: "HT" },
  { id: "JM", code: "+1876", label: "牙买加", iso: "JM" },
  { id: "TT", code: "+1868", label: "特立尼达", iso: "TT" },
  // 南美
  { id: "BR", code: "+55", label: "巴西", iso: "BR" },
  { id: "AR", code: "+54", label: "阿根廷", iso: "AR" },
  { id: "CL", code: "+56", label: "智利", iso: "CL" },
  { id: "CO", code: "+57", label: "哥伦比亚", iso: "CO" },
  { id: "VE", code: "+58", label: "委内瑞拉", iso: "VE" },
  { id: "PE", code: "+51", label: "秘鲁", iso: "PE" },
  { id: "EC", code: "+593", label: "厄瓜多尔", iso: "EC" },
  { id: "BO", code: "+591", label: "玻利维亚", iso: "BO" },
  { id: "PY", code: "+595", label: "巴拉圭", iso: "PY" },
  { id: "UY", code: "+598", label: "乌拉圭", iso: "UY" },
  // 西欧
  { id: "GB", code: "+44", label: "英国", iso: "GB" },
  { id: "FR", code: "+33", label: "法国", iso: "FR" },
  { id: "DE", code: "+49", label: "德国", iso: "DE" },
  { id: "IT", code: "+39", label: "意大利", iso: "IT" },
  { id: "ES", code: "+34", label: "西班牙", iso: "ES" },
  { id: "PT", code: "+351", label: "葡萄牙", iso: "PT" },
  { id: "NL", code: "+31", label: "荷兰", iso: "NL" },
  { id: "BE", code: "+32", label: "比利时", iso: "BE" },
  { id: "CH", code: "+41", label: "瑞士", iso: "CH" },
  { id: "AT", code: "+43", label: "奥地利", iso: "AT" },
  { id: "IE", code: "+353", label: "爱尔兰", iso: "IE" },
  { id: "LU", code: "+352", label: "卢森堡", iso: "LU" },
  // 北欧
  { id: "SE", code: "+46", label: "瑞典", iso: "SE" },
  { id: "NO", code: "+47", label: "挪威", iso: "NO" },
  { id: "DK", code: "+45", label: "丹麦", iso: "DK" },
  { id: "FI", code: "+358", label: "芬兰", iso: "FI" },
  { id: "IS", code: "+354", label: "冰岛", iso: "IS" },
  // 东欧
  { id: "RU", code: "+7", label: "俄罗斯", iso: "RU" },
  { id: "UA", code: "+380", label: "乌克兰", iso: "UA" },
  { id: "PL", code: "+48", label: "波兰", iso: "PL" },
  { id: "CZ", code: "+420", label: "捷克", iso: "CZ" },
  { id: "SK", code: "+421", label: "斯洛伐克", iso: "SK" },
  { id: "HU", code: "+36", label: "匈牙利", iso: "HU" },
  { id: "RO", code: "+40", label: "罗马尼亚", iso: "RO" },
  { id: "BG", code: "+359", label: "保加利亚", iso: "BG" },
  { id: "RS", code: "+381", label: "塞尔维亚", iso: "RS" },
  { id: "HR", code: "+385", label: "克罗地亚", iso: "HR" },
  { id: "SI", code: "+386", label: "斯洛文尼亚", iso: "SI" },
  { id: "BA", code: "+387", label: "波黑", iso: "BA" },
  { id: "AL", code: "+355", label: "阿尔巴尼亚", iso: "AL" },
  { id: "MK", code: "+389", label: "北马其顿", iso: "MK" },
  { id: "ME", code: "+382", label: "黑山", iso: "ME" },
  { id: "LT", code: "+370", label: "立陶宛", iso: "LT" },
  { id: "LV", code: "+371", label: "拉脱维亚", iso: "LV" },
  { id: "EE", code: "+372", label: "爱沙尼亚", iso: "EE" },
  { id: "BY", code: "+375", label: "白俄罗斯", iso: "BY" },
  { id: "MD", code: "+373", label: "摩尔多瓦", iso: "MD" },
  { id: "GR", code: "+30", label: "希腊", iso: "GR" },
  // 大洋洲
  { id: "AU", code: "+61", label: "澳大利亚", iso: "AU" },
  { id: "NZ", code: "+64", label: "新西兰", iso: "NZ" },
  { id: "PG", code: "+675", label: "巴布亚新几内亚", iso: "PG" },
  { id: "FJ", code: "+679", label: "斐济", iso: "FJ" },
  { id: "WS", code: "+685", label: "萨摩亚", iso: "WS" },
  { id: "TO", code: "+676", label: "汤加", iso: "TO" },
  // 非洲
  { id: "EG", code: "+20", label: "埃及", iso: "EG" },
  { id: "ZA", code: "+27", label: "南非", iso: "ZA" },
  { id: "NG", code: "+234", label: "尼日利亚", iso: "NG" },
  { id: "KE", code: "+254", label: "肯尼亚", iso: "KE" },
  { id: "TZ", code: "+255", label: "坦桑尼亚", iso: "TZ" },
  { id: "UG", code: "+256", label: "乌干达", iso: "UG" },
  { id: "ET", code: "+251", label: "埃塞俄比亚", iso: "ET" },
  { id: "GH", code: "+233", label: "加纳", iso: "GH" },
  { id: "CI", code: "+225", label: "科特迪瓦", iso: "CI" },
  { id: "SN", code: "+221", label: "塞内加尔", iso: "SN" },
  { id: "CM", code: "+237", label: "喀麦隆", iso: "CM" },
  { id: "MA", code: "+212", label: "摩洛哥", iso: "MA" },
  { id: "DZ", code: "+213", label: "阿尔及利亚", iso: "DZ" },
  { id: "TN", code: "+216", label: "突尼斯", iso: "TN" },
  { id: "LY", code: "+218", label: "利比亚", iso: "LY" },
  { id: "SD", code: "+249", label: "苏丹", iso: "SD" },
  { id: "ZM", code: "+260", label: "赞比亚", iso: "ZM" },
  { id: "ZW", code: "+263", label: "津巴布韦", iso: "ZW" },
  { id: "MZ", code: "+258", label: "莫桑比克", iso: "MZ" },
  { id: "MG", code: "+261", label: "马达加斯加", iso: "MG" },
  { id: "RW", code: "+250", label: "卢旺达", iso: "RW" },
  { id: "CD", code: "+243", label: "刚果(金)", iso: "CD" },
  { id: "CG", code: "+242", label: "刚果(布)", iso: "CG" },
];

const contactPlatforms = [
  { value: "wechat", label: "微信" },
  { value: "qq", label: "QQ" },
  { value: "telegram", label: "Telegram" },
  { value: "discord", label: "Discord" },
  { value: "twitter", label: "Twitter / X" },
  { value: "github", label: "GitHub" },
  { value: "phone", label: "手机号" },
  { value: "email", label: "邮箱" },
  { value: "whatsapp", label: "WhatsApp" },
  { value: "line", label: "LINE" },
  { value: "signal", label: "Signal" },
  { value: "slack", label: "Slack" },
  { value: "other", label: "其他" },
];

/* ------------------------------------------------------------------ */
/*  工具                                                               */
/* ------------------------------------------------------------------ */

function parsePhone(full?: string): { countryId: string; number: string } {
  if (!full) return { countryId: "CN", number: "" };
  const trimmed = full.trim();
  const match = trimmed.match(/^(\+\d{1,4})\s*(.*)$/);
  if (match) {
    const code = match[1];
    // 找到匹配的国家（优先精确匹配）
    const found = countryCodes.find((c) => c.code === code);
    return { countryId: found?.id || "CN", number: match[2] };
  }
  return { countryId: "CN", number: trimmed };
}

function composePhone(countryId: string, number: string): string {
  const n = number.trim();
  if (!n) return "";
  const entry = countryCodes.find((c) => c.id === countryId);
  return `${entry?.code || "+86"} ${n}`;
}

function fmtTime(v?: string | null) {
  if (!v) return "--";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "--" : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function str(v: unknown, fb = "--") { return typeof v === "string" && v.trim() ? v : fb; }

function fmtDate(v?: string | null) {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString().slice(0, 10);
}

/* ------------------------------------------------------------------ */
/*  联系方式行                                                          */
/* ------------------------------------------------------------------ */

function ContactRow({
  contact,
  index,
  onChange,
  onRemove,
}: {
  contact: AdminContactInfo;
  index: number;
  onChange: (i: number, c: AdminContactInfo) => void;
  onRemove: (i: number) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Select value={contact.platform} onValueChange={(v) => onChange(index, { ...contact, platform: v })}>
        <SelectTrigger className="w-28 h-8 text-xs"><SelectValue placeholder="平台" /></SelectTrigger>
        <SelectContent>
          {contactPlatforms.map((p) => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <Input
        className="flex-1 h-8 text-xs"
        placeholder="账号 / 号码"
        value={contact.value}
        onChange={(e) => onChange(index, { ...contact, value: e.target.value })}
      />
      <Input
        className="w-24 h-8 text-xs"
        placeholder="备注"
        value={contact.label || ""}
        onChange={(e) => onChange(index, { ...contact, label: e.target.value })}
      />
      <Button variant="ghost" size="icon" className="size-8 shrink-0 text-muted-foreground hover:text-red-500" onClick={() => onRemove(index)}>
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  主页面                                                              */
/* ------------------------------------------------------------------ */

export default function ProfilePage() {
  const profileQuery = useAdminProfileQuery();
  const sessionQuery = useAdminSessionQuery();
  const permTreeQuery = useAdminRolePermissionTreeQuery();
  const updateMutation = useUpdateAdminProfileMutation();
  const uploadMutation = useUploadAdminAvatarMutation();
  const operator = useAuthStore((s) => s.operator);
  const fileRef = useRef<HTMLInputElement>(null);

  const profile = profileQuery.data;
  const account = profile?.account;
  const session = sessionQuery.data;
  const assignments = profile?.assignments || [];

  // 表单状态
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [countryId, setCountryId] = useState("CN");
  const [phoneNumber, setPhoneNumber] = useState("");
  const [birthday, setBirthday] = useState("");
  const [bio, setBio] = useState("");
  const [contacts, setContacts] = useState<AdminContactInfo[]>([]);

  useEffect(() => {
    if (account) {
      setDisplayName(account.displayName || "");
      setEmail(account.email || "");
      const parsed = parsePhone(account.phone);
      setCountryId(parsed.countryId);
      setPhoneNumber(parsed.number);
      setBirthday(fmtDate(account.birthday));
      setBio(account.bio || "");
      setContacts(account.contacts || []);
    }
  }, [account]);

  const updateContact = useCallback((i: number, c: AdminContactInfo) => {
    setContacts((s) => s.map((old, idx) => idx === i ? c : old));
  }, []);

  const removeContact = useCallback((i: number) => {
    setContacts((s) => s.filter((_, idx) => idx !== i));
  }, []);

  const addContact = useCallback(() => {
    setContacts((s) => [...s, { platform: "wechat", value: "", label: "" }]);
  }, []);

  async function handleSave() {
    try {
      await updateMutation.mutateAsync({
        displayName,
        email,
        phone: composePhone(countryId, phoneNumber),
        birthday: birthday || undefined,
        bio,
        contacts: contacts.filter((c) => c.value.trim()),
      });
      toast.success("资料已更新");
    } catch (err) { toast.error(err instanceof ApiError ? err.message : "更新失败"); }
  }

  async function handleAvatarChange(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      await uploadMutation.mutateAsync({ file });
      toast.success("头像已更新");
    } catch (err) { toast.error(err instanceof ApiError ? err.message : "上传失败"); }
    e.target.value = "";
  }

  const myRoleKeys = assignments.map((a) => a.roleKey).filter(Boolean) as string[];
  const myPermGroups = permTreeQuery.data
    ?.filter((r) => account?.isSuperAdmin || myRoleKeys.includes(r.key))
    .flatMap((r) => r.permissionGroups) || [];
  const uniqueGroups = myPermGroups.reduce((acc, g) => {
    const existing = acc.find((x) => x.key === g.key);
    if (!existing) {
      acc.push({ ...g, permissions: [...g.permissions] });
    } else {
      for (const p of g.permissions) {
        const ep = existing.permissions.find((x) => x.code === p.code);
        if (!ep) existing.permissions.push(p);
        else if (p.description === "granted" && ep.description !== "granted") ep.description = "granted";
      }
    }
    return acc;
  }, [] as typeof myPermGroups);

  if (profileQuery.isLoading) return <LoadingState title="加载资料" />;
  if (!account) return <LoadingState title="加载资料" />;

  const avatarSrc = avatarUrl(account.avatar || operator?.avatar, operator?.avatarVersion);
  const initials = (account.displayName || account.account || "A").slice(0, 2).toUpperCase();

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="个人资料" />

      <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
        {/* ── 左侧主内容 ── */}
        <div className="space-y-6">
          {/* 头像 + 基本信息 */}
          <Card>
            <CardContent className="p-5">
              <div className="flex items-start gap-5">
                <div className="relative">
                  <Avatar className="size-20 rounded-2xl">
                    <AvatarImage src={avatarSrc} />
                    <AvatarFallback className="rounded-2xl text-lg">{initials}</AvatarFallback>
                  </Avatar>
                  <button
                    type="button"
                    className="absolute -bottom-1 -right-1 flex size-7 items-center justify-center rounded-full border-2 border-background bg-foreground text-background transition-transform hover:scale-110"
                    onClick={() => fileRef.current?.click()}
                    disabled={uploadMutation.isPending}
                  >
                    {uploadMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Camera className="size-3" />}
                  </button>
                  <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/gif,image/webp" className="hidden" aria-label="上传头像" onChange={handleAvatarChange} />
                </div>
                <div className="min-w-0 flex-1">
                  <h2 className="text-lg font-semibold">{account.displayName || account.account}</h2>
                  <p className="mt-0.5 text-sm text-muted-foreground">@{account.account}</p>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {account.isSuperAdmin && <Badge variant="default" className="text-[10px]">超级管理员</Badge>}
                    <Badge variant={account.status === "active" ? "success" : "danger"} className="text-[10px]">{account.status === "active" ? "正常" : "停用"}</Badge>
                    {assignments.length > 0 && !account.isSuperAdmin && (
                      <Badge variant="outline" className="text-[10px]">{assignments.length} 个角色</Badge>
                    )}
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* 编辑表单（Accordion 分区） */}
          <Card>
            <CardContent className="p-5">
              <Accordion type="multiple" defaultValue={["basic", "contacts"]} className="space-y-2">
                {/* 基本信息 */}
                <AccordionItem value="basic" className="rounded-lg border overflow-hidden border-b-0">
                  <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">
                    <div className="flex items-center gap-2">
                      <User className="size-3.5 text-muted-foreground" />
                      基本信息
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="px-4 pb-4 space-y-4">
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1">
                        <Label className="text-xs">显示名称</Label>
                        <Input className="h-8 text-sm" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
                      </div>
                      <div className="space-y-1">
                        <Label className="text-xs">邮箱</Label>
                        <Input className="h-8 text-sm" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
                      </div>
                      <div className="space-y-1">
                        <Label className="text-xs flex items-center gap-1"><Phone className="size-3" />手机号</Label>
                        <div className="flex gap-1.5">
                          <Select value={countryId} onValueChange={setCountryId}>
                            <SelectTrigger className="w-36 h-8 text-xs shrink-0"><SelectValue /></SelectTrigger>
                            <SelectContent className="max-h-72">
                              {countryCodes.map((c) => (
                                <SelectItem key={c.id} value={c.id}>
                                  <CountryFlag code={c.iso} size={14} />
                                  <span className="ml-1 font-mono">{c.code}</span>
                                  <span className="ml-1 text-muted-foreground">{c.label}</span>
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <Input className="flex-1 h-8 text-sm" value={phoneNumber} onChange={(e) => setPhoneNumber(e.target.value.replace(/[^\d\s-]/g, ""))} placeholder="138 0000 0000" />
                        </div>
                      </div>
                      <div className="space-y-1">
                        <Label className="text-xs flex items-center gap-1"><Calendar className="size-3" />出生日期</Label>
                        <Input className="h-8 text-sm" type="date" value={birthday} onChange={(e) => setBirthday(e.target.value)} />
                      </div>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">个人简介</Label>
                      <Textarea rows={3} className="text-sm" placeholder="一句话介绍自己..." value={bio} onChange={(e) => setBio(e.target.value)} />
                    </div>
                  </AccordionContent>
                </AccordionItem>

                {/* 联系方式 */}
                <AccordionItem value="contacts" className="rounded-lg border overflow-hidden border-b-0">
                  <AccordionTrigger className="px-4 py-2.5 text-sm font-medium hover:no-underline">
                    <div className="flex items-center gap-2">
                      <MessageCircle className="size-3.5 text-muted-foreground" />
                      联系方式
                      {contacts.length > 0 && <Badge variant="secondary" className="text-[10px]">{contacts.length}</Badge>}
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="px-4 pb-4 space-y-3">
                    {contacts.length === 0 && (
                      <p className="text-xs text-muted-foreground py-2">暂未添加联系方式。</p>
                    )}
                    {contacts.map((c, i) => (
                      <ContactRow key={i} contact={c} index={i} onChange={updateContact} onRemove={removeContact} />
                    ))}
                    <Button variant="outline" size="sm" onClick={addContact} className="w-full">
                      <CirclePlus className="size-3.5 mr-1.5" />添加联系方式
                    </Button>
                  </AccordionContent>
                </AccordionItem>
              </Accordion>

              <div className="mt-4">
                <Button size="sm" className="h-8 gap-1 text-xs" disabled={updateMutation.isPending} onClick={handleSave}>
                  <Save className="size-3" />{updateMutation.isPending ? "保存中..." : "保存"}
                </Button>
              </div>
            </CardContent>
          </Card>

          {/* 角色与权限 */}
          <Card>
            <CardContent className="space-y-4 p-5">
              <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">角色与权限</h3>

              {account.isSuperAdmin ? (
                <div className="flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 dark:border-emerald-900 dark:bg-emerald-950">
                  <Shield className="size-4 text-emerald-600 dark:text-emerald-400" />
                  <span className="text-xs text-emerald-700 dark:text-emerald-300">超级管理员 -- 拥有所有权限</span>
                </div>
              ) : assignments.length === 0 ? (
                <p className="text-xs text-muted-foreground">暂无角色分配</p>
              ) : (
                <div className="space-y-1.5">
                  {assignments.map((a, i) => (
                    <div key={i} className="flex items-center justify-between rounded-lg border px-3 py-2">
                      <div className="flex items-center gap-2">
                        <KeyRound className="size-3.5 text-muted-foreground" />
                        <span className="text-xs font-medium">{a.roleKey}</span>
                      </div>
                      <span className="text-[10px] text-muted-foreground">
                        {a.appid != null ? `${a.appName || `应用 ${a.appid}`}` : "全局"}
                      </span>
                    </div>
                  ))}
                </div>
              )}

              {uniqueGroups.length > 0 && (
                <>
                  <Separator />
                  <h4 className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">权限详情</h4>
                  <div className="rounded-lg border p-3">
                    <PermissionTree groups={uniqueGroups} />
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </div>

        {/* ── 右侧信息栏 ── */}
        <div className="space-y-4">
          {/* 账户信息 */}
          <Card>
            <CardContent className="space-y-3 p-4">
              <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">账户信息</h3>
              <InfoRow label="ID" value={String(account.id)} mono />
              <InfoRow label="账号" value={account.account} mono />
              <InfoRow label="邮箱" value={str(account.email)} />
              <InfoRow label="手机号" value={str(account.phone)} />
              <InfoRow label="出生日期" value={account.birthday ? fmtDate(account.birthday) : "--"} />
              <InfoRow label="创建时间" value={fmtTime(account.createdAt)} />
              <InfoRow label="更新时间" value={fmtTime(account.updatedAt)} />
              <InfoRow label="最近登录" value={fmtTime(account.lastLoginAt)} />
            </CardContent>
          </Card>

          {/* 联系方式摘要 */}
          {(account.contacts?.length ?? 0) > 0 && (
            <Card>
              <CardContent className="space-y-3 p-4">
                <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">联系方式</h3>
                {account.contacts!.map((c, i) => {
                  const p = contactPlatforms.find((x) => x.value === c.platform);
                  return (
                    <div key={i} className="flex items-center justify-between gap-2">
                      <span className="text-[11px] text-muted-foreground">{p?.label || c.platform}</span>
                      <span className="text-[11px] font-mono truncate max-w-36 text-right">{c.value}</span>
                    </div>
                  );
                })}
              </CardContent>
            </Card>
          )}

          {/* 当前会话 */}
          <Card>
            <CardContent className="space-y-3 p-4">
              <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">当前会话</h3>
              {sessionQuery.isLoading ? (
                <Skeleton className="h-16 w-full rounded" />
              ) : session ? (
                <>
                  <InfoRow label="签发时间" value={fmtTime(session.issuedAt)} />
                  <InfoRow label="过期时间" value={fmtTime(session.expiresAt)} />
                  <InfoRow label="Token ID" value={str(session.tokenId)} mono />
                </>
              ) : (
                <p className="text-xs text-muted-foreground">无会话信息</p>
              )}
            </CardContent>
          </Card>

          {/* 安全状态 */}
          <Card>
            <CardContent className="space-y-3 p-4">
              <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">安全</h3>
              <div className="space-y-1.5 text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">账户状态</span>
                  <Badge variant={account.status === "active" ? "success" : "danger"} className="text-[9px]">{account.status === "active" ? "正常" : "停用"}</Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">认证方式</span>
                  <Badge variant="outline" className="text-[9px]">
                    {account.authSource === "ldap" ? "LDAP / AD 域" : account.authSource === "oidc" ? "OIDC SSO" : "本地密码"}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">权限级别</span>
                  <span className="font-medium">{account.isSuperAdmin ? "超级管理员" : "普通管理员"}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">授权范围</span>
                  <span className="font-medium">{assignments.length} 个</span>
                </div>
              </div>
              <Button size="sm" variant="outline" className="h-7 w-full gap-1 text-xs" asChild>
                <Link href="/security" prefetch>前往安全设置</Link>
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="shrink-0 text-[11px] text-muted-foreground">{label}</span>
      <span className={cn("text-right text-[11px]", mono && "font-mono")}>{value}</span>
    </div>
  );
}
