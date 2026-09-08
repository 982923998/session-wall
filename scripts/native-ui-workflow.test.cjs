'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const {createPrepareDraft, prepareDraft} = require('./native-ui-workflow.cjs');

const threadId = '01a07e9e-6dcf-7533-b8de-de72986af5a5';
const focusCommand = '聚焦主聊天输入框';
const emptyEditor = {
  path: [1], role: 'AXTextArea', title: '', description: '随心输入',
  identifier: 'prompt', value: '\n随心输入',
};
const combo = {
  path: [2], role: 'AXComboBox', title: '命令菜单', description: '',
  identifier: 'command-menu', value: '聚焦主聊天输入框',
};
const suggestion = {
  path: [3], role: 'AXStaticText', title: '聚焦主聊天输入框',
  description: '', identifier: '', value: '',
};

function controllerFixture(message, calls) {
  const responses = [
    {nodes: [], results: []},
    {nodes: [emptyEditor], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...combo, value: ''}], results: []},
    {nodes: [combo, suggestion], results: []},
    {nodes: [emptyEditor], results: []},
    {nodes: [emptyEditor], results: [{text: message}]},
  ];
  return async request => {
    calls.push(request);
    return responses.shift();
  };
}

test('exports prepareDraft without running the controller', () => {
  assert.equal(typeof prepareDraft, 'function');
});

test('prepares and verifies a draft without a send or stop action', async () => {
  const calls = [];
  const message = '保留这条草稿';
  const draft = createPrepareDraft(controllerFixture(message, calls));

  assert.deepEqual(await draft(threadId, message), {
    state: 'draft', thread_id: threadId, message,
  });
  assert.equal(calls.length, 6);
  for (const request of calls) {
    assert.equal(request.preserveClipboard, true);
    assert.ok(request.actions.some(action => action.op === 'activate'));
    assert.ok(!request.actions.some(action => action.op === 'stop'));
  }
  const returnKeys = calls.flatMap(request => request.actions)
    .filter(action => action.op === 'key' && action.key === 36);
  assert.deepEqual(returnKeys, [{op: 'key', key: 36, modifiers: []}]);
  assert.ok(!calls.flatMap(request => request.actions)
    .some(action => action.op === 'press'));
});

test('refuses to overwrite an existing draft before opening the command menu', async () => {
  const calls = [];
  const responses = [
    {nodes: [], results: []},
    {nodes: [{...emptyEditor, value: '用户的草稿'}], results: [{text: `codex://threads/${threadId}`}]},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  await assert.rejects(draft(threadId, '新草稿'), /already contains a draft/);
  assert.equal(calls.length, 2);
});

test('fails when an accessibility match is ambiguous', async () => {
  const calls = [];
  const responses = [
    {nodes: [], results: []},
    {nodes: [emptyEditor, {...emptyEditor, path: [9]}], results: [{text: `codex://threads/${threadId}`}]},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  await assert.rejects(draft(threadId, '新草稿'), /ambiguous/);
  assert.equal(calls.length, 2);
});

test('refuses a draft that appears after the focus command', async () => {
  const calls = [];
  const responses = [
    {nodes: [], results: []},
    {nodes: [emptyEditor], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...combo, value: ''}], results: []},
    {nodes: [combo, suggestion], results: []},
    {nodes: [{...emptyEditor, value: '用户刚刚输入的草稿'}], results: []},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  await assert.rejects(draft(threadId, '新草稿'), /acquired a draft/);
  assert.equal(calls.length, 5);
});

test('accepts the native empty editor value', async () => {
  const calls = [];
  const message = '保留草稿';
  const nativeEmptyEditor = {...emptyEditor, value: ''};
  const responses = [
    {nodes: [], results: []},
    {nodes: [nativeEmptyEditor], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...combo, value: ''}], results: []},
    {nodes: [combo, suggestion], results: []},
    {nodes: [nativeEmptyEditor], results: []},
    {nodes: [nativeEmptyEditor], results: [{text: message}]},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  assert.equal((await draft(threadId, message)).state, 'draft');
  assert.equal(calls.length, 6);
});

test('folds nested command text within the same suggestion', async () => {
  const calls = [];
  const message = '嵌套建议';
  const realCombo = {...combo, path: [1, 4, 0, 1]};
  const outerSuggestion = {...suggestion, path: [1, 4, 0, 0]};
  const innerSuggestion = {...suggestion, path: [1, 4, 0, 0, 0, 0]};
  const responses = [
    {nodes: [], results: []},
    {nodes: [emptyEditor], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...realCombo, value: ''}], results: []},
    {nodes: [realCombo, outerSuggestion, innerSuggestion], results: []},
    {nodes: [emptyEditor], results: []},
    {nodes: [emptyEditor], results: [{text: message}]},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  assert.equal((await draft(threadId, message)).state, 'draft');
  assert.equal(calls.length, 6);
});

test('rejects two distinct matching command suggestions', async () => {
  const calls = [];
  const realCombo = {...combo, path: [1, 4, 0, 1]};
  const firstSuggestion = {...suggestion, path: [1, 4, 0, 0]};
  const secondSuggestion = {...suggestion, path: [1, 4, 0, 2]};
  const responses = [
    {nodes: [], results: []},
    {nodes: [emptyEditor], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...realCombo, value: ''}], results: []},
    {nodes: [realCombo, firstSuggestion, secondSuggestion], results: []},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  await assert.rejects(draft(threadId, '不能写入'), /ambiguous/);
  assert.equal(calls.length, 4);
});

test('waits for delayed controls without replaying any input action', async () => {
  const calls = [];
  const message = '延迟出现的控件';
  const responses = [
    {nodes: [], results: []},
    {nodes: [], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [emptyEditor], results: []},
    {nodes: [], results: []},
    {nodes: [{...combo, value: ''}], results: []},
    {nodes: [combo], results: []},
    {nodes: [combo, suggestion], results: []},
    {nodes: [], results: []},
    {nodes: [emptyEditor], results: []},
    {nodes: [emptyEditor], results: [{text: message}]},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  assert.equal((await draft(threadId, message)).state, 'draft');
  const actions = calls.flatMap(request => request.actions);
  assert.equal(actions.filter(action => action.op === 'openThread').length, 1);
  assert.equal(actions.filter(action => action.op === 'key' && action.key === 37).length, 1);
  assert.equal(actions.filter(action => action.op === 'key' && action.key === 40).length, 1);
  assert.equal(actions.filter(action => action.op === 'key' && action.key === 36).length, 1);
  assert.equal(actions.filter(action => action.op === 'writeClipboard' && action.text === focusCommand).length, 1);
  assert.equal(actions.filter(action => action.op === 'writeClipboard' && action.text === message).length, 1);
  assert.equal(calls.filter(request => request.actions.length === 2 &&
    request.actions[0].op === 'activate' && request.actions[1].op === 'wait').length, 4);
});

test('a delayed existing draft still fails before any input is replayed', async () => {
  const calls = [];
  const responses = [
    {nodes: [], results: []},
    {nodes: [], results: [{text: `codex://threads/${threadId}`}]},
    {nodes: [{...emptyEditor, value: '用户已有草稿'}], results: []},
  ];
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    return responses.shift();
  });

  await assert.rejects(draft(threadId, '不应写入'), /verify-empty-draft.*already contains a draft/);
  const actions = calls.flatMap(request => request.actions);
  assert.equal(actions.some(action => action.op === 'writeClipboard'), false);
  assert.equal(actions.some(action => action.op === 'key' && [40, 36, 9].includes(action.key)), false);
});

test('serializes complete draft transactions', async () => {
  const otherThreadId = '019f3ffd-7d62-79a0-adeb-b826e8b50152';
  const calls = [];
  let activeThread;
  let phase = 0;
  const draft = createPrepareDraft(async request => {
    calls.push(request);
    const openThread = request.actions.find(action => action.op === 'openThread');
    if (openThread) {
      activeThread = openThread.threadId;
      phase = 0;
      return {nodes: [], results: []};
    }
    phase += 1;
    if (phase === 1) return {
      nodes: [emptyEditor], results: [{text: `codex://threads/${activeThread}`}],
    };
    if (phase === 2) return {nodes: [{...combo, value: ''}], results: []};
    if (phase === 3) return {nodes: [combo, suggestion], results: []};
    if (phase === 4) return {nodes: [emptyEditor], results: []};
    const message = request.actions.find(action => action.op === 'writeClipboard')?.text;
    return {nodes: [emptyEditor], results: [{text: message}]};
  });

  await Promise.all([
    draft(threadId, '第一条草稿'),
    draft(otherThreadId, '第二条草稿'),
  ]);

  const navigationIndexes = calls.flatMap((request, index) =>
    request.actions.some(action => action.op === 'openThread') ? [index] : []);
  assert.deepEqual(navigationIndexes, [0, 6]);
});
