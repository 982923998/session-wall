import {z} from "zod";

const receipt = z.object({
  message_id: z.string().optional(), native_message_id: z.string().optional(),
  turn_id: z.string().optional(), error: z.string().optional(),
  state: z.enum(["submitted", "queued", "started", "completed", "interrupted", "failed", "unknown"]),
}).refine(value => value.state !== "started" || Boolean(value.turn_id), "缺少正式运行轮次 ID");

export function parseNativeDelivery(value: unknown) { return receipt.parse(value); }
export function nativeDeliveryText(state: string) {
  switch(state) {
    case "started": return "客户端已开始处理，等待 AI 输出";
    case "queued": return "已交给客户端排队，尚未开始处理";
    case "completed": return "本条消息已处理完成";
    case "interrupted": return "本轮已停止";
    case "failed": return "客户端报告处理失败，请查看输出";
    case "unknown": return "提交状态尚未确认，请先检查客户端，勿重复发送";
    default: return "已提交客户端，正在等待正式接收记录";
  }
}
