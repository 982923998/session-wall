"use client";

import { useDeferredValue, useEffect, useRef, useState } from "react";
import {
  AlertCircle,
  ArchiveRestore,
  ArrowUpRight,
  BadgeCheck,
  Bot,
  Brain,
  CircleCheck,
  ChevronDown,
  ChevronRight,
  Clock3,
  FolderKanban,
  Loader2,
  Maximize2,
  MessageSquareText,
  Minimize2,
  MoreHorizontal,
  Pin,
  RefreshCw,
  Search,
  SendHorizontal,
  Settings2,
  Terminal,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import {parseNativeDelivery, nativeDeliveryText} from "./native-delivery";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
import { MemoizedMarkdown } from "@multica/ui/markdown";
import { cn } from "@multica/ui/lib/utils";
import {
  BOARD_STORAGE_KEY,
  DEFAULT_BOARD_STATE,
  buildProjectBoard,
  effectiveThreadStatus,
  hasThreadStatusChange,
  hasArchivedThread,
  mergeDetectedProjects,
  normalizeBoardState,
  reconcileThreadReviews,
  rememberCatalogThreads,
  threadCardStatus,
  threadDisplayStatus,
  type BoardAgent,
  type BoardState,
  type CodexModel,
  type CodexProject,
  type CodexThread,
  type CodexThreadTranscript,
  type CodexTranscriptItem,
  type ThreadCardStatus,
  type ThreadReviewState,
} from "./model";

const THREAD_CATALOG_URL = "http://127.0.0.1:19514/codex/threads";
const THREAD_STATUS_URL = "http://127.0.0.1:19514/codex/thread-statuses";
const THREAD_TRANSCRIPT_URL = "http://127.0.0.1:19514/codex/thread-transcript";
const CODEX_MODELS_URL = "http://127.0.0.1:19514/codex/models";
const THREAD_SEND_URL = "http://127.0.0.1:19514/codex/send-message";
const THREAD_OPEN_URL = "http://127.0.0.1:19514/codex/open-thread";
const THREAD_RESTORE_URL = "http://127.0.0.1:19514/codex/restore-thread";
const THREAD_STATUS_POLL_MS = 5_000;
const THREAD_TRANSCRIPT_POLL_MS = 500;
const UNASSIGNED_ROW_ID = "unassigned";

const REASONING_LABELS: Record<string, string> = {
  none: "无",
  minimal: "最低",
  low: "低",
  medium: "中",
  high: "高",
  xhigh: "超高",
  max: "最大",
  ultra: "Ultra",
};

function reasoningLabel(value: string): string {
  return REASONING_LABELS[value] || value;
}

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

function rowStateKey(projectId: string, rowId: string): string {
  return `${projectId}::${rowId}`;
}

function ThreadStatusBadge({
  status,
  compact,
  inline = false,
}: {
  status: ThreadCardStatus;
  compact: boolean;
  inline?: boolean;
}) {
  if (!status) return null;
  const active = status === "active";
  const pending = status === "pending";
  const reviewed = status === "reviewed";
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1 px-1.5",
        !inline && "mt-2 self-start",
        compact && "w-full px-1 text-micro",
        active && "border-info/25 bg-info/10 text-info",
        pending && "border-warning/30 bg-warning/10 text-warning",
        reviewed && "border-success/25 bg-success/10 text-success",
        status === "completed" && "border-surface-border bg-muted/60 text-muted-foreground",
      )}
    >
      {active ? <Loader2 className="animate-spin" /> : pending ? <Clock3 /> : reviewed ? <BadgeCheck /> : <CircleCheck />}
      {active ? "进行中" : pending ? "待审阅" : reviewed ? "已审阅" : "已完成"}
    </Badge>
  );
}

function TranscriptItem({ item }: { item: CodexTranscriptItem }) {
  if (item.kind === "tool") {
    return (
      <details className="rounded-lg border border-surface-border bg-surface-hover/45 px-3 py-2 text-caption">
        <summary className="flex cursor-pointer list-none items-center gap-2 text-muted-foreground">
          <Terminal className="size-3.5 shrink-0" />
          <span className="min-w-0 flex-1 truncate">{item.title || "工具调用"}</span>
          {item.status ? <span className="shrink-0 text-micro">{item.status}</span> : null}
        </summary>
        {item.text ? (
          <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-2 font-mono text-micro leading-5 text-muted-foreground">
            {item.text}
          </pre>
        ) : null}
      </details>
    );
  }
  if (item.kind === "activity") {
    return (
      <details className="px-1 text-caption text-muted-foreground">
        <summary className="flex cursor-pointer list-none items-center gap-2">
          <Brain className="size-3.5" />
          <span>{item.title || "处理过程"}</span>
        </summary>
        {item.text ? <div className="mt-1 whitespace-pre-wrap pl-5 text-micro">{item.text}</div> : null}
      </details>
    );
  }
  const user = item.kind === "user";
  return (
    <article className={cn("flex", user ? "justify-end" : "justify-start")}>
      <div className={cn(
        "max-w-[92%] rounded-xl border px-3 py-2 shadow-xs",
        user
          ? "border-brand/20 bg-brand/8"
          : "border-surface-border bg-surface-raised",
      )}>
        <div className="mb-1 text-micro font-medium text-muted-foreground">
          {user ? "你" : item.phase === "commentary" ? "Codex · 进度" : "Codex"}
        </div>
        <MemoizedMarkdown id={`${item.id}-${item.text?.length || 0}`} mode="minimal">
          {item.text || ""}
        </MemoizedMarkdown>
      </div>
    </article>
  );
}

function SessionCard({
  thread,
  collapsed,
  status,
  onToggleCollapsed,
  onPreview,
  onOpen,
  onDelete,
  onRestore,
  restoring,
}: {
  thread: CodexThread;
  collapsed: boolean;
  status: ThreadCardStatus;
  onToggleCollapsed: () => void;
  onPreview: () => void;
  onOpen: () => void;
  onDelete: () => void;
  onRestore: () => void;
  restoring: boolean;
}) {
  const title = thread.name || thread.preview || "未命名会话";
  const unavailable = Boolean(thread.unavailable);
  return (
    <article
      onClick={(event) => {
        if (!unavailable && !(event.target as HTMLElement).closest("button,a")) onPreview();
      }}
      className={cn(
        "group flex min-h-24 shrink-0 cursor-pointer flex-col rounded-xl border border-surface-border bg-surface-raised shadow-xs transition-[width,border-color,box-shadow,transform] duration-150 hover:-translate-y-0.5 hover:border-brand/45 hover:shadow-md",
        collapsed ? "w-20 p-2" : "w-52 p-3",
        status === "active" && "border-info/35",
        unavailable && "cursor-default border-warning/35 bg-warning/5 hover:border-warning/55",
      )}
    >
      <div className={cn("flex gap-1", collapsed ? "flex-col" : "items-start")}>
        <button
          type="button"
          onClick={onPreview}
          disabled={unavailable}
          className="min-w-0 flex-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-brand/60"
          title="查看会话内容"
        >
          <span className="flex items-start gap-1">
            {thread.pinned ? <Pin className="mt-0.5 size-3.5 shrink-0 text-brand" aria-label="已置顶" /> : null}
            <span className={cn(
              "font-semibold text-foreground",
              collapsed ? "line-clamp-3 break-all text-caption leading-4" : "line-clamp-2 text-body",
            )}>{title}</span>
          </span>
        </button>
        <div className={cn("flex shrink-0", collapsed && "justify-between")}>
          {!collapsed && !unavailable ? (
            <Button variant="ghost" size="icon-xs" className="text-muted-foreground" onClick={onOpen} title="在 Codex 中打开">
              <ArrowUpRight />
              <span className="sr-only">在 Codex 中打开</span>
            </Button>
          ) : null}
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            onClick={onToggleCollapsed}
            title={collapsed ? "展开卡片" : "折叠卡片"}
          >
            {collapsed ? <Maximize2 /> : <Minimize2 />}
            <span className="sr-only">{collapsed ? "展开卡片" : "折叠卡片"}</span>
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="ghost" size="icon-xs" className="text-muted-foreground" />}>
              <MoreHorizontal />
              <span className="sr-only">卡片操作</span>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-44">
              <DropdownMenuItem variant="destructive" onClick={onDelete}>
                <Trash2 /> 删除卡片
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {unavailable ? (
        <Badge
          variant="outline"
          className={cn(
            "mt-2 gap-1 border-warning/35 bg-warning/10 px-1.5 text-warning",
            collapsed && "w-full justify-center px-1 text-micro",
          )}
          title="该任务已不在 Codex 当前目录中，通常是被归档；原工作树若已清理，取消归档也不会重建文件。"
        >
          <ArchiveRestore />
          {collapsed ? <span className="sr-only">任务已归档或不可用</span> : "已归档或不可用"}
        </Badge>
      ) : <ThreadStatusBadge status={status} compact={collapsed} />}
      {unavailable ? (
        <Button
          variant="outline"
          size={collapsed ? "icon-xs" : "sm"}
          className={cn("mt-auto border-warning/35 text-warning hover:bg-warning/10", !collapsed && "w-full")}
          onClick={onRestore}
          disabled={restoring}
          title="恢复 Codex 任务"
        >
          {restoring ? <Loader2 className="animate-spin" /> : <ArchiveRestore />}
          {!collapsed ? (restoring ? "恢复中" : "恢复任务") : <span className="sr-only">恢复任务</span>}
        </Button>
      ) : !collapsed ? (
        <button
          type="button"
          onClick={onPreview}
          className="mt-auto flex items-center justify-between pt-3 text-left text-micro text-muted-foreground"
        >
          <span>{formatActivity(thread)}</span>
          <MessageSquareText className="size-3.5 transition-colors group-hover:text-brand" />
        </button>
      ) : null}
    </article>
  );
}

function AgentRow({
  agent,
  threads,
  query,
  collapsed,
  collapsedThreads,
  reviewStates,
  onToggleCollapsed,
  onToggleThread,
  onPreview,
  onOpen,
  onDelete,
  onRestore,
  restoringThreadId,
}: {
  agent: BoardAgent;
  threads: CodexThread[];
  query: string;
  collapsed: boolean;
  collapsedThreads: Record<string, string>;
  reviewStates: Record<string, ThreadReviewState>;
  onToggleCollapsed: () => void;
  onToggleThread: (threadId: string) => void;
  onPreview: (thread: CodexThread) => void;
  onOpen: (thread: CodexThread) => void;
  onDelete: (threadId: string) => void;
  onRestore: (thread: CodexThread) => void;
  restoringThreadId: string;
}) {
  const visible = threads.filter((thread) => matchesSearch(thread, query));
  if (collapsed) {
    return (
      <section className="overflow-hidden rounded-xl border border-surface-border bg-surface-raised/80 shadow-xs">
        <button
          type="button"
          onClick={onToggleCollapsed}
          className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-hover/60"
        >
          <ChevronRight className="size-4 text-muted-foreground" />
          <Bot className="size-4 text-brand" />
          <span className="text-body font-semibold">{agent.name}</span>
          <span className="ml-auto text-caption text-muted-foreground">{visible.length} 张会话卡片</span>
        </button>
      </section>
    );
  }
  return (
    <section className="grid min-h-40 grid-cols-[184px_minmax(0,1fr)] overflow-hidden rounded-xl border border-surface-border bg-surface-raised/80 shadow-xs max-md:grid-cols-1">
      <header className="border-r border-surface-border bg-surface-hover/60 p-4 max-md:border-r-0 max-md:border-b">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon-xs" className="-ml-1 text-muted-foreground" onClick={onToggleCollapsed} title="折叠整行">
            <ChevronDown />
            <span className="sr-only">折叠 {agent.name}</span>
          </Button>
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
              collapsed={Boolean(collapsedThreads[thread.id])}
              status={threadDisplayStatus(thread, reviewStates[thread.id])}
              onToggleCollapsed={() => onToggleThread(thread.id)}
              onPreview={() => onPreview(thread)}
              onOpen={() => onOpen(thread)}
              onDelete={() => onDelete(thread.id)}
              onRestore={() => onRestore(thread)}
              restoring={restoringThreadId === thread.id}
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

function UnassignedThreadRow({
  thread,
  collapsed,
  status,
  onToggleCollapsed,
  onPreview,
  onOpen,
  onDelete,
  onRestore,
  restoring,
}: {
  thread: CodexThread;
  collapsed: boolean;
  status: ThreadCardStatus;
  onToggleCollapsed: () => void;
  onPreview: () => void;
  onOpen: () => void;
  onDelete: () => void;
  onRestore: () => void;
  restoring: boolean;
}) {
  const title = thread.name || thread.preview || "未命名会话";
  const unavailable = Boolean(thread.unavailable);
  return (
    <div className={cn(
      "flex items-center gap-2 border-b border-surface-border/70 px-3 last:border-b-0",
      collapsed ? "py-1.5" : "py-2.5",
    )}>
      <Button variant="ghost" size="icon-xs" className="shrink-0 text-muted-foreground" onClick={onToggleCollapsed} title={collapsed ? "展开会话" : "折叠会话"}>
        {collapsed ? <ChevronRight /> : <ChevronDown />}
        <span className="sr-only">{collapsed ? "展开会话" : "折叠会话"}</span>
      </Button>
      {thread.pinned ? <Pin className="size-3.5 shrink-0 text-brand" aria-label="已置顶" /> : null}
      <button type="button" onClick={onPreview} disabled={unavailable} className="min-w-0 flex-1 truncate text-left text-body font-medium hover:text-brand disabled:text-muted-foreground" title={title}>
        {title}
      </button>
      {unavailable ? (
        <Badge variant="outline" className="gap-1 border-warning/35 bg-warning/10 text-warning">
          <ArchiveRestore /> 已归档或不可用
        </Badge>
      ) : <ThreadStatusBadge status={status} compact={false} inline />}
      {!collapsed ? <span className="shrink-0 text-micro text-muted-foreground">{formatActivity(thread)}</span> : null}
      {unavailable ? (
        <Button variant="outline" size="sm" className="shrink-0 border-warning/35 text-warning" onClick={onRestore} disabled={restoring}>
          {restoring ? <Loader2 className="animate-spin" /> : <ArchiveRestore />}
          {restoring ? "恢复中" : "恢复任务"}
        </Button>
      ) : !collapsed ? (
        <Button variant="ghost" size="icon-xs" className="shrink-0 text-muted-foreground" onClick={onOpen} title="在 Codex 中打开">
          <ArrowUpRight />
          <span className="sr-only">打开会话</span>
        </Button>
      ) : null}
      <DropdownMenu>
        <DropdownMenuTrigger render={<Button variant="ghost" size="icon-xs" className="shrink-0 text-muted-foreground" />}>
          <MoreHorizontal />
          <span className="sr-only">会话操作</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-44">
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            <Trash2 /> 删除会话
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function UnassignedSection({
  threads,
  query,
  collapsed,
  collapsedThreads,
  reviewStates,
  onToggleCollapsed,
  onToggleThread,
  onPreview,
  onOpen,
  onDelete,
  onRestore,
  restoringThreadId,
}: {
  threads: CodexThread[];
  query: string;
  collapsed: boolean;
  collapsedThreads: Record<string, string>;
  reviewStates: Record<string, ThreadReviewState>;
  onToggleCollapsed: () => void;
  onToggleThread: (threadId: string) => void;
  onPreview: (thread: CodexThread) => void;
  onOpen: (thread: CodexThread) => void;
  onDelete: (threadId: string) => void;
  onRestore: (thread: CodexThread) => void;
  restoringThreadId: string;
}) {
  const visible = threads.filter((thread) => matchesSearch(thread, query));
  return (
    <section className="overflow-hidden rounded-xl border border-surface-border bg-surface-raised/80 shadow-xs">
      <button
        type="button"
        onClick={onToggleCollapsed}
        className="flex w-full items-center gap-3 bg-surface-hover/60 px-4 py-3 text-left transition-colors hover:bg-surface-hover"
      >
        {collapsed ? <ChevronRight className="size-4 text-muted-foreground" /> : <ChevronDown className="size-4 text-muted-foreground" />}
        <span className="text-body font-semibold">无角色会话</span>
        <span className="ml-auto text-caption text-muted-foreground">{visible.length} 个会话</span>
      </button>
      {!collapsed ? (
        <div>
          {visible.length ? visible.map((thread) => (
            <UnassignedThreadRow
              key={thread.id}
              thread={thread}
              collapsed={Boolean(collapsedThreads[thread.id])}
              status={threadDisplayStatus(thread, reviewStates[thread.id])}
              onToggleCollapsed={() => onToggleThread(thread.id)}
              onPreview={() => onPreview(thread)}
              onOpen={() => onOpen(thread)}
              onDelete={() => onDelete(thread.id)}
              onRestore={() => onRestore(thread)}
              restoring={restoringThreadId === thread.id}
            />
          )) : (
            <div className="px-4 py-6 text-center text-caption text-muted-foreground">
              {query ? "没有匹配的无角色会话" : "暂无无角色会话"}
            </div>
          )}
        </div>
      ) : null}
    </section>
  );
}

export function SessionCardWall() {
  const [boardState, setBoardState] = useState<BoardState>(DEFAULT_BOARD_STATE);
  const boardStateRef = useRef<BoardState>(DEFAULT_BOARD_STATE);
  const [threads, setThreads] = useState<CodexThread[]>([]);
  const threadsRef = useRef<CodexThread[]>([]);
  const [activeProjectId, setActiveProjectId] = useState(DEFAULT_BOARD_STATE.projects[0]?.id ?? "");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [previewThread, setPreviewThread] = useState<CodexThread | null>(null);
  const [transcript, setTranscript] = useState<CodexThreadTranscript | null>(null);
  const [transcriptLoading, setTranscriptLoading] = useState(false);
  const [transcriptError, setTranscriptError] = useState("");
  const [messageDraft, setMessageDraft] = useState("");
  const [messageSending, setMessageSending] = useState(false);
  const [messageError, setMessageError] = useState("");
  const [clientFallbackThread, setClientFallbackThread] = useState("");
  const [clientReceipt, setClientReceipt] = useState<(ReturnType<typeof parseNativeDelivery> & {threadId: string; originalText: string}) | null>(null);
  const clientPending = Boolean(clientReceipt && ["submitted", "queued", "unknown"].includes(clientReceipt.state));
  const currentClientPending = clientPending && clientReceipt?.threadId === previewThread?.id;
  const [messageReceipt, setMessageReceipt] = useState<{threadId: string; turnId: string; messageId: string; text: string} | null>(null);
  const [messageStopping, setMessageStopping] = useState(false);
  const [restoringThreadId, setRestoringThreadId] = useState("");
  const [codexModels, setCodexModels] = useState<CodexModel[]>([]);
  const [modelsLoading, setModelsLoading] = useState(true);
  const [modelsError, setModelsError] = useState("");
  const [selectedModel, setSelectedModel] = useState("");
  const [selectedReasoningEffort, setSelectedReasoningEffort] = useState("");
  const initializedModelThreadRef = useRef("");
  const [pendingUserMessages, setPendingUserMessages] = useState<CodexTranscriptItem[]>([]);
  const transcriptSignatureRef = useRef("");
  const pendingMessageThreadRef = useRef("");
  const transcriptScrollRef = useRef<HTMLDivElement>(null);
  const followTranscriptRef = useRef(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftAgents, setDraftAgents] = useState("");
  const deferredQuery = useDeferredValue(query.trim().toLocaleLowerCase());
  const board = buildProjectBoard(threads, boardState, activeProjectId);
  const roleCardCount = board.rows.reduce((sum, row) => sum + row.threads.length, 0);
  const projectSessionCount = roleCardCount + board.unassignedThreads.length;
  const previewCatalogThread = previewThread
    ? threads.find((thread) => thread.id === previewThread.id) || previewThread
    : null;
  const previewStatus = previewCatalogThread
    ? threadDisplayStatus(
        {
          ...previewCatalogThread,
          status: effectiveThreadStatus(previewCatalogThread.status, transcript?.status),
        },
        boardState.threadReviews[previewCatalogThread.id],
      )
    : "";
  const transcriptItemIDs = new Set((transcript?.items || []).flatMap((item) => [item.id, item.client_id].filter(Boolean)));
  const displayedTranscriptItems = [
    ...(transcript?.items || []),
    ...pendingUserMessages.filter((item) => !transcriptItemIDs.has(item.id)),
  ];
  const selectedModelDetails = codexModels.find((model) => model.id === selectedModel);
  const modelItems = codexModels.map((model) => ({ value: model.id, label: model.name }));
  const reasoningItems = (selectedModelDetails?.reasoning_efforts || []).map((effort) => ({
    value: effort.value,
    label: reasoningLabel(effort.value),
  }));

  function persist(next: BoardState) {
    const normalized = normalizeBoardState(next);
    boardStateRef.current = normalized;
    setBoardState(normalized);
    localStorage.setItem(BOARD_STORAGE_KEY, JSON.stringify(normalized));
  }

  async function refreshThreads(stateOverride?: BoardState) {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(THREAD_CATALOG_URL, { cache: "no-store" });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      const payload = (await response.json()) as {
        threads?: CodexThread[];
        archived_threads?: CodexThread[];
        projects?: CodexProject[];
      };
      const nextThreads = Array.isArray(payload.threads) ? payload.threads : [];
      const archivedThreads = Array.isArray(payload.archived_threads) ? payload.archived_threads : [];
      const nextProjects = Array.isArray(payload.projects) ? payload.projects : [];
      const remembered = rememberCatalogThreads(
        [...nextThreads, ...archivedThreads],
        stateOverride || boardStateRef.current,
      );
      const reconciled = reconcileThreadReviews(nextThreads, remembered);
      const merged = mergeDetectedProjects(nextThreads, reconciled, nextProjects);
      threadsRef.current = nextThreads;
      setThreads(nextThreads);
      persist(merged);
      setActiveProjectId((currentProjectId) =>
        merged.projects.some((project) => project.id === currentProjectId)
          ? currentProjectId
          : merged.projects[0]?.id || "",
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }

  async function refreshThreadStatuses() {
    try {
      const params = new URLSearchParams();
      for (const thread of threadsRef.current) params.append("thread_id", thread.id);
      const query = params.toString();
      const statusURL = query ? `${THREAD_STATUS_URL}?${query}` : THREAD_STATUS_URL;
      const response = await fetch(statusURL, { cache: "no-store" });
      if (!response.ok) return;
      const payload = (await response.json()) as {
        statuses?: Record<string, string>;
        archived_thread_ids?: string[];
      };
      const statuses = payload.statuses;
      if (!statuses || typeof statuses !== "object") return;
      const archivedThreadIDs = Array.isArray(payload.archived_thread_ids)
        ? payload.archived_thread_ids
        : [];
      if (
        hasThreadStatusChange(threadsRef.current, statuses)
        || hasArchivedThread(threadsRef.current, archivedThreadIDs)
      ) await refreshThreads();
    } catch {
      // The catalog remains usable when the optional live status probe is unavailable.
    }
  }

  async function loadCodexModels() {
    setModelsLoading(true);
    try {
      const response = await fetch(CODEX_MODELS_URL, { cache: "no-store" });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      const payload = (await response.json()) as { models?: CodexModel[] };
      setCodexModels(Array.isArray(payload.models) ? payload.models : []);
      setModelsError("");
    } catch (cause) {
      setModelsError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setModelsLoading(false);
    }
  }

  useEffect(() => {
    let initialState = DEFAULT_BOARD_STATE;
    const saved = localStorage.getItem(BOARD_STORAGE_KEY);
    if (saved) {
      try {
        const normalized = normalizeBoardState(JSON.parse(saved));
        initialState = normalized;
        boardStateRef.current = normalized;
        setBoardState(normalized);
      } catch {
        localStorage.removeItem(BOARD_STORAGE_KEY);
      }
    }
    void refreshThreads(initialState);
    void refreshThreadStatuses();
    void loadCodexModels();
    const statusTimer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refreshThreadStatuses();
    }, THREAD_STATUS_POLL_MS);
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") void refreshThreadStatuses();
    };
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      window.clearInterval(statusTimer);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, []);

  useEffect(() => {
    if (!previewThread || codexModels.length === 0) {
      initializedModelThreadRef.current = "";
      setSelectedModel("");
      setSelectedReasoningEffort("");
      return;
    }
    if (initializedModelThreadRef.current === previewThread.id && codexModels.some((model) => model.id === selectedModel)) return;
    initializedModelThreadRef.current = previewThread.id;
    const model = codexModels.find((candidate) => candidate.id === previewThread.model) || codexModels[0];
    setSelectedModel(model?.id || "");
    setSelectedReasoningEffort(
      model?.reasoning_efforts.some((effort) => effort.value === previewThread.reasoning_effort)
        ? previewThread.reasoning_effort || ""
        : model?.default_reasoning_effort || model?.reasoning_efforts[0]?.value || "",
    );
  }, [previewThread, codexModels, selectedModel]);

  useEffect(() => {
    if (!previewThread) return;
    const thread = previewThread;
    if (pendingMessageThreadRef.current !== thread.id) {
      pendingMessageThreadRef.current = thread.id;
      setPendingUserMessages([]);
    }
    let cancelled = false;
    let timer = 0;
    transcriptSignatureRef.current = "";
    followTranscriptRef.current = true;
    setTranscript(null);
    setTranscriptError("");
    setMessageError("");
    setTranscriptLoading(true);

    async function loadTranscript() {
      try {
        const response = await fetch(`${THREAD_TRANSCRIPT_URL}?thread_id=${encodeURIComponent(thread.id)}`, {
          cache: "no-store",
        });
        if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
        const payload = (await response.json()) as CodexThreadTranscript;
        const items = Array.isArray(payload.items) ? payload.items : [];
        const next = { ...payload, items };
        const last = items.at(-1);
        const signature = `${next.status || ""}:${items.length}:${last?.id || ""}:${last?.text?.length || 0}:${last?.status || ""}`;
        if (!cancelled && signature !== transcriptSignatureRef.current) {
          transcriptSignatureRef.current = signature;
          setTranscript(next);
          const persistedIDs = new Set(items.flatMap((item) => [item.id, item.client_id].filter(Boolean)));
          setPendingUserMessages((current) => current.filter((item) => !persistedIDs.has(item.id)));
        }
        if (!cancelled) {
          setMessageReceipt((receipt) => {
            if (!receipt || receipt.threadId !== thread.id || receipt.turnId !== next.turn_id) return receipt;
            const status = next.status;
            const text = status === "completed" ? "本条消息已处理完成"
              : status === "interrupted" ? "本轮已停止"
              : status === "failed" ? "本轮处理失败，请查看输出"
              : "AI 正在处理，输出持续更新中";
            return receipt.text === text ? receipt : {...receipt, text};
          });
          setTranscriptError("");
          setTranscriptLoading(false);
          timer = window.setTimeout(loadTranscript, threadCardStatus(next.status) === "active" ? THREAD_TRANSCRIPT_POLL_MS : 2000);
        }
      } catch (cause) {
        if (cancelled) return;
        setTranscriptLoading(false);
        setTranscriptError(cause instanceof Error ? cause.message : String(cause));
        timer = window.setTimeout(loadTranscript, 2000);
      }
    }

    void loadTranscript();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [previewThread]);

  useEffect(() => {
    if (!transcript || !followTranscriptRef.current) return;
    const frame = window.requestAnimationFrame(() => {
      const element = transcriptScrollRef.current;
      if (element) element.scrollTop = element.scrollHeight;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [transcript]);

  function hideSession(threadId: string) {
    if (!board.project) return;
    const previous = boardState;
    persist({
      ...boardState,
      hiddenThreads: { ...boardState.hiddenThreads, [threadId]: board.project.id },
    });
    toast("会话已从项目看板隐藏", {
      action: { label: "撤销", onClick: () => persist(previous) },
    });
  }

  function toggleThreadCollapsed(threadId: string) {
    if (!board.project) return;
    const collapsedThreads = { ...boardState.collapsedThreads };
    if (collapsedThreads[threadId] === board.project.id) {
      delete collapsedThreads[threadId];
    } else {
      collapsedThreads[threadId] = board.project.id;
    }
    persist({ ...boardState, collapsedThreads });
  }

  function toggleRowCollapsed(rowId: string) {
    if (!board.project) return;
    const key = rowStateKey(board.project.id, rowId);
    const collapsedRows = { ...boardState.collapsedRows };
    if (collapsedRows[key]) {
      delete collapsedRows[key];
    } else {
      collapsedRows[key] = true;
    }
    persist({ ...boardState, collapsedRows });
  }

  function markThreadReviewed(thread: CodexThread) {
    const current = threadsRef.current.find((candidate) => candidate.id === thread.id) || thread;
    persist({
      ...boardStateRef.current,
      threadReviews: {
        ...boardStateRef.current.threadReviews,
        [thread.id]: {
          state: "reviewed",
          activityAt: Math.max(activityTime(current), Math.floor(Date.now() / 1000)),
        },
      },
    });
    toast("会话已标记为已审阅");
    pendingMessageThreadRef.current = "";
    setPendingUserMessages([]);
    setPreviewThread(null);
  }

  useEffect(() => {
    if (!clientReceipt?.message_id) return;
    const receipt = clientReceipt;
    let cancelled = false;
    let timer = 0;
    async function poll() {
      try {
        const params = new URLSearchParams({action: "client-status", thread_id: receipt.threadId, message_id: receipt.message_id!});
        const response = await fetch(`${THREAD_SEND_URL}?${params}`, {cache: "no-store"});
        if (!response.ok) throw new Error("status unavailable");
        const result = parseNativeDelivery(await response.json());
        if (cancelled) return;
        setClientReceipt({...receipt, ...result, error: result.error});
        if (result.native_message_id && pendingMessageThreadRef.current === receipt.threadId) {
          setMessageDraft((draft) => draft.trim() === receipt.originalText ? "" : draft);
        }
        if (["completed", "interrupted", "failed", "unknown"].includes(result.state)) return;
      } catch {
        if (!cancelled) setClientReceipt({...receipt, state: "unknown", error: "客户端接收状态暂时无法读取，请先检查客户端，勿重复发送"});
        return;
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), 1500);
    }
    void poll();
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [clientReceipt?.message_id]);

  async function sendViaClient() {
    const thread = previewThread;
    const message = messageDraft.trim();
    if (!thread || thread.id !== clientFallbackThread || !message || messageSending || clientPending) return;
    setMessageSending(true);
    setMessageError("");
    // Keep the draft and lock retries if the HTTP response itself is lost.
    setMessageReceipt(null);
    setClientReceipt({threadId: thread.id, originalText: message, state: "unknown"});
    try {
      const response = await fetch(`${THREAD_SEND_URL}?action=client-send&thread_id=${encodeURIComponent(thread.id)}`, {
        method: "POST", headers: {"Content-Type": "text/plain;charset=UTF-8"}, body: message,
      });
      if (!response.ok) {
        const error = await response.text();
        throw new Error(error || `HTTP ${response.status}`);
      }
      const result = parseNativeDelivery(await response.json());
      if (!result.message_id) throw new Error("客户端未返回接收编号，请检查客户端后再重试");
      setClientReceipt({...result, threadId: thread.id, originalText: message});
      if (result.native_message_id) setMessageDraft((draft) => draft.trim() === message ? "" : draft);
    } catch (cause) {
      setMessageError(cause instanceof Error ? cause.message : String(cause));
    } finally { setMessageSending(false); }
  }

  async function sendMessageToThread() {
    const thread = previewThread;
    const message = messageDraft.trim();
    if (!thread || !message || messageSending || currentClientPending) return;
    setMessageSending(true);
    setMessageError("");
    try {
      const params = new URLSearchParams({ thread_id: thread.id });
      if (selectedModel) params.set("model", selectedModel);
      if (selectedReasoningEffort) params.set("reasoning_effort", selectedReasoningEffort);
      const response = await fetch(`${THREAD_SEND_URL}?${params.toString()}`, {
        method: "POST",
        headers: { "Content-Type": "text/plain;charset=UTF-8" },
        body: message,
      });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      const result = (await response.json()) as {
        message_id?: string;
        turn_id?: string;
        state?: "started" | "queued";
      };
      if (!result.message_id || !["started", "queued"].includes(result.state || "")) {
        throw new Error("Codex 没有确认收到这条消息");
      }
      if (result.state === "started" && !result.turn_id) {
        throw new Error("Codex 没有启动这条消息");
      }
      const messageID = result.message_id;
      setPendingUserMessages((current) => [
        ...current,
        { id: messageID, kind: "user", text: message },
      ]);
      setMessageDraft("");
      if (result.state === "started") {
        setMessageReceipt({threadId: thread.id, turnId: result.turn_id!, messageId: messageID, text: "Codex 已接收并启动本轮，等待 AI 输出…"});
        const nextThread = { ...thread, status: "active" };
        const nextThreads = threadsRef.current.map((candidate) => candidate.id === thread.id ? nextThread : candidate);
        threadsRef.current = nextThreads;
        setThreads(nextThreads);
        setPreviewThread(nextThread);
        toast.success("Codex 已收到消息并开始处理");
      } else if (result.state === "queued") {
        setMessageReceipt({threadId:thread.id,turnId:"",messageId:messageID,text:"Codex 已确认排队，尚未开始处理"});
        toast("消息已排队，将在当前任务结束后处理");
      }
    } catch (cause) {
      const error = cause instanceof Error ? cause.message : String(cause);
      if (error.includes("占用了该任务的连接") || error.includes("already has an active writer")) setClientFallbackThread(thread.id);
      setMessageError(error);
    } finally {
      setMessageSending(false);
    }
  }

  async function stopThreadMessage() {
    if (!previewThread || messageStopping) return;
    const threadId = previewThread.id;
    setMessageStopping(true);
    setMessageError("");
    try {
      const response = await fetch(`${THREAD_SEND_URL}?action=stop&thread_id=${encodeURIComponent(threadId)}`, {method: "POST"});
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      setMessageReceipt((receipt) => receipt?.threadId === threadId ? {...receipt, text: "Codex 已接受停止请求，等待本轮结束"} : receipt);
      toast("停止请求已确认");
    } catch (cause) {
      setMessageError(cause instanceof Error ? cause.message : String(cause));
    } finally { setMessageStopping(false); }
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

  async function restoreThread(thread: CodexThread) {
    if (restoringThreadId) return;
    setRestoringThreadId(thread.id);
    try {
      const response = await fetch(
        `${THREAD_RESTORE_URL}?thread_id=${encodeURIComponent(thread.id)}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      await refreshThreads();
      if (threadsRef.current.some((candidate) => candidate.id === thread.id)) {
        toast.success("Codex 任务已恢复");
      } else {
        toast.warning("任务已取消归档，但原工作树可能已被清理；卡片会继续保留");
      }
    } catch (cause) {
      toast.error("无法恢复 Codex 任务", {
        description: cause instanceof Error ? cause.message : String(cause),
      });
    } finally {
      setRestoringThreadId("");
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
    persist({
      version: 1,
      projects,
      assignments,
      hiddenThreads: boardState.hiddenThreads,
      collapsedThreads: boardState.collapsedThreads,
      collapsedRows: boardState.collapsedRows,
      threadReviews: boardState.threadReviews,
      threadSnapshots: boardState.threadSnapshots,
    });
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
          项目由 Codex 工作目录识别。会话标题中 “ · ” 前的角色决定 Agent 行。
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
            <span className="rounded-full bg-muted px-2 py-1">{projectSessionCount} 个项目会话</span>
            <span className="rounded-full bg-muted px-2 py-1">{roleCardCount} 张角色卡片</span>
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
              query={deferredQuery}
              collapsed={Boolean(board.project && boardState.collapsedRows[rowStateKey(board.project.id, row.id)])}
              collapsedThreads={boardState.collapsedThreads}
              reviewStates={boardState.threadReviews}
              onToggleCollapsed={() => toggleRowCollapsed(row.id)}
              onToggleThread={toggleThreadCollapsed}
              onPreview={setPreviewThread}
              onOpen={(thread) => void openThread(thread)}
              onDelete={hideSession}
              onRestore={(thread) => void restoreThread(thread)}
              restoringThreadId={restoringThreadId}
            />
          ))}
          <UnassignedSection
            threads={board.unassignedThreads}
            query={deferredQuery}
            collapsed={Boolean(board.project && boardState.collapsedRows[rowStateKey(board.project.id, UNASSIGNED_ROW_ID)])}
            collapsedThreads={boardState.collapsedThreads}
            reviewStates={boardState.threadReviews}
            onToggleCollapsed={() => toggleRowCollapsed(UNASSIGNED_ROW_ID)}
            onToggleThread={toggleThreadCollapsed}
            onPreview={setPreviewThread}
            onOpen={(thread) => void openThread(thread)}
            onDelete={hideSession}
            onRestore={(thread) => void restoreThread(thread)}
            restoringThreadId={restoringThreadId}
          />
        </div>
      </main>

      <Dialog
        open={Boolean(previewThread)}
        onOpenChange={(open) => {
          if (!open) {
            pendingMessageThreadRef.current = "";
            setPendingUserMessages([]);
            setPreviewThread(null);
          }
        }}
      >
        <DialogContent className="flex h-[80dvh] w-[min(92vw,960px)] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl">
          <DialogHeader className="border-b border-surface-border px-5 py-4 pr-14">
            <div className="flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <DialogTitle className="truncate">
                  {previewThread?.name || previewThread?.preview || "未命名会话"}
                </DialogTitle>
                <DialogDescription className="mt-1 truncate text-caption">
                  {previewThread?.cwd || "本地 Codex 会话"}
                </DialogDescription>
              </div>
              <ThreadStatusBadge
                status={previewStatus}
                compact={false}
                inline
              />
            </div>
          </DialogHeader>

          <div
            ref={transcriptScrollRef}
            onScroll={(event) => {
              const element = event.currentTarget;
              followTranscriptRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < 96;
            }}
            className="min-h-0 flex-1 overflow-y-auto bg-background/55 p-4"
            aria-live="polite"
          >
            {transcriptLoading && !transcript && displayedTranscriptItems.length === 0 ? (
              <div className="grid h-full min-h-64 place-items-center text-caption text-muted-foreground">
                <span className="flex items-center gap-2"><Loader2 className="size-4 animate-spin" />正在读取会话</span>
              </div>
            ) : transcriptError && !transcript && displayedTranscriptItems.length === 0 ? (
              <div className="rounded-lg border border-destructive/25 bg-destructive/6 p-3 text-caption text-destructive">
                {transcriptError}
              </div>
            ) : displayedTranscriptItems.length ? (
              <div className="space-y-3">
                {displayedTranscriptItems.map((item) => <TranscriptItem key={item.id} item={item} />)}
              </div>
            ) : (
              <div className="grid h-full min-h-64 place-items-center text-caption text-muted-foreground">暂无可显示的对话内容</div>
            )}
          </div>

          <form
            className="border-t border-surface-border bg-surface-raised px-5 py-3"
            onSubmit={(event) => {
              event.preventDefault();
              void sendMessageToThread();
            }}
          >
            <div className="mb-2 flex flex-wrap items-center gap-2">
              {modelsLoading ? (
                <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
                  <Loader2 className="size-3.5 animate-spin" /> 正在读取 Codex 模型
                </span>
              ) : codexModels.length ? (
                <>
                  <span className="flex items-center gap-1 text-caption text-muted-foreground">
                    <Bot className="size-3.5" /> 模型
                  </span>
                  <Select
                    items={modelItems}
                    value={selectedModel}
                    onValueChange={(value) => {
                      if (!value) return;
                      const model = codexModels.find((candidate) => candidate.id === value);
                      setSelectedModel(value);
                      if (!model) return;
                      const effort = model.reasoning_efforts.some(
                        (candidate) => candidate.value === selectedReasoningEffort,
                      )
                        ? selectedReasoningEffort
                        : model.default_reasoning_effort || model.reasoning_efforts[0]?.value || "";
                      setSelectedReasoningEffort(effort);
                    }}
                    disabled={messageSending}
                  >
                    <SelectTrigger
                      size="sm"
                      className="w-48 max-w-[45vw]"
                      aria-label="选择 Codex 模型"
                      title={selectedModelDetails?.description}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent align="start">
                      {codexModels.map((model) => (
                        <SelectItem key={model.id} value={model.id} title={model.description}>
                          {model.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  <span className="ml-1 flex items-center gap-1 text-caption text-muted-foreground">
                    <Brain className="size-3.5" /> 推理
                  </span>
                  <Select
                    items={reasoningItems}
                    value={selectedReasoningEffort}
                    onValueChange={(value) => value && setSelectedReasoningEffort(value)}
                    disabled={messageSending || reasoningItems.length === 0}
                  >
                    <SelectTrigger size="sm" className="w-24" aria-label="选择推理程度">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent align="start">
                      {(selectedModelDetails?.reasoning_efforts || []).map((effort) => (
                        <SelectItem key={effort.value} value={effort.value} title={effort.description}>
                          {reasoningLabel(effort.value)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </>
              ) : (
                <span className="text-caption text-muted-foreground" title={modelsError}>
                  未读取到本地 Codex 模型，将沿用任务当前设置
                </span>
              )}
            </div>
            <div className="flex items-end gap-2">
              <Textarea
                aria-label="发送消息到 Codex"
                value={messageDraft}
                onChange={(event) => setMessageDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    event.currentTarget.form?.requestSubmit();
                  }
                }}
                disabled={messageSending}
                className="min-h-20 max-h-40 resize-y"
                placeholder="直接给这个 Codex 任务发送消息…"
              />
              <Button
                type="submit"
                variant="brand"
                disabled={!messageDraft.trim() || messageSending || currentClientPending}
              >
                {messageSending ? <Loader2 className="animate-spin" /> : <SendHorizontal />}
                {messageSending ? "发送中" : "发送"}
              </Button>
              <Button type="button" variant="outline" disabled={messageStopping || messageSending} onClick={() => void stopThreadMessage()}>
                {messageStopping ? "停止中…" : "停止"}
              </Button>
            </div>
            <p role="status" aria-live="polite" className="mt-2 text-caption text-muted-foreground">
              {messageSending ? "正在发送，等待 Codex 确认接收…" : messageReceipt?.threadId === previewThread?.id ? messageReceipt?.text : ""}
              {transcriptError ? " 输出连接暂时中断，正在重试；当前处理状态尚未确认。" : ""}
            </p>
            {messageError ? <p className="mt-2 text-caption text-destructive">{messageError}</p> : null}
            {clientFallbackThread === previewThread?.id ? (
              <div className="mt-2 space-y-2">
                <Button type="button" variant="outline" disabled={messageSending || clientPending || !messageDraft.trim()} onClick={() => void sendViaClient()}>
                  交给客户端发送
                </Button>
                <p className="text-caption text-muted-foreground">通过现有 Codex 客户端提交，不抢占连接；会切换客户端当前任务，并沿用客户端模型与推理设置（不使用上方网页选择）。</p>
              </div>
            ) : null}
            {clientReceipt && clientReceipt.threadId === previewThread?.id ? (
              <div role="status" className="mt-2 text-caption text-muted-foreground">
                {clientReceipt.error || nativeDeliveryText(clientReceipt.state)}
                {clientReceipt.state === "unknown" && !messageSending ? (
                  <Button type="button" variant="ghost" onClick={() => setClientReceipt(null)}>我已检查客户端，解除发送锁定</Button>
                ) : null}
              </div>
            ) : null}
            <p className="mt-2 text-micro text-muted-foreground">
              {previewStatus === "active"
                ? "当前任务正在处理，新消息会加入队列。"
                : "Enter 发送，Shift+Enter 换行；所选模型与推理程度用于下一轮。"}
            </p>
          </form>

          <div className="flex items-center justify-between border-t border-surface-border bg-surface-hover/70 px-5 py-3">
            <span className="text-micro text-muted-foreground">最近 20 轮；进行中时约每半秒更新</span>
            <div className="flex items-center gap-2">
              {previewThread && previewStatus === "pending" ? (
                <Button variant="brand" onClick={() => markThreadReviewed(previewThread)}>
                  <BadgeCheck /> 标记为已审阅
                </Button>
              ) : null}
              <Button variant="outline" onClick={() => previewThread && void openThread(previewThread)}>
                <ArrowUpRight /> 在 Codex 中打开
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>项目名称与 Agent 行</DialogTitle>
            <DialogDescription>项目目录由 Codex 自动识别。会话标题以 “角色 · 功能” 命名时，会自动进入对应 Agent 行；无法匹配的会话进入底部列表。</DialogDescription>
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
              placeholder={"Router Agent\nDomain Agent\nMethods Agent\nImplementation Agent\nVisualization Agent\nWriting Agent\nReviewer Agent"}
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
