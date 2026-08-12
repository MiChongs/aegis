"use client";

import { useEffect, useState } from "react";

/**
 * 防抖取值：输入停下来 `delay` 毫秒后才把值放行。
 *
 * 用它替代「输入框 + 查询按钮」那种形状：让人先打字、再去点一个按钮，
 * 是把本该由机器承担的节流工作转嫁给了操作者。
 *
 * setState 在 `setTimeout` 回调里调用（异步），不违反
 * `react-hooks/set-state-in-effect` —— 那条规则针对的是 effect **同步** setState。
 */
export function useDebouncedValue<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);

  return debounced;
}
