"use client";

import countries from "i18n-iso-countries";
import enLocale from "i18n-iso-countries/langs/en.json";
import zhLocale from "i18n-iso-countries/langs/zh.json";

countries.registerLocale(enLocale);
countries.registerLocale(zhLocale);

const provinceToChina = new Set([
  "北京",
  "北京市",
  "天津",
  "天津市",
  "上海",
  "上海市",
  "重庆",
  "重庆市",
  "河北",
  "河北省",
  "山西",
  "山西省",
  "辽宁",
  "辽宁省",
  "吉林",
  "吉林省",
  "黑龙江",
  "黑龙江省",
  "江苏",
  "江苏省",
  "浙江",
  "浙江省",
  "安徽",
  "安徽省",
  "福建",
  "福建省",
  "江西",
  "江西省",
  "山东",
  "山东省",
  "河南",
  "河南省",
  "湖北",
  "湖北省",
  "湖南",
  "湖南省",
  "广东",
  "广东省",
  "海南",
  "海南省",
  "四川",
  "四川省",
  "贵州",
  "贵州省",
  "云南",
  "云南省",
  "陕西",
  "陕西省",
  "甘肃",
  "甘肃省",
  "青海",
  "青海省",
  "台湾",
  "台湾省",
  "内蒙古",
  "内蒙古自治区",
  "广西",
  "广西壮族自治区",
  "西藏",
  "西藏自治区",
  "宁夏",
  "宁夏回族自治区",
  "新疆",
  "新疆维吾尔自治区",
  "香港",
  "香港特别行政区",
  "澳门",
  "澳门特别行政区"
]);

const labelAliases = new Map<string, string>([
  ["中国", "CN"],
  ["中国大陆", "CN"],
  ["中华人民共和国", "CN"],
  ["美国", "US"],
  ["美利坚合众国", "US"],
  ["英国", "GB"],
  ["大不列颠及北爱尔兰联合王国", "GB"],
  ["俄罗斯", "RU"],
  ["俄罗斯联邦", "RU"],
  ["韩国", "KR"],
  ["大韩民国", "KR"],
  ["朝鲜", "KP"],
  ["朝鲜民主主义人民共和国", "KP"],
  ["伊朗", "IR"],
  ["伊朗伊斯兰共和国", "IR"],
  ["越南", "VN"],
  ["越南社会主义共和国", "VN"],
  ["老挝", "LA"],
  ["老挝人民民主共和国", "LA"],
  ["叙利亚", "SY"],
  ["阿拉伯叙利亚共和国", "SY"],
  ["委内瑞拉", "VE"],
  ["委内瑞拉玻利瓦尔共和国", "VE"],
  ["玻利维亚", "BO"],
  ["多民族玻利维亚国", "BO"],
  ["坦桑尼亚", "TZ"],
  ["坦桑尼亚联合共和国", "TZ"],
  ["摩尔多瓦", "MD"],
  ["摩尔多瓦共和国", "MD"],
  ["文莱", "BN"],
  ["文莱达鲁萨兰国", "BN"],
  ["阿联酋", "AE"],
  ["阿拉伯联合酋长国", "AE"],
  ["捷克", "CZ"],
  ["捷克共和国", "CZ"],
  ["科特迪瓦", "CI"],
  ["刚果（金）", "CD"],
  ["刚果民主共和国", "CD"],
  ["刚果（布）", "CG"],
  ["刚果共和国", "CG"],
  ["巴勒斯坦", "PS"],
  ["中国台湾", "TW"],
  ["中国香港", "CN"],
  ["中国澳门", "CN"]
]);

const englishNameAliases = new Map<string, string>([
  ["China", "CN"],
  ["United States", "US"],
  ["Korea", "KR"],
  ["Dem. Rep. Korea", "KP"],
  ["Congo", "CG"],
  ["Aland", "AX"],
  ["United States of America", "US"],
  ["Dem. Rep. Congo", "CD"],
  ["Central African Rep.", "CF"],
  ["Dominican Rep.", "DO"],
  ["Eq. Guinea", "GQ"],
  ["Solomon Is.", "SB"],
  ["Bosnia and Herz.", "BA"],
  ["S. Sudan", "SS"],
  ["W. Sahara", "EH"],
  ["Czechia", "CZ"],
  ["Laos", "LA"],
  ["eSwatini", "SZ"],
  ["Macedonia", "MK"],
  ["Timor-Leste", "TL"],
  ["Palestine", "PS"],
  ["Kosovo", "XK"],
  ["Myanmar", "MM"],
  ["Côte d'Ivoire", "CI"],
  ["Falkland Is.", "FK"],
  ["Fr. S. Antarctic Lands", "TF"]
]);

export type CountryHotspotInput = {
  region: string;
  code?: string;
  count: number;
};

export type CountryHotspotItem = {
  code: string;
  region: string;
  englishName: string;
  count: number;
  share: number;
};

export type CountryHotspotUnknownItem = {
  region: string;
  count: number;
  share: number;
};

export type CountryHotspotResult = {
  total: number;
  items: CountryHotspotItem[];
  unknownItems: CountryHotspotUnknownItem[];
};

function normalizeLabel(label: string) {
  return label.replace(/\s+/g, " ").trim();
}

function normalizeCountryCode(rawCode?: string) {
  const value = normalizeLabel(rawCode || "").toUpperCase();
  if (!value) {
    return "";
  }
  if (value.length === 2) {
    return value;
  }
  if (value.length === 3) {
    return countries.alpha3ToAlpha2(value) || "";
  }
  return "";
}

export function getCountryCodeFromEnglishName(name: string) {
  const normalized = normalizeLabel(name);
  return englishNameAliases.get(normalized) || countries.getAlpha2Code(normalized, "en") || "";
}

export function resolveCountryCode(region: string, rawCode?: string) {
  const normalizedCode = normalizeCountryCode(rawCode);
  if (normalizedCode) {
    return normalizedCode;
  }

  const normalizedRegion = normalizeLabel(region);
  if (!normalizedRegion) {
    return "";
  }
  if (provinceToChina.has(normalizedRegion)) {
    return "CN";
  }
  if (labelAliases.has(normalizedRegion)) {
    return labelAliases.get(normalizedRegion) || "";
  }
  return (
    countries.getAlpha2Code(normalizedRegion, "zh") ||
    countries.getAlpha2Code(normalizedRegion, "en") ||
    ""
  );
}

function getDisplayName(code: string, fallback: string) {
  return countries.getName(code, "zh") || fallback;
}

function getEnglishName(code: string, fallback: string) {
  return countries.getName(code, "en") || fallback;
}

export function buildCountryHotspots(items: CountryHotspotInput[]): CountryHotspotResult {
  const total = items.reduce((sum, item) => sum + item.count, 0);
  const aggregated = new Map<string, { code: string; region: string; englishName: string; count: number }>();
  const unknown = new Map<string, number>();

  for (const item of items) {
    const region = normalizeLabel(item.region);
    const count = Number.isFinite(item.count) ? item.count : 0;
    if (!region || count <= 0) {
      continue;
    }

    const code = resolveCountryCode(region, item.code);
    if (!code) {
      unknown.set(region, (unknown.get(region) || 0) + count);
      continue;
    }

    const current = aggregated.get(code);
    const nextCount = (current?.count || 0) + count;
    aggregated.set(code, {
      code,
      region: getDisplayName(code, region),
      englishName: getEnglishName(code, region),
      count: nextCount
    });
  }

  const mappedItems = Array.from(aggregated.values())
    .sort((left, right) => right.count - left.count || left.region.localeCompare(right.region, "zh-CN"))
    .map((item) => ({
      ...item,
      share: total > 0 ? Number(((item.count / total) * 100).toFixed(1)) : 0
    }));

  const unknownItems = Array.from(unknown.entries())
    .map(([region, count]) => ({
      region,
      count,
      share: total > 0 ? Number(((count / total) * 100).toFixed(1)) : 0
    }))
    .sort((left, right) => right.count - left.count || left.region.localeCompare(right.region, "zh-CN"));

  return {
    total,
    items: mappedItems,
    unknownItems
  };
}
