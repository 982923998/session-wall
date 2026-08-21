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
}

export interface ProjectBoard {
  project: BoardProject | null;
  rows: Array<BoardAgent & { threads: CodexThread[] }>;
  availableThreads: CodexThread[];
}

export const BOARD_STORAGE_KEY = "multica:codex-session-card-wall:v1";

export const DEFAULT_BOARD_STATE: BoardState = {
  version: 1,
  projects: [
    {
      id: "multiecho-t2",
      name: "Multiecho-T2",
      agents: [
        { id: "leader", name: "Leader Agent" },
        { id: "methods", name: "Methods Agent" },
        { id: "analysis", name: "Analysis Agent" },
        { id: "writing", name: "Writing Agent" },
        { id: "reviewer", name: "Reviewer Agent" },
      ],
    },
  ],
  assignments: {},
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

  return {
    version: 1,
    projects: projects.length ? projects : structuredClone(DEFAULT_BOARD_STATE.projects),
    assignments,
  };
}

function activity(thread: CodexThread): number {
  return thread.recency_at || thread.updated_at || thread.created_at || 0;
}

export function buildProjectBoard(
  sourceThreads: CodexThread[],
  sourceState: BoardState,
  projectId?: string,
): ProjectBoard {
  const state = normalizeBoardState(sourceState);
  const threads = [...sourceThreads].sort((left, right) => activity(right) - activity(left));
  const project = state.projects.find((candidate) => candidate.id === projectId) ?? state.projects[0] ?? null;
  const rows = project
    ? project.agents.map((agent) => ({
        ...agent,
        threads: threads.filter((thread) => {
          const assignment = state.assignments[thread.id];
          return assignment?.projectId === project.id && assignment.agentId === agent.id;
        }),
      }))
    : [];

  return {
    project,
    rows,
    availableThreads: threads.filter((thread) => !state.assignments[thread.id]),
  };
}

export function codexThreadUrl(threadId: string): string {
  return `codex://threads/${encodeURIComponent(threadId)}`;
}
