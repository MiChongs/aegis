"use client";

import { useCallback, useMemo, useState } from "react";
import type { BanFilter } from "@/lib/api/firewall-bans";
import type { IPBanEntry } from "@/lib/api/types";
import { useDebouncedValue } from "@/lib/use-debounced-value";

/**
 * IP 封禁列表的状态：筛选、分页、选择。提在页签层，是因为概况里的分布项、
 * IP 查询里的操作都要改它。三条规则：
 * - 筛选任何一项变化都回到第 1 页并清空选择（选中的是「旧条件下的那批」，留着只会误操作）；
 * - 勾选模式（ids）跨页保留，按筛选全选模式（filter）只代表「当前条件下的全部」；
 * - 关键字 300ms 防抖后才进查询，选择在敲键时就清掉。
 */

export type BanStatusFilter = "active" | "expired" | "revoked" | "all";
export type BanSort = NonNullable<BanFilter["sort"]>;

export type BanListFilter = {
  status: BanStatusFilter;
  source: string;
  /** "" 全部，"default" 沿用全局默认，其余为模式值 */
  mode: string;
  country: string;
  keyword: string;
  expiring: boolean;
  permanent: "" | "true" | "false";
  sort: BanSort;
};

export const DEFAULT_BAN_FILTER: BanListFilter = {
  status: "active",
  source: "",
  mode: "",
  country: "",
  keyword: "",
  expiring: false,
  permanent: "",
  sort: "created_desc"
};

export type BanSelection = { kind: "ids"; rows: Map<number, IPBanEntry> } | { kind: "filter" };

const EMPTY_SELECTION: BanSelection = { kind: "ids", rows: new Map() };

export const PAGE_SIZES = [50, 100, 200] as const;

export function useBanListState() {
  const [filter, setFilter] = useState<BanListFilter>(DEFAULT_BAN_FILTER);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<number>(50);
  const [selection, setSelection] = useState<BanSelection>(EMPTY_SELECTION);

  const update = useCallback((patch: Partial<BanListFilter>) => {
    setFilter((prev) => ({ ...prev, ...patch }));
    setPage(1);
    setSelection(EMPTY_SELECTION);
  }, []);

  /** 清空附加条件；状态与排序保留（它们不以胶囊显示） */
  const reset = useCallback(() => update({ source: "", mode: "", country: "", keyword: "", expiring: false, permanent: "" }), [update]);
  const clearSelection = useCallback(() => setSelection(EMPTY_SELECTION), []);
  const dropFromSelection = useCallback(
    (id: number) =>
      setSelection((prev) => {
        if (prev.kind === "filter") return EMPTY_SELECTION;
        if (!prev.rows.has(id)) return prev;
        const rows = new Map(prev.rows);
        rows.delete(id);
        return { kind: "ids", rows };
      }),
    []
  );

  const keyword = useDebouncedValue(filter.keyword.trim(), 300);

  /** 不带分页的查询条件（计数、按筛选解封、导出共用） */
  const query = useMemo<BanFilter>(
    () => ({
      status: filter.status,
      source: filter.source || undefined,
      mode: (filter.mode || undefined) as BanFilter["mode"],
      country: filter.country || undefined,
      keyword: keyword || undefined,
      expiringWithin: filter.expiring ? 86_400 : undefined,
      permanent: filter.permanent || undefined,
      sort: filter.sort
    }),
    [filter.status, filter.source, filter.mode, filter.country, filter.expiring, filter.permanent, filter.sort, keyword]
  );

  const pageQuery = useMemo<BanFilter>(() => ({ ...query, page, pageSize }), [query, page, pageSize]);

  const changePageSize = useCallback((size: number) => {
    setPageSize(size);
    setPage(1);
  }, []);

  return {
    filter,
    update,
    reset,
    page,
    setPage,
    pageSize,
    setPageSize: changePageSize,
    selection,
    setSelection,
    clearSelection,
    dropFromSelection,
    query,
    pageQuery
  };
}

export type BanListState = ReturnType<typeof useBanListState>;

/** 和默认值相比改了几项（不算状态与排序） */
export function activeFilterCount(filter: BanListFilter) {
  return [filter.source, filter.mode, filter.country, filter.keyword.trim(), filter.expiring, filter.permanent].filter(Boolean).length;
}
