"use client";

import { useMemo } from "react";
import { appSectionGroups, type AppSectionGroup } from "@/lib/app-sections";
import { pinyinMatchIndices, usePinyinReady } from "@/lib/pinyin-search";

/**
 * 区块筛选：宽屏侧栏与窄屏选择器共用同一套命中规则。
 *
 * 三十多个区块里找「卡密」，输中文、输 `km`、输 `kami` 都该命中；说明文字也参与匹配，
 * 「回调验签」这种只在说明里出现的词能把人带到激励广告。分组名命中时整组保留。
 */
export function useSectionFilter(query: string): AppSectionGroup[] {
  const pinyinReady = usePinyinReady();
  return useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return appSectionGroups;
    const hit = (text: string) => text.toLowerCase().includes(q) || pinyinMatchIndices(text, q) !== null;
    return appSectionGroups
      .map((group) =>
        hit(group.title)
          ? group
          : { ...group, sections: group.sections.filter((item) => hit(item.title) || hit(item.summary) || item.key.includes(q)) }
      )
      .filter((group) => group.sections.length > 0);
    // pinyinReady 变化时重算：词典晚到时让已输入的首字母立即生效
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, pinyinReady]);
}
