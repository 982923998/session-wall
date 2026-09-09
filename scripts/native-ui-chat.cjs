'use strict';

const {randomUUID, randomBytes, createHash, timingSafeEqual} = require('node:crypto');
const {runController, findNode, actionFor} = require('./native-ui-driver.cjs');
const {prepareDraft} = require('./native-ui-workflow.cjs');
const database = require('./native-ui-receipts.cjs');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const validID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const isEmpty = node => ['', '\n随心输入'].includes(node.value);
const editor = snapshot => findNode(snapshot, n => n.role === 'AXTextArea' && n.description === '随心输入');
const inComposer = (node, input) => input.path.slice(0, -1).every((part, i) => node.path?.[i] === part);

function createNativeChat(deps = {}) {
  const control = deps.runController || runController;
  const prepare = deps.prepareDraft || prepareDraft;
  const data = deps.database || database;
  const wait = deps.sleep || sleep;
  const receipts = new Map();
  let transactions = Promise.resolve();
  const serial = action => {
    const next = transactions.then(action);
    transactions = next.catch(() => {});
    return next;
  };
  const call = actions => control({preserveClipboard:true,actions:[{op:'activate'},...actions]});

  async function identity(threadId, navigate) {
    if (navigate) await call([{op:'openThread',threadId},{op:'activate'},{op:'wait',seconds:1}]);
    const snapshot = await call([{op:'key',key:37,modifiers:['command','option']},{op:'wait',seconds:0.2},{op:'readClipboard'}]);
    if (snapshot.results?.[0]?.text !== `codex://threads/${threadId}`) throw new Error('客户端当前任务不匹配，操作已取消');
    return snapshot;
  }
  async function refresh(receipt) {
    if (receipt.after == null) return receipt;
    const found = await data.findReceipt(receipt.thread_id,receipt.message,receipt.after);
    if (found) { Object.assign(receipt,found); delete receipt.error; }
    else if (Date.now()-receipt.created_at > 120000 && receipt.state === 'submitted') {
      receipt.state='unknown'; receipt.error='未找到正式接收记录，请查看客户端后决定是否重试';
    }
    return receipt;
  }
  function publicReceipt(receipt) {
    return {message_id:receipt.message_id,native_message_id:receipt.native_message_id,
      turn_id:receipt.turn_id,state:receipt.state,error:receipt.error};
  }
  async function stop(threadId) {
    const snapshot=await identity(threadId,true);
    const turn=await data.readLatestTurn(threadId);
    if (!turn || turn.state!=='started') return {stopped:false};
    const input=editor(snapshot);
    const stopButton=findNode(snapshot,n=>inComposer(n,input)&&n.role==='AXButton'&&n.description==='停止'&&n.enabled);
    if(snapshot.nodes.some(n=>n.role==='AXMenuItem'||n.role==='AXComboBox'))throw new Error('客户端有打开的菜单，请先关闭菜单再停止');
    await call([actionFor(stopButton,'press')]);
    for(let n=0;n<30;n++) {
      const current=await data.readLatestTurn(threadId);
      if(current?.turn_id===turn.turn_id && current.state==='interrupted')return {stopped:true,turn_id:turn.turn_id};
      if(current?.turn_id===turn.turn_id && current.state!=='started')throw new Error('当前任务已结束，无需停止');
      if(current?.turn_id!==turn.turn_id)throw new Error('原轮次已结束，但另一个轮次已经开始；未发送新消息');
      await wait(250);
    }
    throw new Error('停止尚未获得确认，请查看客户端状态');
  }
  async function send(params) {
    const {threadId,message}=params;
    if(typeof message!=='string'||!message.trim()||Buffer.byteLength(message)>16384)throw new Error('消息不能为空或超过长度限制');
    const settings=await data.readThreadSettings(threadId);
    if(!settings)throw new Error('客户端任务不存在；消息未发送');
    if ((params.model && settings?.model!==params.model) || (params.effort && settings?.reasoning_effort!==params.effort)) {
      throw new Error('网页选择与客户端当前模型或推理程度不同。请先在客户端切换到所选设置；本条消息未发送');
    }
    if(params.interrupt)await stop(threadId);
    await prepare(threadId,message);
    const snapshot=await identity(threadId,false);
    const input=editor(snapshot);
    if(input.value!==message.slice(0,2000))throw new Error('客户端草稿已变化，本条消息未提交');
    const button=findNode(snapshot,n=>inComposer(n,input)&&n.role==='AXButton'&&['发送','加入队列','Send','Queue'].includes(n.description)&&n.enabled);
    const receipt={thread_id:threadId,message,message_id:randomUUID(),created_at:Date.now(),after:await data.readBaseline(threadId),state:'submitted'};
    receipts.set(receipt.message_id,receipt);
    try {
      const after=await call([{op:'key',key:36}]);
      if(!isEmpty(editor(after))) {
        receipt.state='unknown';receipt.error='尚未确认客户端是否提交，文字可能仍在输入框中；请勿重复发送';
      } else if(['加入队列','Queue'].includes(button.description)) receipt.state='queued';
    } catch(error) { receipt.state='unknown';receipt.error=error.message; }
    try {
      for(let n=0;n<8&&!receipt.turn_id;n++) { await refresh(receipt);if(receipt.turn_id)break;await wait(250); }
    } catch {
      receipt.state='unknown';receipt.error='已尝试提交，但接收记录暂时不可读；请勿重复发送';
    }
    return publicReceipt(receipt);
  }
  return {
    async request(method,params) {
      if(!validID.test(params?.threadId||''))throw new Error('Invalid thread ID');
      params={...params,threadId:params.threadId.toLowerCase()};
      if(method==='sessionWall/messageStatus') {
        const receipt=receipts.get(params.messageId);
        if(!receipt||receipt.thread_id!==params.threadId)return {state:'unknown'};
        return publicReceipt(await refresh(receipt));
      }
      if(method==='sessionWall/send')return serial(()=>send(params));
      if(method==='sessionWall/stop')return serial(()=>stop(params.threadId));
      throw new Error('Unsupported UI operation');
    },
  };
}

if(require.main===module) {
  const WebSocket=require('../node_modules/ws');
  const {homedir}=require('node:os');
  const {join}=require('node:path');
  const fs=require('node:fs');
  const tokenPath=process.env.SESSION_WALL_UI_TOKEN_FILE||join(homedir(),'Library','Application Support','SessionWall','ui-control-token');
  fs.mkdirSync(require('node:path').dirname(tokenPath),{recursive:true,mode:0o700});
  try {fs.writeFileSync(tokenPath,randomBytes(32).toString('hex'),{flag:'wx',mode:0o600});} catch(e){if(e.code!=='EEXIST')throw e;}
  const info=fs.lstatSync(tokenPath);
  if(!info.isFile()||(info.mode&0o077))throw new Error('UI control token must be a private regular file');
  const token=fs.readFileSync(tokenPath,'utf8').trim();
  if(!/^[0-9a-f]{64}$/.test(token))throw new Error('Invalid UI control token');
  const digest=value=>createHash('sha256').update(value).digest();
  const expected=digest('Bearer '+token);
  const chat=createNativeChat();
  const server=new WebSocket.Server({host:'127.0.0.1',port:19515,maxPayload:128*1024,
    verifyClient:({origin,req})=>!origin&&timingSafeEqual(digest(req.headers.authorization||''),expected)});
  server.on('connection',socket=>socket.on('message',async raw=>{
    let request;
    try {
      request=JSON.parse(raw);
      if(request.method==='initialized')return;
      const result=request.method==='initialize'?{codexHome:process.env.CODEX_HOME||join(homedir(),'.codex')}:await chat.request(request.method,request.params||{});
      if(socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({id:request.id,result}));
    }catch(error){if(socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({id:request?.id??null,error:{code:-32000,message:error.message}}));}
  }));
  server.on('listening',()=>console.log('Session Wall native UI control listening on loopback port 19515'));
}
module.exports={createNativeChat};
