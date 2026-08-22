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
  { id: "t1", name: "method-primary analysis", cwd: "/projects/manuscript", recency_at: 30 },
  { id: "t2", name: "Methods-robustness check", cwd: "/projects/manuscript", recency_at: 20 },
  { id: "t3", name: "writing-discussion revision", cwd: "/projects/manuscript", recency_at: 10 },
  { id: "t5", name: "general project question", cwd: "/projects/manuscript", recency_at: 8 },
  { id: "t6", name: "unknown-follow-up", cwd: "/projects/manuscript", recency_at: 7 },
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

  it("groups cards by the title prefix and accepts singular role aliases", () => {
    const state = mergeDetectedProjects(threads, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(threads, state, "/projects/manuscript");

    expect(board.rows.find((row) => row.id === "methods")?.threads.map((thread) => thread.id)).toEqual(["t1", "t2"]);
    expect(board.rows.find((row) => row.id === "writing")?.threads.map((thread) => thread.id)).toEqual(["t3"]);
    expect(board.rows.flatMap((row) => row.threads).map((thread) => thread.id)).not.toContain("t4");
  });

  it("puts sessions without a recognized title role in the unassigned list", () => {
    const state = mergeDetectedProjects(threads, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(threads, state, "/projects/manuscript");
    expect(board.rows.find((row) => row.id === "leader")?.threads).toEqual([]);
    expect(board.unassignedThreads.map((thread) => thread.id)).toEqual(["t5", "t6"]);
  });

  it("uses the title prefix instead of legacy manual assignments", () => {
    const state = mergeDetectedProjects(threads, normalizeBoardState({
      version: 1,
      projects: [],
      assignments: { t3: { projectId: "/projects/manuscript", agentId: "methods" } },
    }));
    const board = buildProjectBoard(threads, state, "/projects/manuscript");
    expect(board.rows.find((row) => row.id === "writing")?.threads.map((thread) => thread.id)).toEqual(["t3"]);
  });

  it("omits locally deleted cards without deleting the Codex thread", () => {
    const state = mergeDetectedProjects(threads, {
      version: 1,
      projects: [],
      assignments: {},
      hiddenThreads: { t2: "/projects/manuscript", t5: "/projects/manuscript" },
    });
    const board = buildProjectBoard(threads, state, "/projects/manuscript");
    expect(board.rows.flatMap((row) => row.threads).map((thread) => thread.id)).toEqual(["t1", "t3"]);
    expect(board.unassignedThreads.map((thread) => thread.id)).toEqual(["t6"]);
  });

  it("normalizes persisted folding maps for old and new board state", () => {
    const oldState = normalizeBoardState({ version: 1, projects: [], assignments: {}, hiddenThreads: {} });
    expect(oldState.collapsedThreads).toEqual({});
    expect(oldState.collapsedRows).toEqual({});

    const foldedState = normalizeBoardState({
      version: 1,
      projects: [],
      assignments: {},
      hiddenThreads: {},
      collapsedThreads: { t1: "/projects/manuscript" },
      collapsedRows: { "/projects/manuscript::methods": true },
    });
    expect(foldedState.collapsedThreads).toEqual({ t1: "/projects/manuscript" });
    expect(foldedState.collapsedRows).toEqual({ "/projects/manuscript::methods": true });
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
