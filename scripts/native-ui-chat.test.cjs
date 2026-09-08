'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const {createNativeChat} = require('./native-ui-chat.cjs');

const threadId = '01a07e9e-6dcf-7533-b8de-de72986af5a5';
const message = '仅用于单元测试的消息';

function input(value = message) {
  return {
    path: [4, 0],
    role: 'AXTextArea',
    title: '',
    description: '随心输入',
    identifier: 'composer',
    value,
    enabled: true,
  };
}

function button(description) {
  return {
    path: [4, 1],
    role: 'AXButton',
    title: '',
    description,
    identifier: `${description}-button`,
    value: '',
    enabled: true,
  };
}

function snapshot({link = threadId, value = message, buttonDescription = '发送'} = {}) {
  return {
    results: [{text: `codex://threads/${link}`}],
    nodes: [input(value), button(buttonDescription)],
  };
}

function dependencies(overrides = {}) {
  return {
    runController: overrides.runController || (async () => snapshot()),
    prepareDraft: overrides.prepareDraft || (async () => ({state: 'draft', thread_id: threadId, message})),
    database: {
      readThreadSettings: async () => ({model: 'gpt-5.6-sol', reasoning_effort: 'high'}),
      readBaseline: async () => 7,
      findReceipt: async () => null,
      readLatestTurn: async () => null,
      ...overrides.database,
    },
    sleep: overrides.sleep || (async () => {}),
  };
}

function hasKey(request, key) {
  return request.actions.some(action => action.op === 'key' && action.key === key);
}

const hasSubmit = request => hasKey(request, 36);
const hasStop = request => request.actions.some(action => action.op === 'press' && action.description === '停止');

test('keeps a receipt and never retries submission when history readback fails',async()=>{
  let submissions=0;
  const chat=createNativeChat(dependencies({
    runController:async request=>{if(hasSubmit(request)){submissions++;return snapshot({value:''});}return snapshot();},
    database:{findReceipt:async()=>{throw new Error('temporary database error');}},
  }));
  const result=await chat.request('sessionWall/send',{threadId,message});
  assert.equal(result.state,'unknown');assert.ok(result.message_id);assert.equal(submissions,1);
});

test('does not submit when the current native thread identity differs', async () => {
  const controls = [];
  let prepared = 0;
  const chat = createNativeChat(dependencies({
    prepareDraft: async () => { prepared += 1; },
    runController: async request => {
      controls.push(request);
      return snapshot({link: '019f3ffd-7d62-79a0-adeb-b826e8b50152'});
    },
  }));

  await assert.rejects(chat.request('sessionWall/send', {threadId, message}), /当前任务不匹配/);
  assert.equal(prepared, 1);
  assert.equal(controls.some(hasSubmit), false);
});

test('does not prepare or submit when model settings differ', async () => {
  const controls = [];
  let prepared = 0;
  const chat = createNativeChat(dependencies({
    prepareDraft: async () => { prepared += 1; },
    runController: async request => { controls.push(request); return snapshot(); },
  }));

  await assert.rejects(chat.request('sessionWall/send', {
    threadId,
    message,
    model: 'gpt-6-astra',
    effort: 'high',
  }), /网页选择与客户端当前模型/);
  assert.equal(prepared, 0);
  assert.equal(controls.length, 0);
});

test('returns started and a turn ID only after a formal receipt is found', async () => {
  const controls = [];
  let receiptChecks = 0;
  const chat = createNativeChat(dependencies({
    runController: async request => {
      controls.push(request);
      return hasSubmit(request) ? snapshot({value: '\n随心输入'}) : snapshot();
    },
    database: {
      findReceipt: async () => {
        receiptChecks += 1;
        return {
          native_message_id: 'native-message-1',
          turn_id: 'turn-1',
          state: 'started',
        };
      },
    },
  }));

  const result = await chat.request('sessionWall/send', {threadId, message});
  assert.equal(controls.filter(hasSubmit).length, 1);
  assert.equal(receiptChecks, 1);
  assert.equal(result.state, 'started');
  assert.equal(result.turn_id, 'turn-1');
  assert.equal(result.native_message_id, 'native-message-1');
});

test('does not invent started or a turn ID without a formal receipt', async () => {
  let receiptChecks = 0;
  const chat = createNativeChat(dependencies({
    runController: async request =>
      hasSubmit(request) ? snapshot({value: ''}) : snapshot(),
    database: {
      findReceipt: async () => { receiptChecks += 1; return null; },
    },
  }));

  const result = await chat.request('sessionWall/send', {threadId, message});
  assert.equal(receiptChecks, 8);
  assert.equal(result.state, 'submitted');
  assert.equal(result.turn_id, undefined);
});

test('reports queued only when the queue button clears the input and no receipt exists', async () => {
  const controls = [];
  const chat = createNativeChat(dependencies({
    runController: async request => {
      controls.push(request);
      return hasSubmit(request)
        ? snapshot({value: '', buttonDescription: '加入队列'})
        : snapshot({buttonDescription: '加入队列'});
    },
  }));

  const result = await chat.request('sessionWall/send', {threadId, message});
  assert.equal(controls.filter(hasSubmit).length, 1);
  assert.equal(result.state, 'queued');
  assert.equal(result.turn_id, undefined);
});

function stopController(turnStates, controls) {
  return dependencies({
    runController: async request => {
      controls.push(request);
      if (request.actions.some(action => action.op === 'openThread')) return {nodes: [], results: []};
      if (hasStop(request)) return snapshot({value: '', buttonDescription: '停止'});
      return snapshot({buttonDescription: '停止'});
    },
    database: {
      readLatestTurn: async () => turnStates.length > 1 ? turnStates.shift() : turnStates[0],
    },
  });
}

test('stop fails when no terminal state is observed', async () => {
  const controls = [];
  const started = {turn_id: 'turn-active', state: 'started'};
  const chat = createNativeChat(stopController([started], controls));

  await assert.rejects(chat.request('sessionWall/stop', {threadId}), /停止尚未获得确认/);
  assert.equal(controls.filter(hasStop).length, 1);
});

test('stop succeeds after the same turn is formally interrupted', async () => {
  const controls = [];
  const chat = createNativeChat(stopController([
    {turn_id: 'turn-active', state: 'started'},
    {turn_id: 'turn-active', state: 'interrupted'},
  ], controls));

  assert.deepEqual(await chat.request('sessionWall/stop', {threadId}), {
    stopped: true,
    turn_id: 'turn-active',
  });
  assert.equal(controls.filter(hasStop).length, 1);
});

test('stop does not claim success for a non-interrupted terminal state', async () => {
  const controls = [];
  const chat = createNativeChat(stopController([
    {turn_id: 'turn-active', state: 'started'},
    {turn_id: 'turn-active', state: 'completed'},
  ], controls));

  await assert.rejects(chat.request('sessionWall/stop', {threadId}), /当前任务已结束/);
});
