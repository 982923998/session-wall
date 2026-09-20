// @vitest-environment node
import {it,expect} from "vitest";
import {observeMessageTiming, messageTimingText} from "./message-timing";
import type {CodexThreadTranscript} from "./model";

it("attributes timing to the new matching turn, not historical output",()=>{
  const timing={threadId:"t",text:"hello",beforeIDs:["old"],clickedAt:10000,acknowledgedAt:11000};
  const transcript:CodexThreadTranscript={thread_id:"t",items:[
    {id:"old",kind:"user",text:"hello",turn_id:"old",created_at_ms:1000,started_at_ms:1000},
    {id:"u",kind:"user",text:"hello",turn_id:"new",created_at_ms:13000,started_at_ms:12000},
    {id:"a",kind:"assistant",text:"working",turn_id:"new",created_at_ms:17000},
  ]};
  const result=observeMessageTiming(timing,transcript);
  expect(result.startedAt).toBe(12000);
  expect(result.firstOutputAt).toBe(17000);
  expect(messageTimingText(result)).toContain("+7.0秒");
  expect(observeMessageTiming(timing,{...transcript,thread_id:"other"})).toBe(timing);
  expect(observeMessageTiming(timing,{...transcript,items:[...transcript.items,{...transcript.items[1]!,id:"duplicate"}]})).toBe(timing);
});
