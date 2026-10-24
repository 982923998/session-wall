import {test,expect} from "@playwright/test";

const id="01a08089-fb23-7d42-bfe5-89a968d8a56a";
const other="01a08089-fb23-7d42-bfe5-89a968d8a56b";
const png=Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZ0AAAAASUVORK5CYII=","base64");

test("image selection, drop and paste preserve text sending and failed drafts",async({page})=>{
  const requests:{type:string;body:Buffer|null}[]=[];
  let fail=false;
  await page.route("http://127.0.0.1:19514/**",async route=>{
    const request=route.request();
    const path=new URL(request.url()).pathname;
    let payload:unknown={};
    if(path==="/codex/threads")payload={projects:[{id:"images",name:"Image test",roots:["/ui-image-test"],position:0}],threads:[
      {id,name:"writing · upload test",cwd:"/ui-image-test",project_root:"/ui-image-test"},
      {id:other,name:"writing · other test",cwd:"/ui-image-test",project_root:"/ui-image-test"},
    ]};
    if(path==="/codex/models")payload={models:[]};
    if(path==="/codex/thread-statuses")payload={statuses:{},archived_thread_ids:[]};
    if(path==="/codex/thread-transcript")payload={thread_id:id,status:"completed",items:[]};
    if(path==="/codex/thread-settings")payload={thread_id:id,model:"test-model",reasoning_effort:"high",fast_mode:false};
    if(path==="/codex/send-message"){
      requests.push({type:request.headers()["content-type"]||"",body:request.postDataBuffer()});
      if(fail){await route.fulfill({status:503,body:"test delivery failure",headers:{"access-control-allow-origin":"http://127.0.0.1:3000"}});return;}
      payload={message_id:"test-receipt",state:"queued",delivery:"client_queue"};
    }
    await route.fulfill({json:payload,headers:{"access-control-allow-origin":"http://127.0.0.1:3000"}});
  });
  await page.goto("http://127.0.0.1:3000/session-board");
  await page.getByRole("button",{name:"writing · upload test",exact:true}).click();
  const dialog=page.getByRole("dialog");
  const input=dialog.locator('input[type="file"]');
  const textarea=dialog.getByRole("textbox",{name:"发送消息到 Codex"});
  await input.setInputFiles({name:"selected.png",mimeType:"image/png",buffer:png});
  await expect(dialog.locator("img")).toHaveCount(1);
  for(const [event,name]of [["paste","paste.png"],["drop","drop.png"]]){
    const transfer=await page.evaluateHandle(({bytes,name})=>{
      const transfer=new DataTransfer();
      transfer.items.add(new File([new Uint8Array(bytes)],name,{type:"image/png"}));
      return transfer;
    },{bytes:[...png],name});
    if(event==="paste"){
      await textarea.evaluate((element,clipboardData)=>element.dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData})),transfer);
    }else{
      await dialog.dispatchEvent("drop",{dataTransfer:transfer});
    }
    await transfer.dispose();
  }
  await expect(dialog.locator("img")).toHaveCount(3);
  await dialog.getByRole("button",{name:"移除图片 paste.png",exact:true}).click();
  await expect(dialog.locator("img")).toHaveCount(2);
  await textarea.fill("中文图片消息");
  await textarea.dispatchEvent("keydown",{key:"Enter",isComposing:true,keyCode:13});
  expect(requests).toHaveLength(0);
  await dialog.getByRole("button",{name:"发送",exact:true}).click();
  await expect(dialog.locator("img")).toHaveCount(0);
  expect(requests[0]?.type).toContain("multipart/form-data");
  expect(requests[0]?.body?.includes(png)).toBe(true);
  await textarea.fill("plain text");
  await textarea.press("Enter");
  await expect(textarea).toHaveValue("");
  expect(requests[1]?.type).toContain("text/plain");
  expect(requests[1]?.body?.toString()).toBe("plain text");
  fail=true;
  await input.setInputFiles({name:"keep.png",mimeType:"image/png",buffer:png});
  await textarea.fill("keep me");
  await dialog.getByRole("button",{name:"发送",exact:true}).click();
  await expect(dialog.getByText("test delivery failure",{exact:true})).toBeVisible();
  await expect(textarea).toHaveValue("keep me");
  await expect(dialog.locator("img")).toHaveCount(1);
  await page.keyboard.press("Escape");
  await page.getByRole("button",{name:"writing · other test",exact:true}).click();
  await expect(page.getByRole("dialog").locator("img")).toHaveCount(0);
});
