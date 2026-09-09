// @vitest-environment node
import {describe,it,expect} from "vitest";
import {parseNativeDelivery,nativeDeliveryText} from "./native-delivery";
describe("native message receipts",()=>{
  it("does not equate submission with execution",()=>{
    expect(parseNativeDelivery({message_id:"local",state:"submitted"}).turn_id).toBeUndefined();
    expect(nativeDeliveryText("submitted")).toContain("等待正式接收");
    expect(()=>parseNativeDelivery({state:"started"})).toThrow();
  });
  it("keeps native identity for transcript deduplication",()=>{
    expect(parseNativeDelivery({message_id:"local",native_message_id:"native",turn_id:"turn",state:"started"}).native_message_id).toBe("native");
    expect(()=>parseNativeDelivery({state:"made-up"})).toThrow();
  });
});
