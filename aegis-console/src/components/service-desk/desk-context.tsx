"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { TicketKind } from "@/lib/api/tickets";

// 服务台的「是哪一种台」：工单中心与意见反馈共用同一套组件，
// 名词、对方称呼、深链地址由这里决定，组件里不再写死「反馈」或「工单」。

export type DeskConfig = {
  kind: TicketKind;
  /** 单条叫什么：工单 / 反馈 */
  noun: string;
  /** 对方叫什么：提单人 / 反馈人 */
  person: string;
  /** 深链落点，站内信与通知的 ?id= 指向这里 */
  basePath: string;
  title: string;
};

export const DESK_CONFIG: Record<TicketKind, DeskConfig> = {
  ticket: { kind: "ticket", noun: "工单", person: "提单人", basePath: "/tickets", title: "工单中心" },
  feedback: { kind: "feedback", noun: "反馈", person: "反馈人", basePath: "/feedback", title: "意见反馈" }
};

const DeskContext = createContext<DeskConfig>(DESK_CONFIG.feedback);

export function DeskProvider({ kind, children }: { kind: TicketKind; children: ReactNode }) {
  return <DeskContext.Provider value={DESK_CONFIG[kind]}>{children}</DeskContext.Provider>;
}

export function useDesk() {
  return useContext(DeskContext);
}

/** 某条工单 / 反馈的站内深链 */
export function deskLink(config: DeskConfig, id: number) {
  return `${config.basePath}?id=${id}`;
}
