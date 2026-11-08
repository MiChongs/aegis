"use client";

import { useParams } from "next/navigation";

import { SceneStudio } from "@/components/risk/studio/scene-studio";

/** 场景工作台：管线、规则、决策流、策略、模拟、发布（docs/risk-platform.md） */
export default function RiskScenePage() {
  const params = useParams<{ key: string }>();
  return <SceneStudio sceneKey={decodeURIComponent(params.key)} />;
}
