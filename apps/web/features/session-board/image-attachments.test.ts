// @vitest-environment node
import {it,expect} from "vitest";
import {validateImages,transferImageFiles,MAX_IMAGE_BYTES,transcriptImageURLs} from "./image-attachments";

it("accepts selected/pasted/dropped image files with bounded size and count",()=>{
  const image=new File(["image"],"test.png",{type:"image/png"});
  expect(validateImages([image],[])).toBeNull();
  expect(validateImages([image],Array(6).fill(image))).toContain("6");
  expect(validateImages([new File(["svg"],"test.svg",{type:"image/svg+xml"})],[])).toContain("支持");
  expect(validateImages([new File([new Uint8Array(MAX_IMAGE_BYTES+1)],"big.png",{type:"image/png"})],[])).toContain("8 MB");
  expect(transferImageFiles({files:[image],items:[]} as unknown as DataTransfer)).toEqual([image]);
  expect(transferImageFiles({files:[],items:[{kind:"file",getAsFile:()=>image}]} as unknown as DataTransfer)).toEqual([image]);
});

it("does not render malformed or external image URLs from a response",()=>{
  expect(transcriptImageURLs("not-an-array")).toEqual([]);
  expect(transcriptImageURLs(["https://external.example/image.png"])).toEqual([]);
  const owned="http://127.0.0.1:19514/codex/attachments/01a08089-fb23-7d42-bfe5-89a968d8a56a/1.png";
  expect(transcriptImageURLs([owned])).toEqual([owned]);
});
