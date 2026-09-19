export interface CodexThread {
  id: string;
  name?: string;
  preview?: string;
  cwd?: string;
  project_root?: string;
  status?: string;
  branch?: string;
  model?: string;
  reasoning_effort?: string;
  parent_thread_id?: string;
  delegated?: boolean;
  latest_turn_origin?: "direct" | "delegated";
  created_at?: number;
  updated_at?: number;
  recency_at?: number;
  pinned?: boolean;
  agent_role?: string;
  agent_nickname?: string;
  unavailable?: boolean;
}

export interface CodexReasoningEffort {
  value: string;
  description?: string;
}

export interface CodexModel {
  id: string;
  name: string;
  description?: string;
  default_reasoning_effort: string;
  reasoning_efforts: CodexReasoningEffort[];
  default: boolean;
}

export interface CodexProject {
  id: string;
  name: string;
  position: number;
  roots: string[];
}

export const FOLLOW_TASK_MODEL = "__task_settings__";

export function messageModelOptions(model: string, effort: string): Record<string, string> {
  if (!model || model === FOLLOW_TASK_MODEL) return {};
  return {model, ...(effort ? {reasoning_effort: effort} : {})};
}

export interface CodexTranscriptItem {
  id: string;
  client_id?: string;
  kind: "user" | "assistant" | "tool" | "activity";
  text?: string;
  title?: string;
  status?: string;
  phase?: string;
}

export interface CodexThreadTranscript {
	turn_id?: string;
  thread_id: string;
  status?: string;
  items: CodexTranscriptItem[];
}

export interface BoardAgent {
  id: string;
  name: string;
}

export function transcriptSignature(transcript: CodexThreadTranscript): string {
  return JSON.stringify(transcript);
}

export function transcriptProgress(transcript: CodexThreadTranscript | null, error: string): string {
  if (error) return "进度读取暂时中断，正在重试；当前运行状态尚未确认";
  if (!transcript || threadCardStatus(transcript.status) !== "active") return "";
  const last = transcript.items.at(-1);
  if (last?.kind === "tool" && ["inProgress", "in_progress", "running"].includes(last.status || "")) return "进行中 · 正在执行工具";
  return "进行中 · 等待新的可显示输出";
}

export interface BoardProject {
  id: string;
  name: string;
  agents: BoardAgent[];
}

export interface ThreadAssignment {
  projectId: string;
  agentId: string;
}

export interface ThreadReviewState {
  state: "running" | "pending" | "reviewed";
  activityAt: number;
}

export interface BoardState {
  version: 1;
  projects: BoardProject[];
  assignments: Record<string, ThreadAssignment>;
  hiddenThreads: Record<string, string>;
  collapsedThreads: Record<string, string>;
  collapsedRows: Record<string, boolean>;
  threadReviews: Record<string, ThreadReviewState>;
  threadSnapshots: Record<string, CodexThread>;
}

export interface ProjectBoard {
  project: BoardProject | null;
  rows: Array<BoardAgent & { threads: CodexThread[] }>;
  unassignedThreads: CodexThread[];
}

export type ThreadCardStatus = "active" | "completed" | "pending" | "reviewed" | "";

export const BOARD_STORAGE_KEY = "multica:codex-session-card-wall:v1";

export const DEFAULT_AGENTS: BoardAgent[] = [
  { id: "router", name: "Router Agent" },
  { id: "domain", name: "Domain Agent" },
  { id: "methods", name: "Methods Agent" },
  { id: "implementation", name: "Implementation Agent" },
  { id: "visualization", name: "Visualization Agent" },
  { id: "writing", name: "Writing Agent" },
  { id: "reviewer", name: "Reviewer Agent" },
];

export const DEFAULT_BOARD_STATE: BoardState = {
  version: 1,
  projects: [],
  assignments: {},
  hiddenThreads: {},
  collapsedThreads: {},
  collapsedRows: {},
  threadReviews: {},
  threadSnapshots: {},
};

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function threadCardStatus(value: unknown): ThreadCardStatus {
  const status = text(value).toLocaleLowerCase();
  if (status === "active" || status === "running" || status === "inprogress") return "active";
  return status === "completed" ? "completed" : "";
}

export function effectiveThreadStatus(
  catalogStatus: unknown,
  transcriptStatus: unknown,
): ThreadCardStatus {
  const catalog = threadCardStatus(catalogStatus);
  const transcript = threadCardStatus(transcriptStatus);
  if (catalog === "active" || transcript === "active") return "active";
  return transcript || catalog;
}

export function hasThreadStatusChange(
  threads: CodexThread[],
  statuses: Record<string, string>,
): boolean {
  return threads.some((thread) => {
    const status = statuses[thread.id];
    return typeof status === "string" && status !== thread.status;
  });
}

export function hasArchivedThread(
  threads: CodexThread[],
  archivedThreadIDs: string[],
): boolean {
  const archived = new Set(archivedThreadIDs);
  return threads.some((thread) => archived.has(thread.id));
}

function threadActivity(thread: CodexThread): number {
  return thread.recency_at || thread.updated_at || thread.created_at || 0;
}

export function threadDisplayStatus(
  thread: CodexThread,
  review?: ThreadReviewState,
): ThreadCardStatus {
  if (thread.unavailable) return "";
  const status = threadCardStatus(thread.status);
  if (status === "active") return "active";
  if (status !== "completed") return "";
  if (threadTurnIsDelegated(thread)) return "completed";
  if (review?.state === "running" || review?.state === "pending") return "pending";
  if (review?.state === "reviewed" && review.activityAt >= threadActivity(thread)) return "reviewed";
  if (review?.state === "reviewed") return "pending";
  return "completed";
}

export function reconcileThreadReviews(threads: CodexThread[], sourceState: BoardState): BoardState {
  const state = normalizeBoardState(sourceState);
  const threadReviews = { ...state.threadReviews };
  for (const thread of threads) {
    if (threadTurnIsDelegated(thread)) {
      delete threadReviews[thread.id];
      continue;
    }
    const status = threadCardStatus(thread.status);
    const previous = threadReviews[thread.id];
    const activityAt = threadActivity(thread);
    if (status === "active") {
      threadReviews[thread.id] = { state: "running", activityAt };
    } else if (status === "completed") {
      if (previous?.state === "running" || (previous?.state === "reviewed" && activityAt > previous.activityAt)) {
        threadReviews[thread.id] = { state: "pending", activityAt };
      } else if (previous?.state === "pending" && activityAt > previous.activityAt) {
        threadReviews[thread.id] = { state: "pending", activityAt };
      }
    }
  }
  return { ...state, threadReviews };
}

function threadTurnIsDelegated(thread: CodexThread): boolean {
  return thread.latest_turn_origin === "delegated"
    || Boolean(thread.parent_thread_id || thread.delegated);
}

function normalizeAgents(value: unknown): BoardAgent[] {
  if (!Array.isArray(value)) return [];
  const agents = value
    .map((agent) => {
      const id = text(agent?.id);
      const name = text(agent?.name) || "未命名 Agent";
      if (id === "leader") {
        return { id: "router", name: name === "Leader Agent" ? "Router Agent" : name };
      }
      return id === "analysis"
        ? { id: "implementation", name: name === "Analysis Agent" ? "Implementation Agent" : name }
        : { id, name };
    })
    .filter((agent) => agent.id && agent.id !== "server-experiment" && agent.id !== "server");
  if (agents.length && !agents.some((agent) => agent.id === "visualization")) {
    const implementationIndex = agents.findIndex((agent) => agent.id === "implementation");
    agents.splice(implementationIndex >= 0 ? implementationIndex + 1 : agents.length, 0, {
      id: "visualization",
      name: "Visualization Agent",
    });
  }
  if (agents.length && !agents.some((agent) => agent.id === "domain")) {
    const routerIndex = agents.findIndex((agent) => agent.id === "router");
    agents.splice(routerIndex >= 0 ? routerIndex + 1 : 0, 0, {
      id: "domain",
      name: "Domain Agent",
    });
  }
  return agents;
}

function normalizeThreadSnapshot(value: unknown): CodexThread | null {
  if (!value || typeof value !== "object") return null;
  const source = value as Partial<CodexThread>;
  const id = text(source.id);
  const cwd = text(source.cwd);
  const projectRoot = text(source.project_root);
  if (!id || (!cwd && !projectRoot)) return null;
  const finiteNumber = (candidate: unknown): number | undefined =>
    typeof candidate === "number" && Number.isFinite(candidate) ? candidate : undefined;
  const latestTurnOrigin = source.latest_turn_origin === "direct" || source.latest_turn_origin === "delegated"
    ? source.latest_turn_origin
    : undefined;
  return {
    id,
    name: text(source.name) || undefined,
    preview: text(source.preview) || undefined,
    cwd: cwd || undefined,
    project_root: projectRoot || undefined,
    status: text(source.status) || undefined,
    branch: text(source.branch) || undefined,
    model: text(source.model) || undefined,
    reasoning_effort: text(source.reasoning_effort) || undefined,
    parent_thread_id: text(source.parent_thread_id) || undefined,
    delegated: source.delegated === true || undefined,
    latest_turn_origin: latestTurnOrigin,
    created_at: finiteNumber(source.created_at),
    updated_at: finiteNumber(source.updated_at),
    recency_at: finiteNumber(source.recency_at),
    pinned: source.pinned === true || undefined,
    agent_role: text(source.agent_role) || undefined,
    agent_nickname: text(source.agent_nickname) || undefined,
  };
}

export function normalizeBoardState(value: unknown): BoardState {
  if (!value || typeof value !== "object") {
    return structuredClone(DEFAULT_BOARD_STATE);
  }
  const source = value as Partial<BoardState>;
  const projects = Array.isArray(source.projects)
    ? source.projects
        .map((project) => ({
          id: text(project?.id),
          name: text(project?.name) || "未命名项目",
          agents: normalizeAgents(project?.agents),
        }))
        .filter((project) => project.id)
    : [];

  const assignments: Record<string, ThreadAssignment> = {};
  if (source.assignments && typeof source.assignments === "object") {
    for (const [threadId, assignment] of Object.entries(source.assignments)) {
      const projectId = text(assignment?.projectId);
      const sourceAgentId = text(assignment?.agentId);
      const agentId = sourceAgentId === "analysis"
        ? "implementation"
        : sourceAgentId === "leader" ? "router" : sourceAgentId;
      if (text(threadId) && projectId && agentId) {
        assignments[threadId] = { projectId, agentId };
      }
    }
  }

  const hiddenThreads: Record<string, string> = {};
  if (source.hiddenThreads && typeof source.hiddenThreads === "object") {
    for (const [threadId, projectId] of Object.entries(source.hiddenThreads)) {
      if (text(threadId) && text(projectId)) hiddenThreads[threadId] = text(projectId);
    }
  }

  const collapsedThreads: Record<string, string> = {};
  if (source.collapsedThreads && typeof source.collapsedThreads === "object") {
    for (const [threadId, projectId] of Object.entries(source.collapsedThreads)) {
      if (text(threadId) && text(projectId)) collapsedThreads[threadId] = text(projectId);
    }
  }

  const collapsedRows: Record<string, boolean> = {};
  if (source.collapsedRows && typeof source.collapsedRows === "object") {
    for (const [rowId, collapsed] of Object.entries(source.collapsedRows)) {
      if (text(rowId) && collapsed === true) collapsedRows[rowId] = true;
    }
  }

  const threadReviews: Record<string, ThreadReviewState> = {};
  if (source.threadReviews && typeof source.threadReviews === "object") {
    for (const [threadId, review] of Object.entries(source.threadReviews)) {
      const state = text(review?.state);
      const activityAt = typeof review?.activityAt === "number" && Number.isFinite(review.activityAt)
        ? review.activityAt
        : 0;
      if (text(threadId) && (state === "running" || state === "pending" || state === "reviewed")) {
        threadReviews[threadId] = { state, activityAt };
      }
    }
  }

  const threadSnapshots: Record<string, CodexThread> = {};
  if (source.threadSnapshots && typeof source.threadSnapshots === "object") {
    for (const [threadId, snapshot] of Object.entries(source.threadSnapshots)) {
      const normalized = normalizeThreadSnapshot(snapshot);
      if (normalized && normalized.id === text(threadId)) {
        threadSnapshots[normalized.id] = normalized;
      }
    }
  }

  return {
    version: 1,
    projects: projects.length ? projects : structuredClone(DEFAULT_BOARD_STATE.projects),
    assignments,
    hiddenThreads,
    collapsedThreads,
    collapsedRows,
    threadReviews,
    threadSnapshots,
  };
}

export function rememberCatalogThreads(
  sourceThreads: CodexThread[],
  sourceState: BoardState,
): BoardState {
  const state = normalizeBoardState(sourceState);
  const threadSnapshots = { ...state.threadSnapshots };
  for (const thread of sourceThreads) {
    const snapshot = normalizeThreadSnapshot(thread);
    if (snapshot) threadSnapshots[snapshot.id] = snapshot;
  }
  return { ...state, threadSnapshots };
}

function activity(thread: CodexThread): number {
  return threadActivity(thread);
}

function compareThreads(left: CodexThread, right: CodexThread): number {
  if (Boolean(left.pinned) !== Boolean(right.pinned)) return left.pinned ? -1 : 1;
  return activity(right) - activity(left);
}

function projectName(cwd: string): string {
  return cwd.split(/[\\/]/).filter(Boolean).at(-1) || cwd;
}

function threadProjectPath(thread: CodexThread): string {
  return text(thread.project_root) || text(thread.cwd);
}

function inferredProjectName(cwd: string, threads: CodexThread[]): string {
  const components = cwd.split(/[\\/]/).filter(Boolean).reverse();
  const titles = threads.filter((thread) => threadProjectPath(thread) === cwd).map((thread) => text(thread.name));
  return components.find((component) => titles.some((title) =>
    title.startsWith(`${component} ·`) || title.startsWith(`${component}｜`) || title.startsWith(`${component} |`),
  )) || components.find((component) => component.includes("+")) || projectName(cwd);
}

export function mergeDetectedProjects(
  sourceThreads: CodexThread[],
  sourceState: BoardState,
  catalogProjects?: CodexProject[],
): BoardState {
  const authoritativeCatalog = catalogProjects !== undefined;
  catalogProjects = catalogProjects || [];
  const state = normalizeBoardState(sourceState);
  const catalogRoots = catalogProjects.flatMap((project) => project.roots).filter(Boolean);
  const allowedRoots = new Set(catalogRoots);
  const rememberedThreads = Object.values(state.threadSnapshots).filter((thread) => {
    const root = threadProjectPath(thread);
    return root && (!authoritativeCatalog || allowedRoots.has(root));
  });
  const detectedThreads = [...new Map(
    [...rememberedThreads, ...sourceThreads.filter((thread) => !authoritativeCatalog || allowedRoots.has(threadProjectPath(thread)))].map((thread) => [thread.id, thread]),
  ).values()];
  const orderedThreads = [...detectedThreads].sort(compareThreads);
  const detectedPaths = [...new Set([...catalogRoots, ...orderedThreads.map(threadProjectPath).filter(Boolean)])];
  const catalogByRoot = new Map(catalogProjects.flatMap((project) =>
    project.roots.map((root) => [root, project] as const),
  ));
  detectedPaths.sort((left, right) => {
    const leftPinned = detectedThreads.some((thread) => threadProjectPath(thread) === left && thread.pinned);
    const rightPinned = detectedThreads.some((thread) => threadProjectPath(thread) === right && thread.pinned);
    if (leftPinned !== rightPinned) return leftPinned ? -1 : 1;
    const leftPosition = catalogByRoot.get(left)?.position;
    const rightPosition = catalogByRoot.get(right)?.position;
    if (leftPosition !== undefined && rightPosition !== undefined && leftPosition !== rightPosition) {
      return leftPosition - rightPosition;
    }
    const leftActivity = orderedThreads.find((thread) => threadProjectPath(thread) === left);
    const rightActivity = orderedThreads.find((thread) => threadProjectPath(thread) === right);
    return (rightActivity ? activity(rightActivity) : 0) - (leftActivity ? activity(leftActivity) : 0);
  });
  const legacyProject = state.projects.length === 1 && state.projects[0]?.id === "multiecho-t2"
    ? state.projects[0]
    : null;
  const projects = detectedPaths.map((cwd, index) => {
    const existing = state.projects.find((project) => project.id === cwd);
    const agents = existing?.agents.length
      ? existing.agents
      : index === 0 && legacyProject?.agents.length
        ? legacyProject.agents
        : structuredClone(DEFAULT_AGENTS);
    const automaticName = catalogByRoot.get(cwd)?.name || inferredProjectName(cwd, detectedThreads);
    const storedName = existing?.name && existing.name !== projectName(cwd) ? existing.name : "";
    return {
      id: cwd,
      name: storedName || (index === 0 ? legacyProject?.name : "") || automaticName,
      agents,
    };
  });
  const firstProjectId = projects[0]?.id;
  const assignments = Object.fromEntries(
    Object.entries(state.assignments).map(([threadId, assignment]) => [
      threadId,
      assignment.projectId === "multiecho-t2" && firstProjectId
        ? { ...assignment, projectId: firstProjectId }
        : assignment,
    ]),
  );
  return {
    version: 1,
    projects,
    assignments,
    hiddenThreads: state.hiddenThreads,
    collapsedThreads: state.collapsedThreads,
    collapsedRows: state.collapsedRows,
    threadReviews: state.threadReviews,
    threadSnapshots: state.threadSnapshots,
  };
}

function normalizeRole(value: string): string {
  return value
    .toLocaleLowerCase()
    .replace(/\bagent\b/g, "")
    .replace(/[^a-z0-9]+/g, "")
    .trim();
}

function roleAliases(agent: BoardAgent): Set<string> {
  const aliases = new Set<string>();
  for (const source of [agent.id, agent.name]) {
    const normalized = normalizeRole(source);
    if (!normalized) continue;
    aliases.add(normalized);
    if (normalized.endsWith("s") && !normalized.endsWith("is") && !normalized.endsWith("ss")) {
      aliases.add(normalized.slice(0, -1));
    }
  }
  return aliases;
}

function rowForThread(thread: CodexThread, project: BoardProject): string {
  const explicitRole = normalizeRole(text(thread.agent_role));
  if (explicitRole) {
    const explicitAgent = project.agents.find((agent) => roleAliases(agent).has(explicitRole));
    if (explicitAgent) return explicitAgent.id;
  }
  const title = text(thread.name);
  const middleDot = title.indexOf(" · ");
  const separator = middleDot > 0 ? middleDot : title.indexOf("-");
  if (separator <= 0) return "";
  const role = normalizeRole(title.slice(0, separator));
  if (!role) return "";
  return project.agents.find((agent) => roleAliases(agent).has(role))?.id || "";
}

export function buildProjectBoard(
  sourceThreads: CodexThread[],
  sourceState: BoardState,
  projectId?: string,
): ProjectBoard {
  const state = normalizeBoardState(sourceState);
  const liveThreadIDs = new Set(sourceThreads.map((thread) => thread.id));
  const missingThreads = Object.values(state.threadSnapshots)
    .filter((thread) => !liveThreadIDs.has(thread.id))
    .map((thread) => ({ ...thread, status: undefined, unavailable: true }));
  const threads = [...sourceThreads, ...missingThreads]
    .filter((thread) => threadProjectPath(thread))
    .sort(compareThreads);
  const project = state.projects.find((candidate) => candidate.id === projectId) ?? state.projects[0] ?? null;
  const projectThreads = project
    ? threads.filter((thread) => threadProjectPath(thread) === project.id && state.hiddenThreads[thread.id] !== project.id)
    : [];
  const rows = project
    ? project.agents.map((agent) => ({
        ...agent,
        threads: projectThreads.filter((thread) => rowForThread(thread, project) === agent.id),
      }))
    : [];
  const unassignedThreads = project
    ? projectThreads.filter((thread) => !rowForThread(thread, project))
    : [];

  return {
    project,
    rows,
    unassignedThreads,
  };
}

export function codexThreadUrl(threadId: string): string {
  return `codex://threads/${encodeURIComponent(threadId)}`;
}
