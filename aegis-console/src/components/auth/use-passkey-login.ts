"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  beginAdminPasskeyLogin,
  loginAdminWithPasskey,
  type AdminLoginResponse,
  type AdminPasskeyLoginBegin
} from "@/lib/api/auth";
import { useIsClient } from "@/lib/use-client-value";
import {
  describePasskeyError,
  getPasskeyAssertion,
  passkeyAutofillAvailable,
  passkeyRegistrationSupported,
  passkeySecureContextIssue
} from "@/lib/webauthn";

/** 挑战离过期还剩多久就提前换新：留出用户从选中到提交的时间 */
const REFRESH_MARGIN_MS = 20_000;
/** 服务端没给有效期时按多久换一次 */
const FALLBACK_TTL_MS = 4 * 60_000;

/** 用户自己取消、或我们主动中断：不算错误，不提示 */
function isDismissal(error: unknown) {
  const cause = error instanceof Error && error.cause ? error.cause : error;
  return cause instanceof DOMException && (cause.name === "NotAllowedError" || cause.name === "AbortError");
}

function freshFor(begin: AdminPasskeyLoginBegin | null) {
  if (!begin) return 0;
  const expires = Date.parse(begin.session.expiresAt);
  return Number.isFinite(expires) ? expires - Date.now() - REFRESH_MARGIN_MS : FALLBACK_TTL_MS;
}

/**
 * 登录页的通行密钥登录，两条入口共用一套状态：
 *
 * 1. **自动填充**（条件式 UI）：登录表单可见时挂一个 `mediation: "conditional"` 的请求，
 *    点账号框时浏览器在建议列表里列出本站的通行密钥，选中即登录。挑战快过期时自动换新；
 *    服务端没开通行密钥模块（拿挑战报错）时静默不挂，界面上不出现任何东西。
 * 2. **按钮**：弹出系统的通行密钥选择框。按下时先中断挂着的条件式请求（同一时刻只能有一个），
 *    挑战提前取好，按下后直接调起 —— Safari 要求 WebAuthn 紧跟在点击之后，中间不能先等一次网络。
 *
 * 两条入口拿到断言后都交给 `/passkey/login`，成功走 `onSuccess`（与密码登录成功同一出口）。
 */
export function usePasskeyLogin({
  active,
  onSuccess,
  onError
}: {
  /** 登录表单是否在显示（两步验证、成功态时不挂请求） */
  active: boolean;
  onSuccess: (result: AdminLoginResponse) => void;
  onError: (message: string | null) => void;
}) {
  const isClient = useIsClient();
  const supported = isClient && passkeyRegistrationSupported() && !passkeySecureContextIssue();

  const [pending, setPending] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  /** 弹窗用掉预取的挑战后 +1，触发重新预取 */
  const [prefetchRound, setPrefetchRound] = useState(0);
  /** 条件式请求的中断器：按钮弹窗前要先把它停掉 */
  const conditionalRef = useRef<AbortController | null>(null);
  /** 给按钮预取的挑战 */
  const prefetchedRef = useRef<AdminPasskeyLoginBegin | null>(null);
  const callbacks = useRef({ onSuccess, onError });
  useEffect(() => {
    callbacks.current = { onSuccess, onError };
  });

  const submit = useCallback(async (challengeId: string, credential: Record<string, unknown>) => {
    setPending(true);
    callbacks.current.onError(null);
    try {
      callbacks.current.onSuccess(await loginAdminWithPasskey({ challengeId, credential }));
      return true;
    } catch (error) {
      callbacks.current.onError(error instanceof Error ? error.message : "通行密钥登录失败");
      return false;
    } finally {
      setPending(false);
    }
  }, []);

  // 自动填充：表单可见、且没有弹窗在进行时挂着
  useEffect(() => {
    if (!active || !supported || modalOpen) return;
    let cancelled = false;
    let timer: number | undefined;

    const arm = async () => {
      if (cancelled || !(await passkeyAutofillAvailable()) || cancelled) return;
      let begin: AdminPasskeyLoginBegin;
      try {
        begin = await beginAdminPasskeyLogin();
      } catch {
        return; // 模块未开启或被风控拦下：不挂自动填充
      }
      if (cancelled) return;
      const controller = new AbortController();
      conditionalRef.current = controller;
      timer = window.setTimeout(() => {
        controller.abort();
        void arm();
      }, Math.max(freshFor(begin), 30_000));

      let credential: Record<string, unknown>;
      try {
        credential = await getPasskeyAssertion(begin.options, { mediation: "conditional", signal: controller.signal });
      } catch {
        return; // 被中断（换新挑战、弹窗、离开页面）或浏览器拒绝：都不提示
      } finally {
        window.clearTimeout(timer);
        if (conditionalRef.current === controller) conditionalRef.current = null;
      }
      if (cancelled) return;
      const ok = await submit(begin.session.challengeId, credential);
      if (!ok && !cancelled) void arm();
    };

    void arm();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      conditionalRef.current?.abort();
      conditionalRef.current = null;
    };
  }, [active, supported, modalOpen, submit]);

  // 给按钮预取挑战，快过期时换新
  useEffect(() => {
    if (!active || !supported) return;
    let cancelled = false;
    let timer: number | undefined;
    const prefetch = async () => {
      try {
        const begin = await beginAdminPasskeyLogin();
        if (cancelled) return;
        prefetchedRef.current = begin;
        timer = window.setTimeout(() => void prefetch(), Math.max(freshFor(begin), 30_000));
      } catch {
        prefetchedRef.current = null;
      }
    };
    void prefetch();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [active, supported, prefetchRound]);

  const start = useCallback(async () => {
    if (pending || modalOpen) return;
    conditionalRef.current?.abort();
    conditionalRef.current = null;
    setModalOpen(true);
    callbacks.current.onError(null);
    try {
      let begin = prefetchedRef.current;
      prefetchedRef.current = null;
      if (freshFor(begin) <= 0) begin = await beginAdminPasskeyLogin();
      const credential = await getPasskeyAssertion(begin!.options);
      await submit(begin!.session.challengeId, credential);
    } catch (error) {
      if (!isDismissal(error)) {
        callbacks.current.onError(error instanceof DOMException ? describePasskeyError(error) : error instanceof Error ? error.message : "通行密钥登录失败");
      }
    } finally {
      setModalOpen(false);
      setPrefetchRound((round) => round + 1);
    }
  }, [pending, modalOpen, submit]);

  return { supported, pending: pending || modalOpen, start };
}
