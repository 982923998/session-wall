"use client";

import { useDeferredValue, useEffect, useState } from "react";
import {
  AlertCircle,
  ArrowUpRight,
  Bot,
  Check,
  FolderKanban,
  MoreHorizontal,
  RefreshCw,
  Search,
  Settings2,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import {
  BOARD_STORAGE_KEY,
  DEFAULT_BOARD_STATE,
  buildProjectBoard,
  codexThreadUrl,
  mergeDetectedProjects,
  normalizeBoardState,
  type BoardAgent,
  type BoardState,
  type CodexThread,
} from "./model";

const THREAD_CATALOG_URL = "http://127.0.0.1:19514/codex/threads";
const THREAD_OPEN_URL = "http://127.0.0.1:19514/codex/open-thread";

function newId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID().slice(0, 8)}`;
}

function activityTime(thread: CodexThread): number {
  return thread.recency_at || thread.updated_at || thread.created_at || 0;
}

function formatActivity(thread: CodexThread): string {
  const seconds = activityTime(thread);
  if (!seconds) return "时间未知";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(seconds * 1000));
}

function matchesSearch(thread: CodexThread, query: string): boolean {
  if (!query) return true;
  return [thread.name, thread.preview, thread.cwd, thread.branch]
    .filter(Boolean)
    .join(" ")
    .toLocaleLowerCase()
    .includes(query);
}

function SessionCard({
  thread,
  agents,
  selectedAgentId,
  onMove,
  onOpen,
  onDelete,
}: {
  thread: CodexThread;
  agents: BoardAgent[];
  selectedAgentId: string;
  onMove: (agentId: string) => void;
  onOpen: () => void;
  onDelete: () => void;
}) {
  const title = thread.name || thread.preview || "未命名会话";
  return (
    <article className="group flex min-h-24 w-52 shrink-0 flex-col rounded-xl border border-surface-border bg-surface-raised p-3 shadow-xs transition-[border-color,box-shadow,transform] duration-150 hover:-translate-y-0.5 hover:border-brand/45 hover:shadow-md">
      <div className="flex items-start gap-1">
        <a
          href={codexThreadUrl(thread.id)}
          onClick={(event) => {
            event.preventDefault();
            onOpen();
          }}
          className="min-w-0 flex-1 outline-none focus-visible:ring-2 focus-visible:ring-brand/60"
          title="在 Codex 中继续此会话"
        >
          <span className="line-clamp-2 text-body font-semibold text-foreground">{title}</span>
        </a>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="ghost" size="icon-xs" className="-mt-1 -mr-1 text-muted-foreground" />}>
            <MoreHorizontal />
            <span className="sr-only">卡片操作</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            {agents.map((agent) => (
              <DropdownMenuItem key={agent.id} onClick={() => onMove(agent.id)}>
                {agent.id === selectedAgentId ? <Check /> : <span className="size-4" />}
                {agent.name}
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              <Trash2 /> 删除卡片
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <button
        type="button"
        onClick={onOpen}
        className="mt-auto flex items-center justify-between pt-3 text-left text-micro text-muted-foreground"
      >
        <span>{formatActivity(thread)}</span>
        <ArrowUpRight className="size-3.5 transition-colors group-hover:text-brand" />
      </button>
    </article>
  );
}

function AgentRow({
  agent,
  threads,
  allAgents,
  query,
  onMove,
  onOpen,
  onDelete,
}: {
  agent: BoardAgent;
  threads: CodexThread[];
  allAgents: BoardAgent[];
  query: string;
  onMove: (threadId: string, agentId: string) => void;
  onOpen: (thread: CodexThread) => void;
  onDelete: (threadId: string) => void;
}) {
  const visible = threads.filter((thread) => matchesSearch(thread, query));
  return (
    <section className="grid min-h-40 grid-cols-[184px_minmax(0,1fr)] overflow-hidden rounded-xl border border-surface-border bg-surface-raised/80 shadow-xs max-md:grid-cols-1">
      <header className="border-r border-surface-border bg-surface-hover/60 p-4 max-md:border-r-0 max-md:border-b">
        <div className="flex items-center gap-2">
          <Bot className="size-4 text-brand" />
          <h2 className="text-body font-semibold">{agent.name}</h2>
        </div>
        <p className="mt-2 text-caption text-muted-foreground">{visible.length} 张会话卡片</p>
      </header>
      <div className="flex min-w-0 gap-3 overflow-x-auto p-3">
        {visible.length ? (
          visible.map((thread) => (
            <SessionCard
              key={thread.id}
              thread={thread}
              agents={allAgents}
              selectedAgentId={agent.id}
              onMove={(agentId) => onMove(thread.id, agentId)}
              onOpen={() => onOpen(thread)}
              onDelete={() => onDelete(thread.id)}
            />
          ))
        ) : (
          <div className="grid min-w-56 flex-1 place-items-center text-caption text-muted-foreground">
            {query ? "没有匹配的会话" : "暂无会话卡片"}
          </div>
        )}
      </div>
    </section>
  );
}

export function SessionCardWall() {
  const [boardState, setBoardState] = useState<BoardState>(DEFAULT_BOARD_STATE);
  const [threads, setThreads] = useState<CodexThread[]>([]);
  const [activeProjectId, setActiveProjectId] = useState(DEFAULT_BOARD_STATE.projects[0]?.id ?? "");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftAgents, setDraftAgents] = useState("");
  const deferredQuery = useDeferredValue(query.trim().toLocaleLowerCase());
  const board = buildProjectBoard(threads, boardState, activeProjectId);
  const projectCardCount = board.rows.reduce((sum, row) => sum + row.threads.length, 0);

  function persist(next: BoardState) {
    const normalized = normalizeBoardState(next);
    setBoardState(normalized);
    localStorage.setItem(BOARD_STORAGE_KEY, JSON.stringify(normalized));
  }

  async function refreshThreads(stateOverride?: BoardState) {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(THREAD_CATALOG_URL, { cache: "no-store" });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      const payload = (await response.json()) as { threads?: CodexThread[] };
      const nextThreads = Array.isArray(payload.threads) ? payload.threads : [];
      const merged = mergeDetectedProjects(nextThreads, stateOverride || boardState);
      setThreads(nextThreads);
      persist(merged);
      setActiveProjectId(merged.projects[0]?.id || "");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    let initialState = DEFAULT_BOARD_STATE;
    const saved = localStorage.getItem(BOARD_STORAGE_KEY);
    if (saved) {
      try {
        const normalized = normalizeBoardState(JSON.parse(saved));
        initialState = normalized;
        setBoardState(normalized);
      } catch {
        localStorage.removeItem(BOARD_STORAGE_KEY);
      }
    }
    void refreshThreads(initialState);
  }, []);

  function moveThread(threadId: string, agentId: string) {
    if (!board.project) return;
    const assignments = { ...boardState.assignments };
    if (!agentId) return;
    assignments[threadId] = { projectId: board.project.id, agentId };
    persist({ ...boardState, assignments });
  }

  function deleteCard(threadId: string) {
    if (!board.project) return;
    const previous = boardState;
    persist({
      ...boardState,
      hiddenThreads: { ...boardState.hiddenThreads, [threadId]: board.project.id },
    });
    toast("卡片已从项目中删除", {
      action: { label: "撤销", onClick: () => persist(previous) },
    });
  }

  async function openThread(thread: CodexThread) {
    setError("");
    try {
      const response = await fetch(THREAD_OPEN_URL, {
        method: "POST",
        headers: { "Content-Type": "text/plain;charset=UTF-8" },
        body: thread.id,
      });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  function openProjectEditor() {
    setDraftName(board.project?.name || "");
    setDraftAgents(board.project?.agents.map((agent) => agent.name).join("\n") || "");
    setDialogOpen(true);
  }

  function saveProject() {
    const name = draftName.trim();
    const names = draftAgents.split("\n").map((agent) => agent.trim()).filter(Boolean);
    if (!name || !names.length) return;
    const existing = board.project;
    if (!existing) return;
    const agents = names.map((agentName) => {
      const old = existing?.agents.find((agent) => agent.name === agentName);
      return old || { id: newId("agent"), name: agentName };
    });
    const project = { id: existing.id, name, agents };
    const projects = boardState.projects.map((candidate) => (candidate.id === existing.id ? project : candidate));
    const validAgentIds = new Set(agents.map((agent) => agent.id));
    const assignments = Object.fromEntries(
      Object.entries(boardState.assignments).filter(([, assignment]) =>
        assignment.projectId !== project.id || validAgentIds.has(assignment.agentId),
      ),
    );
    persist({ version: 1, projects, assignments, hiddenThreads: boardState.hiddenThreads });
    setActiveProjectId(project.id);
    setDialogOpen(false);
  }

  return (
    <div className="flex h-dvh bg-background text-foreground">
      <aside className="flex w-64 shrink-0 flex-col border-r border-surface-border bg-sidebar p-3 max-lg:w-52 max-md:hidden">
        <div className="flex items-center gap-2 px-2 py-3">
          <div className="grid size-8 place-items-center rounded-lg bg-brand text-brand-foreground">
            <FolderKanban className="size-4" />
          </div>
          <div>
            <div className="text-body font-semibold">Session Wall</div>
            <div className="text-micro text-muted-foreground">Codex 会话索引</div>
          </div>
        </div>
        <div className="mt-3 px-2 text-micro font-medium uppercase tracking-wider text-muted-foreground">检测到的项目</div>
        <nav className="mt-1 space-y-1">
          {boardState.projects.map((project) => (
            <button
              key={project.id}
              type="button"
              onClick={() => setActiveProjectId(project.id)}
              className={cn(
                "flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-body transition-colors",
                project.id === board.project?.id
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
              )}
            >
              <FolderKanban className="size-4" />
              <span className="truncate">{project.name}</span>
            </button>
          ))}
        </nav>
        <div className="mt-auto rounded-lg border border-surface-border bg-surface-hover/45 p-3 text-caption leading-5 text-muted-foreground">
          项目和会话由 Codex 工作目录自动识别。这里只保存 Agent 行归类。
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-y-auto">
        <header className="sticky top-0 z-10 border-b border-surface-border bg-background/90 px-6 py-4 backdrop-blur-md max-sm:px-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-micro font-medium uppercase tracking-[0.14em] text-brand">Project conversation atlas</p>
              <h1 className="mt-0.5 font-heading text-xl font-semibold">{board.project?.name || "会话卡片墙"}</h1>
            </div>
            <div className="flex items-center gap-2">
              <div className="relative w-72 max-sm:w-48">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input value={query} onChange={(event) => setQuery(event.target.value)} className="pl-8" placeholder="搜索会话" />
              </div>
              <Button variant="outline" onClick={openProjectEditor} disabled={!board.project}>
                <Settings2 /> 项目设置
              </Button>
              <Button variant="brand" onClick={() => void refreshThreads()} disabled={loading}>
                <RefreshCw className={cn(loading && "animate-spin")} /> 刷新会话
              </Button>
            </div>
          </div>
          <div className="mt-3 flex gap-2 text-caption text-muted-foreground">
            <span className="rounded-full bg-muted px-2 py-1">{projectCardCount} 张项目卡片</span>
            <span className="rounded-full bg-muted px-2 py-1">{board.rows.length} 个 Agent 行</span>
          </div>
        </header>

        <div className="space-y-3 p-6 max-sm:p-3">
          {error ? (
            <div className="flex items-start gap-3 rounded-xl border border-destructive/25 bg-destructive/6 p-4 text-body">
              <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
              <div>
                <div className="font-medium">无法读取 Codex 会话目录</div>
                <div className="mt-1 text-caption text-muted-foreground">{error}</div>
                <div className="mt-1 text-caption text-muted-foreground">请确认本地 Multica daemon 已启动。</div>
              </div>
            </div>
          ) : null}
          {board.rows.map((row) => (
            <AgentRow
              key={row.id}
              agent={row}
              threads={row.threads}
              allAgents={board.project?.agents || []}
              query={deferredQuery}
              onMove={moveThread}
              onOpen={(thread) => void openThread(thread)}
              onDelete={deleteCard}
            />
          ))}
        </div>
      </main>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>项目名称与 Agent 行</DialogTitle>
            <DialogDescription>项目目录由 Codex 自动识别。每行填写一个 Agent，未指定角色的会话默认进入第一行。</DialogDescription>
          </DialogHeader>
          <label className="space-y-1.5 text-caption font-medium">
            <span>项目名称</span>
            <Input value={draftName} onChange={(event) => setDraftName(event.target.value)} placeholder="例如 Multiecho-T2" />
          </label>
          <label className="space-y-1.5 text-caption font-medium">
            <span>Agent 行，每行一个</span>
            <textarea
              value={draftAgents}
              onChange={(event) => setDraftAgents(event.target.value)}
              className="min-h-44 w-full resize-y rounded-lg border border-input bg-background p-2.5 text-body outline-none focus:border-brand focus:ring-2 focus:ring-brand/20"
              placeholder={"Leader Agent\nMethods Agent\nAnalysis Agent\nWriting Agent\nReviewer Agent"}
            />
          </label>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button variant="brand" onClick={saveProject} disabled={!draftName.trim() || !draftAgents.trim()}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </div>
  );
}
