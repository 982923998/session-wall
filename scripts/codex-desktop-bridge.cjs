#!/opt/homebrew/bin/node
'use strict';

// Preserve the desktop's stdio connection; web clients use that same writer.
const { spawn, execFileSync } = require('node:child_process');
const readline = require('node:readline');
const { randomUUID } = require('node:crypto');
const WebSocket = require('../node_modules/ws');
const binary = '/Applications/ChatGPT.app/Contents/Resources/codex';
const args = process.argv.slice(2);
const serverIndex = args.indexOf('app-server');
function launchedFromDesktop() {
  let pid=process.ppid;
  for(let depth=0;pid>1&&depth<12;depth++) {
    try {
      const row=execFileSync('/bin/ps',['-p',String(pid),'-o','ppid=,comm='],{encoding:'utf8',timeout:2000}).trim();
      if(row.endsWith('/Applications/ChatGPT.app/Contents/MacOS/ChatGPT'))return true;
      pid=Number(row.split(/\s+/)[0]);
    }catch{return false;}
  }
  return false;
}
const isServer = serverIndex >= 0 && launchedFromDesktop() && !args.slice(serverIndex + 1).some(a =>
  ['generate-json-schema', 'generate-ts', 'daemon', 'proxy', 'help', '--help', '-h'].includes(a));

if (!isServer) {
  const child = spawn(binary, args, { stdio: 'inherit', env: { ...process.env, CODEX_CLI_PATH: binary } });
  child.on('exit', code => { process.exitCode = code ?? 1; });
  child.on('error', error => { console.error(error.message); process.exitCode = 1; });
} else {
  const allowed = new Set(['thread/read', 'thread/resume', 'thread/turns/list', 'thread/list',
    'thread/loaded/list', 'thread/unarchive', 'model/list', 'turn/start', 'turn/interrupt',
    'turn/steer', 'thread/queue/add', 'thread/queue/list', 'thread/queue/delete',
    'thread/queue/start', 'thread/queue/update', 'thread/queue/reorder', 'thread/settings/update']);
  const child = spawn(binary, args, { stdio: ['pipe', 'pipe', 'inherit'], env: { ...process.env, CODEX_CLI_PATH: binary } });
  const clients = new Set();
  const pending = new Map();
  const deliveries = new Map();
  const queues = new Map();
  const paused = new Set();
  const operations = new Map();
  const rpc = (method, params) => new Promise((resolve, reject) => {
    const id = 'session-wall-' + randomUUID();
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('Codex response not confirmed; do not resend automatically')); }, 15000);
    pending.set(id, { resolve: value => { clearTimeout(timer); resolve(value); }, reject: error => { clearTimeout(timer); reject(error); } });
    write({ id, method, params });
  });
  const serial = (threadId, action) => {
    const next = (operations.get(threadId) || Promise.resolve()).catch(() => {}).then(action);
    operations.set(threadId, next);
    next.finally(() => { if (operations.get(threadId) === next) operations.delete(threadId); }).catch(() => {});
    return next;
  };
  const active = async threadId => {
    const {thread} = await rpc('thread/read', {threadId, includeTurns:true});
    return (thread.turns || []).findLast(turn => turn.status === 'inProgress')?.id || null;
  };
  const stop = async threadId => {
    const turnId = await active(threadId);
    if (!turnId) return;
    await rpc('turn/interrupt', {threadId, turnId});
    for (let n=0;n<50;n++) {
      if (await active(threadId) !== turnId) return;
      await new Promise(resolve => setTimeout(resolve,100));
    }
    throw new Error('停止尚未完成，未发送新消息');
  };
  const start = async delivery => {
    const result = await rpc(delivery.submissionId ? 'thread/queue/start' : 'turn/start',
      delivery.submissionId ? {threadId:delivery.threadId,queuedSubmissionId:delivery.submissionId} :
        {threadId:delivery.threadId,clientUserMessageId:delivery.message_id,input:[{type:'text',text:delivery.message}],
          ...(delivery.model ? {model:delivery.model} : {}), ...(delivery.effort ? {effort:delivery.effort} : {})});
    delivery.turn_id=result.turn.id; delivery.state='started';
    return {message_id:delivery.message_id,turn_id:delivery.turn_id,state:delivery.state};
  };
  const drain = async threadId => {
    const queue=queues.get(threadId);
    if (!queue?.length || paused.has(threadId) || await active(threadId)) return;
    const delivery=queue[0];
    try {
      if (delivery.model || delivery.effort) await rpc('thread/settings/update', {threadId,
        ...(delivery.model ? {model:delivery.model} : {}), ...(delivery.effort ? {effort:delivery.effort} : {})});
      await start(delivery); queue.shift();
    } catch(error) { delivery.state='failed'; delivery.error=error.message; queue.shift(); }
  };
  const webAction = async (method, params) => {
    const threadId=params.threadId;
    if (!/^[0-9a-f-]{36}$/i.test(threadId || '')) throw new Error('Invalid thread ID');
    if(method==='sessionWall/messageStatus') {
      const d=deliveries.get(params.messageId);
      if(!d || d.threadId!==threadId) return {state:'unknown'};
      return {message_id:d.message_id,turn_id:d.turn_id,state:d.state,error:d.error};
    }
    return serial(threadId,async()=>{
      if(method==='sessionWall/stop') { paused.add(threadId); await stop(threadId); return {}; }
      const message=typeof params.message==='string'?params.message.trim():'';
      if(!message || Buffer.byteLength(message)>16*1024)throw new Error('Invalid message');
      await rpc('thread/resume',{threadId,excludeTurns:true});
      if(params.interrupt===true) await stop(threadId);
      paused.delete(threadId);
      const delivery={threadId,message,model:params.model,effort:params.effort,message_id:randomUUID()};
      deliveries.set(delivery.message_id,delivery);
      // Only remove terminal receipts; pending messages must remain traceable.
      if(deliveries.size>500)for(const [id,d] of deliveries){if(['completed','interrupted','failed'].includes(d.state)){deliveries.delete(id);if(deliveries.size<=500)break;}}
      if(await active(threadId) || (params.interrupt!==true && queues.get(threadId)?.length)) {
        const result=await rpc('thread/queue/add',{threadId,clientUserMessageId:delivery.message_id,input:[{type:'text',text:message}]});
        delivery.submissionId=result.queuedSubmission.id;delivery.state='queued';
        const queue=queues.get(threadId)||[];queue.push(delivery);queues.set(threadId,queue);
        return {message_id:delivery.message_id,state:'queued'};
      }
      try{return await start(delivery);}catch(error){delivery.state='failed';delivery.error=error.message;throw error;}
    });
  };
  const queueTimer=setInterval(()=>{for(const [threadId,queue] of queues)if(queue.length&&!operations.has(threadId))serial(threadId,()=>drain(threadId)).catch(()=>{});},1000);
  queueTimer.unref();
  let initializeID, initialized;
  const endpoint = new WebSocket.Server({ host: '127.0.0.1', port: 19515, maxPayload: 32 * 1024 * 1024,
    verifyClient: ({origin}) => !origin || /^http:\/\/(127\.0\.0\.1|localhost):3000$/.test(origin) });
  const write = message => child.stdin.write(JSON.stringify(message) + '\n');
  const reply = (ws, message) => { if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(message)); };
  endpoint.on('error', error => console.error('Session Wall bridge unavailable:', error.message));
  endpoint.on('connection', ws => {
    clients.add(ws);
    ws.on('close', () => {
      clients.delete(ws);
      for (const [id, request] of pending) if (request.ws === ws) pending.delete(id);
    });
    ws.on('message', data => {
      let message;
      try { message = JSON.parse(data); } catch { ws.close(1003); return; }
      if (message.method === 'initialized') return;
      if (message.method === 'initialize') {
        reply(ws, initialized ? { id: message.id, result: initialized } :
          { id: message.id, error: { code: -32000, message: 'Codex desktop is still initializing' } });
        return;
      }
      if (['sessionWall/send','sessionWall/stop','sessionWall/messageStatus'].includes(message.method)) {
        webAction(message.method,message.params||{}).then(result=>reply(ws,{id:message.id,result}),
          error=>reply(ws,{id:message.id,error:{code:-32000,message:error.message}}));
        return;
      }
      if (!allowed.has(message.method) || message.id == null) {
        reply(ws, { id: message.id ?? null, error: { code: -32601, message: 'Method not available through Session Wall' } });
        return;
      }
      const id = 'session-wall-' + randomUUID();
      pending.set(id, { ws, id: message.id });
      write({ ...message, id });
    });
  });
  readline.createInterface({ input: process.stdin }).on('line', line => {
    try { const message = JSON.parse(line); if (message.method === 'initialize') initializeID = message.id; } catch {}
    child.stdin.write(line + '\n');
  }).on('close', () => child.stdin.end());
  readline.createInterface({ input: child.stdout }).on('line', line => {
    let message;
    try { message = JSON.parse(line); } catch { process.stdout.write(line + '\n'); return; }
    if (message.id === initializeID && message.result) initialized = message.result;
    const request = pending.get(message.id);
    if (request) {
      pending.delete(message.id);
      if(request.resolve) message.error?request.reject(new Error(message.error.message)):request.resolve(message.result);
      else reply(request.ws, { ...message, id: request.id });
      return;
    }
    if (typeof message.id==='string'&&message.id.startsWith('session-wall-')) return;
    if (message.method==='turn/completed') {
      for(const d of deliveries.values())if(d.turn_id===message.params?.turn?.id){d.state=message.params.turn.status;d.error=message.params.turn.error?.message;}
    }
    // All model tool/approval requests remain owned by the desktop UI.
    process.stdout.write(line + '\n');
    if (message.id == null) for (const ws of clients) reply(ws, message);
  });
  child.on('exit', code => { for (const ws of clients) ws.terminate(); endpoint.close(); process.exit(code ?? 1); });
  child.on('error', error => { console.error(error.message); process.exit(1); });
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal));
}
