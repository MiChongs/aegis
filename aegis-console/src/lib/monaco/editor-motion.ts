"use client";

import { useReducedMotion } from "motion/react";
import type { editor } from "monaco-editor";

/**
 * 编辑器的动效档位。
 *
 * Monaco 里这几样默认全是关的（VS Code 也一样），因为它要照顾极老的机器。
 * 但打开之后差别很大：光标从"每 500ms 硬闪一次"变成呼吸式渐隐渐现，
 * 跳转时不再瞬移而是滑过去，滚动有惯性 —— 这几件事合起来才是
 * "在 macOS 上写代码"的手感。
 *
 * 两道闸门决定给不给：
 *
 *  1. **系统级** `prefers-reduced-motion`。一个持续呼吸的光标正是这类偏好
 *     要挡的东西（前庭不适、注意力障碍），全站其余动效已经统一守这条，
 *     编辑器没有理由例外。
 *  2. **用户级**偏好开关。低端机上光标平滑移动会掉帧，而掉帧的光标
 *     比不动的光标更难看 —— 这不是无障碍问题，是性能问题，
 *     所以要单独给一个开关而不是让人去改系统设置。
 */
export type EditorMotionOptions = Pick<
  editor.IStandaloneEditorConstructionOptions,
  | "cursorBlinking"
  | "cursorSmoothCaretAnimation"
  | "smoothScrolling"
  | "mouseWheelScrollSensitivity"
  | "fastScrollSensitivity"
  | "cursorSurroundingLines"
  | "cursorSurroundingLinesStyle"
>;

/**
 * `phase` 是几档闪烁里最接近 macOS 的一档。
 *
 * Monaco 给了五种：`blink`（硬开硬关）、`smooth`（淡入淡出到全透明）、
 * `phase`（在两个不透明度之间脉动，**从不完全消失**）、`expand`（缩放）、
 * `solid`（不闪）。macOS 的插入符是脉动而不是消失，所以是 `phase` ——
 * 而"从不完全消失"这一点也让它在长代码里更容易被找回来。
 */
export function editorMotionOptions(animated: boolean): EditorMotionOptions {
  if (!animated) {
    return {
      // 关掉动效时用 solid 而不是 blink：既然是为了减少动，
      // 一个每秒闪两次的方块显然不该留下。
      cursorBlinking: "solid",
      cursorSmoothCaretAnimation: "off",
      smoothScrolling: false,
      mouseWheelScrollSensitivity: 1,
      fastScrollSensitivity: 5,
      cursorSurroundingLines: 0,
      cursorSurroundingLinesStyle: "default"
    };
  }
  return {
    cursorBlinking: "phase",
    // "explicit" 只在明确移动光标时才滑（方向键、点击、跳转），
    // 打字时不滑 —— 打字时滑动会让插入符永远落后于字符，反而黏手。
    cursorSmoothCaretAnimation: "explicit",
    smoothScrolling: true,
    mouseWheelScrollSensitivity: 1,
    fastScrollSensitivity: 5,
    // 光标周围留三行：跳到匹配项或诊断行时不会贴在窗口最底下，
    // 视线不用先找一遍光标在哪
    cursorSurroundingLines: 3,
    cursorSurroundingLinesStyle: "default"
  };
}

/**
 * 结合系统偏好与用户开关，算出最终要不要动。
 *
 * 系统偏好是**否决权**：用户把开关打开也不能盖过 `prefers-reduced-motion`，
 * 那条偏好的语义就是"无论应用怎么想，我都不要"。
 */
export function useEditorMotion(preference: boolean): EditorMotionOptions {
  const reduced = useReducedMotion();
  return editorMotionOptions(preference && !reduced);
}
