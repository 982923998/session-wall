import type { Metadata } from "next";
import { SessionCardWall } from "@/features/session-board/session-card-wall";

export const metadata: Metadata = {
  title: "Codex 会话卡片墙",
  description: "按项目和 Agent 行定位并继续 Codex 原生会话。",
};

export default function SessionBoardPage() {
  return <SessionCardWall />;
}
