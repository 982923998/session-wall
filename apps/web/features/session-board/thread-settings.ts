import {z} from "zod";

const settingsSchema=z.object({
  thread_id:z.string(),
  model:z.string().default(""),
  reasoning_effort:z.string().default(""),
  service_tier:z.string().nullable().optional(),
  fast_mode:z.boolean().nullable().optional(),
});

export function parseThreadSettings(value:unknown){return settingsSchema.parse(value);}
export function fastModeLabel(value:boolean|null|undefined){
  return value===true?"FAST 已开启":value===false?"FAST 未开启":"FAST 状态未知";
}
