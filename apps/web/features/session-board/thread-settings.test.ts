// @vitest-environment node
import {it,expect} from "vitest";
import {parseThreadSettings,fastModeLabel} from "./thread-settings";

it("shows explicit FAST state without treating missing data as off",()=>{
  expect(fastModeLabel(true)).toBe("FAST 已开启");
  expect(fastModeLabel(false)).toBe("FAST 未开启");
  expect(fastModeLabel(parseThreadSettings({thread_id:"t"}).fast_mode)).toBe("FAST 状态未知");
  expect(()=>parseThreadSettings({thread_id:"t",fast_mode:"true"})).toThrow();
  expect(parseThreadSettings({thread_id:"t",model:"actual-model",reasoning_effort:"high",fast_mode:true}).model).toBe("actual-model");
});
