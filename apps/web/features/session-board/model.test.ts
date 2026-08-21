import { describe, expect, it } from "vitest";
import {
  DEFAULT_AGENTS,
  DEFAULT_BOARD_STATE,
  buildProjectBoard,
  codexThreadUrl,
  mergeDetectedProjects,
  normalizeBoardState,
  type CodexThread,
} from "./model";

const threads: CodexThread[] = [
  { id: "t1", name: "方法 A", cwd: "/projects/manuscript", recency_at: 30 },
  { id: "t2", name: "方法 B", cwd: "/projects/manuscript", recency_at: 20 },
  { id: "t3", name: "写作", cwd: "/projects/manuscript", recency_at: 10 },
  { id: "t4", name: "其他项目", cwd: "/projects/other", recency_at: 5 },
];

describe("session board model", () => {
  it("uses five default agent rows for detected projects", () => {
    expect(DEFAULT_BOARD_STATE.projects).toEqual([]);
    expect(DEFAULT_AGENTS.map((agent) => agent.name)).toEqual([
      "Leader Agent",
      "Methods Agent",
      "Analysis Agent",
      "Writing Agent",
      "Reviewer Agent",
    ]);
  });

  it("groups cards by agent and sorts by last activity", () => {
    const state = mergeDetectedProjects(threads, normalizeBoardState({
      version: 1,
      projects: [],
      assignments: {
        t1: { projectId: "/projects/manuscript", agentId: "methods" },
        t2: { projectId: "/projects/manuscript", agentId: "methods" },
        t3: { projectId: "/projects/manuscript", agentId: "writing" },
      },
    }));
    const board = buildProjectBoard(threads, state, "/projects/manuscript");

    expect(board.rows.find((row) => row.id === "methods")?.threads.map((thread) => thread.id)).toEqual(["t1", "t2"]);
    expect(board.rows.find((row) => row.id === "writing")?.threads.map((thread) => thread.id)).toEqual(["t3"]);
    expect(board.rows.flatMap((row) => row.threads).map((thread) => thread.id)).not.toContain("t4");
  });

  it("puts project sessions without role metadata in the first agent row", () => {
    const state = mergeDetectedProjects(threads, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(threads, state, "/projects/manuscript");
    expect(board.rows[0]?.threads.map((thread) => thread.id)).toEqual(["t1", "t2", "t3"]);
  });

  it("detects projects from conversation working directories", () => {
    const state = mergeDetectedProjects(threads, DEFAULT_BOARD_STATE);
    expect(state.projects.map((project) => [project.id, project.name])).toEqual([
      ["/projects/manuscript", "manuscript"],
      ["/projects/other", "other"],
    ]);
  });

  it("builds the native Codex deep link", () => {
    expect(codexThreadUrl("thread a/b")).toBe("codex://threads/thread%20a%2Fb");
  });
});
