import { describe, expect, it } from "vitest";
import {
  DEFAULT_BOARD_STATE,
  buildProjectBoard,
  codexThreadUrl,
  normalizeBoardState,
  type CodexThread,
} from "./model";

const threads: CodexThread[] = [
  { id: "t1", name: "方法 A", recency_at: 30 },
  { id: "t2", name: "方法 B", recency_at: 20 },
  { id: "t3", name: "写作", recency_at: 10 },
  { id: "t4", name: "候选会话", recency_at: 40 },
];

describe("session board model", () => {
  it("starts with the requested project and five agent rows", () => {
    expect(DEFAULT_BOARD_STATE.projects[0]?.name).toBe("Multiecho-T2");
    expect(DEFAULT_BOARD_STATE.projects[0]?.agents.map((agent) => agent.name)).toEqual([
      "Leader Agent",
      "Methods Agent",
      "Analysis Agent",
      "Writing Agent",
      "Reviewer Agent",
    ]);
  });

  it("groups cards by agent and sorts by last activity", () => {
    const state = normalizeBoardState({
      ...DEFAULT_BOARD_STATE,
      assignments: {
        t1: { projectId: "multiecho-t2", agentId: "methods" },
        t2: { projectId: "multiecho-t2", agentId: "methods" },
        t3: { projectId: "multiecho-t2", agentId: "writing" },
      },
    });
    const board = buildProjectBoard(threads, state, "multiecho-t2");

    expect(board.rows.find((row) => row.id === "methods")?.threads.map((thread) => thread.id)).toEqual(["t1", "t2"]);
    expect(board.rows.find((row) => row.id === "writing")?.threads.map((thread) => thread.id)).toEqual(["t3"]);
    expect(board.availableThreads.map((thread) => thread.id)).toEqual(["t4"]);
  });

  it("does not offer cards assigned to another project", () => {
    const state = normalizeBoardState({
      ...DEFAULT_BOARD_STATE,
      assignments: { t4: { projectId: "other", agentId: "agent" } },
    });
    expect(buildProjectBoard(threads, state, "multiecho-t2").availableThreads.map((thread) => thread.id)).toEqual(["t1", "t2", "t3"]);
  });

  it("builds the native Codex deep link", () => {
    expect(codexThreadUrl("thread a/b")).toBe("codex://threads/thread%20a%2Fb");
  });
});
