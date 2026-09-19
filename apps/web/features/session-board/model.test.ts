import { describe, expect, it } from "vitest";
import {
  DEFAULT_AGENTS,
  DEFAULT_BOARD_STATE,
  FOLLOW_TASK_MODEL,
  messageModelOptions,
  buildProjectBoard,
  codexThreadUrl,
  effectiveThreadStatus,
  hasArchivedThread,
  hasThreadStatusChange,
  mergeDetectedProjects,
  normalizeBoardState,
  reconcileThreadReviews,
  rememberCatalogThreads,
  threadCardStatus,
  threadDisplayStatus,
  transcriptSignature,
  transcriptProgress,
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

it("does not send cached model settings in follow-task mode", () => {
  expect(messageModelOptions(FOLLOW_TASK_MODEL, "high")).toEqual({});
  expect(messageModelOptions("", "high")).toEqual({});
  expect(messageModelOptions("chosen-model", "high")).toEqual({model:"chosen-model",reasoning_effort:"high"});
});

it("refreshes earlier progress and same-length edits even if the last item is unchanged", () => {
  const transcript = {thread_id:"t", status:"active", items:[{id:"a",kind:"tool" as const,text:"old"},{id:"b",kind:"assistant" as const,text:"tail"}]};
  const changed = {...transcript, items:transcript.items.map(item => item.id === "a" ? {...item,text:"new"} : item)};
  expect(transcriptSignature(changed)).not.toBe(transcriptSignature(transcript));
  expect(transcriptProgress(transcript, "")).toContain("等待新的可显示输出");
  expect(transcriptProgress({...transcript,status:"completed"}, "")).toBe("");
  expect(transcriptProgress(transcript, "offline")).toContain("尚未确认");
});

it("uses refreshed pinned scope even when empty and keeps empty pinned projects", () => {
  const cached = rememberCatalogThreads([{id:"old",name:"method · 01",cwd:"/old"}], DEFAULT_BOARD_STATE);
  expect(mergeDetectedProjects([], cached, []).projects).toEqual([]);
  const next = mergeDetectedProjects([{id:"stale",cwd:"/old"}], cached, [{id:"new",name:"New",roots:["/new"],position:0}]);
  expect(next.projects.map(project => project.id)).toEqual(["/new"]);
});

describe("session board model", () => {
  it("maps Codex turn states to the two visible card states", () => {
    expect(threadCardStatus("active")).toBe("active");
    expect(threadCardStatus("inProgress")).toBe("active");
    expect(threadCardStatus("running")).toBe("active");
    expect(threadCardStatus("completed")).toBe("completed");
    expect(threadCardStatus("interrupted")).toBe("");
    expect(threadCardStatus("notLoaded")).toBe("");
  });

  it("detects a status change that requires a full catalog refresh", () => {
    const current = [{ id: "running", status: "active" }, { id: "done", status: "completed" }];
    expect(hasThreadStatusChange(current, { running: "active", done: "completed" })).toBe(false);
    expect(hasThreadStatusChange(current, { running: "completed", done: "completed" })).toBe(true);
  });

  it("detects when a visible Codex task has been archived", () => {
    const current = [{ id: "visible" }, { id: "still-here" }];
    expect(hasArchivedThread(current, ["old-thread"])).toBe(false);
    expect(hasArchivedThread(current, ["old-thread", "visible"])).toBe(true);
  });

  it("keeps the preview active while the transcript still reports the previous completed turn", () => {
    expect(effectiveThreadStatus("active", "completed")).toBe("active");
    expect(effectiveThreadStatus("completed", "active")).toBe("active");
    expect(effectiveThreadStatus("completed", "completed")).toBe("completed");
  });

  it("moves newly completed running threads through pending review to reviewed", () => {
    const running = { id: "review-me", status: "active", recency_at: 10 };
    const runningState = reconcileThreadReviews([running], DEFAULT_BOARD_STATE);
    expect(threadDisplayStatus(running, runningState.threadReviews[running.id])).toBe("active");

    const completed = { ...running, status: "completed", recency_at: 20 };
    const pendingState = reconcileThreadReviews([completed], runningState);
    expect(pendingState.threadReviews[running.id]).toEqual({ state: "pending", activityAt: 20 });
    expect(threadDisplayStatus(completed, pendingState.threadReviews[running.id])).toBe("pending");

    const reviewedState = normalizeBoardState({
      ...pendingState,
      threadReviews: { [running.id]: { state: "reviewed", activityAt: 20 } },
    });
    expect(threadDisplayStatus(completed, reviewedState.threadReviews[running.id])).toBe("reviewed");

    const rerunning = { ...completed, status: "active", recency_at: 30 };
    const rerunningState = reconcileThreadReviews([rerunning], reviewedState);
    const completedAgain = { ...rerunning, status: "completed", recency_at: 40 };
    const pendingAgain = reconcileThreadReviews([completedAgain], rerunningState);
    expect(threadDisplayStatus(completedAgain, pendingAgain.threadReviews[running.id])).toBe("pending");
  });

  it("requires review only for the initiating thread, not a thread it spawned", () => {
    const rootRunning = { id: "root", status: "active", recency_at: 10 };
    const childRunning = {
      id: "child",
      parent_thread_id: "root",
      status: "active",
      recency_at: 11,
    };
    const historicalDelegatedRunning = {
      id: "historical-child",
      delegated: true,
      status: "active",
      recency_at: 12,
    };
    const invokedRunning = {
      id: "reused-thread",
      latest_turn_origin: "delegated" as const,
      status: "active",
      recency_at: 13,
    };
    const runningState = reconcileThreadReviews(
      [rootRunning, childRunning, historicalDelegatedRunning, invokedRunning],
      DEFAULT_BOARD_STATE,
    );
    expect(runningState.threadReviews.root?.state).toBe("running");
    expect(runningState.threadReviews.child).toBeUndefined();
    expect(runningState.threadReviews[historicalDelegatedRunning.id]).toBeUndefined();
    expect(runningState.threadReviews[invokedRunning.id]).toBeUndefined();
    expect(threadDisplayStatus(childRunning, runningState.threadReviews.child)).toBe("active");

    const rootCompleted = { ...rootRunning, status: "completed", recency_at: 20 };
    const childCompleted = { ...childRunning, status: "completed", recency_at: 21 };
    const historicalDelegatedCompleted = {
      ...historicalDelegatedRunning,
      status: "completed",
      recency_at: 22,
    };
    const invokedCompleted = {
      ...invokedRunning,
      status: "completed",
      recency_at: 23,
    };
    const completedState = reconcileThreadReviews(
      [rootCompleted, childCompleted, historicalDelegatedCompleted, invokedCompleted],
      runningState,
    );
    expect(threadDisplayStatus(rootCompleted, completedState.threadReviews.root)).toBe("pending");
    expect(completedState.threadReviews.child).toBeUndefined();
    expect(threadDisplayStatus(childCompleted, completedState.threadReviews.child)).toBe("completed");
    expect(threadDisplayStatus(historicalDelegatedCompleted, completedState.threadReviews[historicalDelegatedCompleted.id])).toBe("completed");
    expect(threadDisplayStatus(invokedCompleted, completedState.threadReviews[invokedCompleted.id])).toBe("completed");

    const directlyStartedAgain = {
      ...invokedCompleted,
      latest_turn_origin: "direct" as const,
      status: "active",
      recency_at: 30,
    };
    const directlyRunningState = reconcileThreadReviews([directlyStartedAgain], completedState);
    expect(directlyRunningState.threadReviews[directlyStartedAgain.id]?.state).toBe("running");
    const directlyCompleted = { ...directlyStartedAgain, status: "completed", recency_at: 40 };
    const directlyCompletedState = reconcileThreadReviews([directlyCompleted], directlyRunningState);
    expect(threadDisplayStatus(
      directlyCompleted,
      directlyCompletedState.threadReviews[directlyCompleted.id],
    )).toBe("pending");
  });

  it("keeps legacy completed threads completed until a running transition is observed", () => {
    const legacy = { id: "legacy", status: "completed", recency_at: 10 };
    const state = reconcileThreadReviews([legacy], DEFAULT_BOARD_STATE);
    expect(state.threadReviews[legacy.id]).toBeUndefined();
    expect(threadDisplayStatus(legacy, state.threadReviews[legacy.id])).toBe("completed");
  });

  it("uses seven default agent rows for detected projects", () => {
    expect(DEFAULT_BOARD_STATE.projects).toEqual([]);
    expect(DEFAULT_AGENTS.map((agent) => agent.name)).toEqual([
      "Router Agent",
      "Domain Agent",
      "Methods Agent",
      "Implementation Agent",
      "Visualization Agent",
      "Writing Agent",
      "Reviewer Agent",
    ]);
  });

  it("groups domain conversations into the Domain Agent row", () => {
    const source = [
      { id: "domain-dot", name: "domain · domain", cwd: "/projects/asd" },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.find((row) => row.id === "domain")?.threads.map((thread) => thread.id)).toEqual([
      "domain-dot",
    ]);
    expect(board.unassignedThreads).toEqual([]);
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
    expect(board.rows.find((row) => row.id === "router")?.threads).toEqual([]);
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
      collapsedThreads: {},
      collapsedRows: {},
      threadReviews: {},
      threadSnapshots: {},
    });
    const board = buildProjectBoard(threads, state, "/projects/manuscript");
    expect(board.rows.flatMap((row) => row.threads).map((thread) => thread.id)).toEqual(["t1", "t3"]);
    expect(board.unassignedThreads.map((thread) => thread.id)).toEqual(["t6"]);
  });

  it("keeps a snapshot card when a previously visible Codex task disappears", () => {
    const cwd = "/projects/asd";
    const live = [{
      id: "archived-later",
      name: "implementation · 05",
      cwd,
      status: "completed",
      recency_at: 42,
    }];
    const remembered = rememberCatalogThreads(live, DEFAULT_BOARD_STATE);
    const state = mergeDetectedProjects([], remembered, [{
      id: "asd",
      name: "ASD",
      position: 0,
      roots: [cwd],
    }]);
    const board = buildProjectBoard([], state, cwd);
    const card = board.rows.find((row) => row.id === "implementation")?.threads[0];

    expect(card).toMatchObject({
      id: "archived-later",
      name: "implementation · 05",
      cwd,
      unavailable: true,
    });
    expect(state.projects.map((project) => project.id)).toEqual([cwd]);
  });

  it("renders a named historical archive returned by the catalog backfill", () => {
    const cwd = "/projects/asd";
    const archived = [{
      id: "archived-before-session-wall",
      name: "method · 1.3.3",
      project_root: cwd,
      recency_at: 21,
    }];
    const remembered = rememberCatalogThreads(archived, DEFAULT_BOARD_STATE);
    const state = mergeDetectedProjects([], remembered, [{
      id: "asd",
      name: "AI+ASD",
      position: 0,
      roots: [cwd],
    }]);
    const board = buildProjectBoard([], state, cwd);

    expect(board.rows.find((row) => row.id === "methods")?.threads[0]).toMatchObject({
      id: "archived-before-session-wall",
      name: "method · 1.3.3",
      unavailable: true,
    });
  });

  it("does not resurrect a missing task that the user explicitly hid", () => {
    const cwd = "/projects/asd";
    const remembered = rememberCatalogThreads([
      { id: "hidden", name: "method · 03", cwd },
    ], normalizeBoardState({
      ...DEFAULT_BOARD_STATE,
      hiddenThreads: { hidden: cwd },
    }));
    const state = mergeDetectedProjects([], remembered, [{
      id: "asd",
      name: "ASD",
      position: 0,
      roots: [cwd],
    }]);
    const board = buildProjectBoard([], state, cwd);

    expect(board.rows.flatMap((row) => row.threads)).toEqual([]);
    expect(board.unassignedThreads).toEqual([]);
  });

  it("uses the live task again after a missing snapshot is restored", () => {
    const cwd = "/projects/asd";
    const first: CodexThread[] = [{ id: "restore-me", name: "router · 创建对话", cwd, recency_at: 10 }];
    const remembered = rememberCatalogThreads(first, DEFAULT_BOARD_STATE);
    const restored: CodexThread[] = [{ ...first[0]!, status: "completed", recency_at: 20 }];
    const state = mergeDetectedProjects(restored, rememberCatalogThreads(restored, remembered));
    const board = buildProjectBoard(restored, state, cwd);

    expect(board.rows.find((row) => row.id === "router")?.threads).toEqual(restored);
    expect(board.rows.find((row) => row.id === "router")?.threads[0]?.unavailable).toBeUndefined();
  });

  it("normalizes persisted folding maps for old and new board state", () => {
    const oldState = normalizeBoardState({ version: 1, projects: [], assignments: {}, hiddenThreads: {} });
    expect(oldState.collapsedThreads).toEqual({});
    expect(oldState.collapsedRows).toEqual({});
    expect(oldState.threadSnapshots).toEqual({});

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

  it("migrates the removed Analysis Agent row to Implementation Agent", () => {
    const state = normalizeBoardState({
      version: 1,
      projects: [{
        id: "/projects/asd",
        name: "ASD",
        agents: [
          { id: "analysis", name: "Analysis Agent" },
          { id: "server-experiment", name: "Server Agent" },
        ],
      }],
      assignments: { t1: { projectId: "/projects/asd", agentId: "analysis" } },
    });
    expect(state.projects[0]?.agents).toEqual([
      { id: "domain", name: "Domain Agent" },
      { id: "implementation", name: "Implementation Agent" },
      { id: "visualization", name: "Visualization Agent" },
    ]);
    expect(state.assignments.t1?.agentId).toBe("implementation");
  });

  it("detects projects from conversation working directories", () => {
    const state = mergeDetectedProjects(threads, DEFAULT_BOARD_STATE);
    expect(state.projects.map((project) => [project.id, project.name])).toEqual([
      ["/projects/manuscript", "manuscript"],
      ["/projects/other", "other"],
    ]);
  });

  it("uses the Codex project catalog and migrates the old basename label", () => {
    const cwd = "/Users/research/AI+ASD/code/V1";
    const source = [{ id: "asd-1", name: "analysis", cwd, recency_at: 10 }];
    const saved = normalizeBoardState({
      version: 1,
      projects: [{ id: cwd, name: "V1", agents: DEFAULT_AGENTS }],
      assignments: {},
    });
    const state = mergeDetectedProjects(source, saved, [{
      id: "project-asd",
      name: "AI+ASD",
      position: 0,
      roots: [cwd],
    }]);
    expect(state.projects[0]?.name).toBe("AI+ASD");
  });

  it("keeps a manual project rename over the catalog name", () => {
    const cwd = "/projects/V1";
    const saved = normalizeBoardState({
      version: 1,
      projects: [{ id: cwd, name: "我的 ASD 项目", agents: DEFAULT_AGENTS }],
      assignments: {},
    });
    const state = mergeDetectedProjects([{ id: "asd-1", cwd }], saved, [{
      id: "project-asd",
      name: "AI+ASD",
      position: 0,
      roots: [cwd],
    }]);
    expect(state.projects[0]?.name).toBe("我的 ASD 项目");
  });

  it("infers AI+ASD from a Codex title when the project catalog has no entry", () => {
    const cwd = "/Users/research/AI+ASD/code/V1";
    const state = mergeDetectedProjects([
      { id: "asd-1", name: "AI+ASD · server · Step5n", cwd },
    ], DEFAULT_BOARD_STATE);
    expect(state.projects[0]?.name).toBe("AI+ASD");
  });

  it("infers a plus-sign project from cwd without putting the project in the title", () => {
    const cwd = "/Users/research/AI+ASD/code/FC_Selection_ASD_AI_TMS/V1";
    const state = mergeDetectedProjects([
      { id: "asd-1", name: "method · normative modeling", cwd },
    ], DEFAULT_BOARD_STATE);
    expect(state.projects[0]?.name).toBe("AI+ASD");
  });

  it("groups the role-middle-dot-function naming format", () => {
    const source = [
      { id: "method-dot", name: "method · normative modeling", cwd: "/projects/asd", recency_at: 1 },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.find((row) => row.id === "methods")?.threads.map((thread) => thread.id)).toEqual(["method-dot"]);
  });

  it("groups implementation conversations into the Implementation Agent row", () => {
    const source = [
      { id: "implementation-dot", name: "implementation · 01-v3", cwd: "/projects/asd" },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.find((row) => row.id === "implementation")?.threads.map((thread) => thread.id)).toEqual([
      "implementation-dot",
    ]);
  });

  it("groups visualization conversations into the Visualization Agent row", () => {
    const source = [
      { id: "visualization-dot", name: "visualization · fig4", cwd: "/projects/asd" },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.find((row) => row.id === "visualization")?.threads.map((thread) => thread.id)).toEqual([
      "visualization-dot",
    ]);
  });

  it("migrates Leader to Router and groups Router conversations", () => {
    const cwd = "/projects/asd";
    const saved = normalizeBoardState({
      version: 1,
      projects: [{ id: cwd, name: "ASD", agents: [{ id: "leader", name: "Leader Agent" }] }],
      assignments: { t1: { projectId: cwd, agentId: "leader" } },
    });
    const source = [{ id: "router-dot", name: "router · main", cwd }];
    const board = buildProjectBoard(source, mergeDetectedProjects(source, saved), cwd);
    expect(saved.projects[0]?.agents[0]).toEqual({ id: "router", name: "Router Agent" });
    expect(saved.assignments.t1?.agentId).toBe("router");
    expect(board.rows.find((row) => row.id === "router")?.threads.map((thread) => thread.id)).toEqual(["router-dot"]);
  });

  it("does not create a Server Agent row", () => {
    const source = [
      { id: "server-dot", name: "server-experiment · Step5n", cwd: "/projects/asd" },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.map((row) => row.id)).not.toContain("server-experiment");
    expect(board.unassignedThreads.map((thread) => thread.id)).toEqual(["server-dot"]);
  });

  it("uses explicit backend role metadata when the title has no role prefix", () => {
    const source = [
      { id: "router-legacy", name: "你是本项目的router agent", agent_role: "router", cwd: "/projects/asd" },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/asd");
    expect(board.rows.find((row) => row.id === "router")?.threads.map((thread) => thread.id)).toEqual([
      "router-legacy",
    ]);
  });

  it("merges Codex worktree conversations into their Git project root", () => {
    const root = "/Users/research/AI+ASD/code/V1";
    const source = [
      { id: "main", name: "method · 01-v3", cwd: root },
      {
        id: "worktree",
        name: "method · 03",
        cwd: "/Users/research/.codex/worktrees/a932/V1",
        project_root: root,
      },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, root);
    expect(state.projects.map((project) => project.id)).toEqual([root]);
    expect(board.rows.find((row) => row.id === "methods")?.threads.map((thread) => thread.id)).toEqual([
      "main",
      "worktree",
    ]);
  });

  it("puts pinned conversations and their project first", () => {
    const source = [
      { id: "new", name: "question", cwd: "/projects/new", recency_at: 100 },
      { id: "pinned", name: "question", cwd: "/projects/pinned", recency_at: 10, pinned: true },
      { id: "older", name: "question", cwd: "/projects/pinned", recency_at: 20 },
    ];
    const state = mergeDetectedProjects(source, DEFAULT_BOARD_STATE);
    const board = buildProjectBoard(source, state, "/projects/pinned");
    expect(state.projects[0]?.id).toBe("/projects/pinned");
    expect(board.unassignedThreads.map((thread) => thread.id)).toEqual(["pinned", "older"]);
  });

  it("builds the native Codex deep link", () => {
    expect(codexThreadUrl("thread a/b")).toBe("codex://threads/thread%20a%2Fb");
  });
});
