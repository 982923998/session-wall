import type {CodexThreadTranscript} from "./model";

export interface MessageTiming {
  threadId: string;
  text: string;
  beforeIDs: string[];
  clickedAt: number;
  acknowledgedAt?: number;
  startedAt?: number;
  firstOutputAt?: number;
  failed?: boolean;
}

export function observeMessageTiming(timing: MessageTiming, transcript: CodexThreadTranscript): MessageTiming {
  if (timing.threadId !== transcript.thread_id) return timing;
  const matches = transcript.items.filter(item => item.kind === "user" &&
    !timing.beforeIDs.includes(item.id) && item.text?.trim() === timing.text &&
    (item.created_at_ms || 0) >= timing.clickedAt &&
    (item.started_at_ms || 0) >= timing.clickedAt - 1000 && item.turn_id);
  // Do not attribute a different task or ambiguous repeated input to this send.
  if (matches.length !== 1) return timing;
  const user = matches[0]!;
  const output = transcript.items.find(item => item.turn_id === user.turn_id &&
    item.kind === "assistant" && item.text && (item.created_at_ms || 0) >= (user.started_at_ms || 0));
  const startedAt = user.started_at_ms;
  const firstOutputAt = output?.created_at_ms;
  if (timing.startedAt === startedAt && timing.firstOutputAt === firstOutputAt) return timing;
  return {...timing, startedAt, firstOutputAt};
}

export function messageTimingText(timing: MessageTiming): string {
  const stamp = (at?: number) => at
    ? `${new Date(at).toLocaleTimeString("zh-CN", {hour12:false})}（+${Math.max(0,(at-timing.clickedAt)/1000).toFixed(1)}秒）`
    : timing.failed ? "未确认" : "等待记录";
  return `点击发送 ${new Date(timing.clickedAt).toLocaleTimeString("zh-CN", {hour12:false})} · 投递确认 ${stamp(timing.acknowledgedAt)} · 任务启动 ${stamp(timing.startedAt)} · 首条回复 ${stamp(timing.firstOutputAt)}`;
}
