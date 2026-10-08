/**
 * 设备字典在用户接口里的完整映射（后端 `devicedomain.Info`）。
 *
 * 会话列表、登录记录、扫码登录的发起端都带这个对象。服务端只保存客户端上报的原始型号
 * 与补充信息，展示字段在读取时按设备字典现查，修正字典后立刻生效。
 * 契约见 docs/app-integration.md「设备信息」。
 */
export type DeviceInfo = {
  /** 展示名：命中字典为「厂商 营销名」，否则为客户端上报值或 UA 推断 */
  name: string;
  /** 客户端上报的原始型号（SM-G998B / iPhone14,3） */
  identifier?: string;
  platform?: string;
  marketingName?: string;
  manufacturer?: string;
  manufacturerIconUrl?: string;
  deviceImageUrl?: string;
  brand?: string;
  codename?: string;
  os?: string;
  osVersion?: string;
  appVersion?: string;
  dictionaryId?: number;
  matched: boolean;
  source: "dictionary" | "client" | "user_agent" | "unknown" | string;
};
