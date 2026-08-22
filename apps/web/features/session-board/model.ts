export interface CodexThread {
  id: string;
  name?: string;
  preview?: string;
  cwd?: string;
  status?: string;
  branch?: string;
  created_at?: number;
  updated_at?: number;
  recency_at?: number;
  pinned?: boolean;
  agent_role?: string;
  agent_nickname?: string;
}

export interface BoardAgent {
  id: string;
  name: string;
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

export interface BoardState {
  version: 1;
  projects: BoardProject[];
  assignments: Record<string, ThreadAssignment>;
  hiddenThreads: Record<string, string>;
  collapsedThreads: Record<string, string>;
  collapsedRows: Record<string, boolean>;
}

export interface ProjectBoard {
  project: BoardProject | null;
  rows: Array<BoardAgent & { threads: CodexThread[] }>;
  unassignedThreads: CodexThread[];
}

export const BOARD_STORAGE_KEY = "multica:codex-session-card-wall:v1";

export const DEFAULT_AGENTS: BoardAgent[] = [
  { id: "leader", name: "Leader Agent" },
  { id: "methods", name: "Methods Agent" },
  { id: "analysis", name: "Analysis Agent" },
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
};

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
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
          agents: Array.isArray(project?.agents)
            ? project.agents
                .map((agent) => ({ id: text(agent?.id), name: text(agent?.name) || "未命名 Agent" }))
                .filter((agent) => agent.id)
            : [],
        }))
        .filter((project) => project.id)
    : [];

  const assignments: Record<string, ThreadAssignment> = {};
  if (source.assignments && typeof source.assignments === "object") {
    for (const [threadId, assignment] of Object.entries(source.assignments)) {
      const projectId = text(assignment?.projectId);
      const agentId = text(assignment?.agentId);
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

  return {
    version: 1,
    projects: projects.length ? projects : structuredClone(DEFAULT_BOARD_STATE.projects),
    assignments,
    hiddenThreads,
    collapsedThreads,
    collapsedRows,
  };
}

function activity(thread: CodexThread): number {
  return thread.recency_at || thread.updated_at || thread.created_at || 0;
}

function projectName(cwd: string): string {
  return cwd.split("/").filter(Boolean).at(-1) || cwd;
}

export function mergeDetectedProjects(sourceThreads: CodexThread[], sourceState: BoardState): BoardState {
  const state = normalizeBoardState(sourceState);
  const orderedThreads = [...sourceThreads].sort((left, right) => activity(right) - activity(left));
  const detectedPaths = [...new Set(orderedThreads.map((thread) => text(thread.cwd)).filter(Boolean))];
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
    return {
      id: cwd,
      name: existing?.name || (index === 0 ? legacyProject?.name : "") || projectName(cwd),
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
  const title = text(thread.name);
  const separator = title.indexOf("-");
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
  const threads = [...sourceThreads]
    .filter((thread) => text(thread.cwd))
    .sort((left, right) => activity(right) - activity(left));
  const project = state.projects.find((candidate) => candidate.id === projectId) ?? state.projects[0] ?? null;
  const projectThreads = project
    ? threads.filter((thread) => thread.cwd === project.id && state.hiddenThreads[thread.id] !== project.id)
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
