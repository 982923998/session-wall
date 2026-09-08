'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const {
  readBaseline,
  findReceipt,
  readLatestTurn,
  readThreadSettings,
  createNativeUIReceipts,
  createSQLiteRunner,
} = require('./native-ui-receipts.cjs');

const threadId = '01a07e9e-6dcf-7533-b8de-de72986af5a5';

test('exports receipt readers without accessing Codex databases', () => {
  assert.equal(typeof readBaseline, 'function');
  assert.equal(typeof findReceipt, 'function');
  assert.equal(typeof readLatestTurn, 'function');
  assert.equal(typeof readThreadSettings, 'function');
});

test('reads the last item ordinal as a per-thread baseline', async () => {
  const calls = [];
  const receipts = createNativeUIReceipts({
    historyDatabase: '/tmp/test-history.sqlite',
    stateDatabase: '/tmp/test-state.sqlite',
    runSQLite: async (database, query, parameters) => {
      calls.push({database, query, parameters});
      return [{max_ordinal: 42}];
    },
  });

  assert.equal(await receipts.readBaseline(threadId.toUpperCase()), 42);
  assert.deepEqual(calls[0].parameters, {'@thread_id': threadId});
  assert.equal(calls[0].database, '/tmp/test-history.sqlite');
  assert.match(calls[0].query, /MAX\(rollout_ordinal\)/);
});

test('uses minus one when a thread has no recorded items', async () => {
  const receipts = createNativeUIReceipts({runSQLite: async () => []});
  assert.equal(await receipts.readBaseline(threadId), -1);
});

test('finds the first exact user-message receipt after the baseline', async () => {
  const calls = [];
  const receipts = createNativeUIReceipts({
    runSQLite: async (database, query, parameters) => {
      calls.push({database, query, parameters});
      return [
        {
          item_json: JSON.stringify({
            type: 'userMessage', id: 'native-wrong',
            content: [{type: 'text', text: '内容相似但不相同'}],
          }),
          turn_id: 'turn-wrong', rollout_ordinal: 8, status: 'completed',
        },
        {
          item_json: JSON.stringify({
            type: 'userMessage', id: 'native-exact', clientId: 'web-exact',
            content: [{type: 'text', text: '精确'}, {type: 'text', text: '消息'}],
          }),
          turn_id: 'turn-exact', rollout_ordinal: 9, status: 'inProgress',
        },
        {
          item_json: JSON.stringify({
            type: 'userMessage', id: 'native-later',
            content: [{type: 'text', text: '精确消息'}],
          }),
          turn_id: 'turn-later', rollout_ordinal: 10, status: 'completed',
        },
      ];
    },
  });

  assert.deepEqual(await receipts.findReceipt(threadId, '精确\n消息', 7), {
    native_message_id: 'native-exact',
    turn_id: 'turn-exact',
    state: 'started',
  });
  assert.deepEqual(calls[0].parameters, {'@thread_id': threadId, '@after_ordinal': 7});
  assert.match(calls[0].query, /i\.item_type = 'userMessage'/);
  assert.match(calls[0].query, /LIMIT 100/);
});

test('joins segmented user-message text with the Card transcript separator', async () => {
  const receipts = createNativeUIReceipts({
    runSQLite: async () => [{
      item_json: JSON.stringify({
        type: 'userMessage', id: 'segmented-message',
        content: [
          {type: 'text', text: '第一段'},
          {type: 'localImage', path: '/tmp/not-read.png'},
          {type: 'text', text: '第二段'},
        ],
      }),
      turn_id: 'segmented-turn', rollout_ordinal: 2, status: 'completed',
    }],
  });

  assert.deepEqual(await receipts.findReceipt(threadId, '第一段\n第二段', 1), {
    native_message_id: 'segmented-message',
    turn_id: 'segmented-turn',
    state: 'completed',
  });
  assert.equal(await receipts.findReceipt(threadId, '第一段第二段', 1), null);
});

test('falls back to clientId and maps terminal turn states', async () => {
  for (const state of ['completed', 'interrupted', 'failed']) {
    const receipts = createNativeUIReceipts({
      runSQLite: async () => [{
        item_json: JSON.stringify({
          type: 'userMessage', clientId: `client-${state}`,
          content: [{type: 'text', text: state}],
        }),
        turn_id: `turn-${state}`, rollout_ordinal: 1, status: state,
      }],
    });
    assert.deepEqual(await receipts.findReceipt(threadId, state, -1), {
      native_message_id: `client-${state}`,
      turn_id: `turn-${state}`,
      state,
    });
  }
});

test('returns null when no later item is an exact match', async () => {
  const receipts = createNativeUIReceipts({
    runSQLite: async () => [
      {item_json: '{bad json', turn_id: 'bad', status: 'completed'},
      {
        item_json: JSON.stringify({
          type: 'userMessage', id: 'different',
          content: [{type: 'text', text: 'message '}],
        }),
        turn_id: 'different', status: 'completed',
      },
    ],
  });
  assert.equal(await receipts.findReceipt(threadId, 'message', 0), null);
});

test('accepts exactly one native trailing newline without normalizing other differences', async () => {
  let itemText = '原始正文\n';
  const receipts = createNativeUIReceipts({
    runSQLite: async () => [{
      item_json: JSON.stringify({
        type: 'userMessage', id: 'native-newline',
        content: [{type: 'text', text: itemText}],
      }),
      turn_id: 'newline-turn', rollout_ordinal: 3, status: 'completed',
    }],
  });

  assert.deepEqual(await receipts.findReceipt(threadId, '原始正文', 2), {
    native_message_id: 'native-newline',
    turn_id: 'newline-turn',
    state: 'completed',
  });

  itemText = '原始 正文\n';
  assert.equal(await receipts.findReceipt(threadId, '原始正文', 2), null);
  itemText = '原始正文\n\n';
  assert.equal(await receipts.findReceipt(threadId, '原始正文', 2), null);
  itemText = ' 原始正文\n';
  assert.equal(await receipts.findReceipt(threadId, '原始正文', 2), null);
});

test('reads the latest turn and maps its live status', async () => {
  const calls = [];
  const receipts = createNativeUIReceipts({
    historyDatabase: '/tmp/test-history.sqlite',
    runSQLite: async (database, query, parameters) => {
      calls.push({database, query, parameters});
      return [{turn_id: 'latest-turn', status: 'inProgress'}];
    },
  });

  assert.deepEqual(await receipts.readLatestTurn(threadId), {
    turn_id: 'latest-turn', state: 'started',
  });
  assert.equal(calls[0].database, '/tmp/test-history.sqlite');
  assert.deepEqual(calls[0].parameters, {'@thread_id': threadId});
  assert.match(calls[0].query, /FROM thread_turns/);
  assert.match(calls[0].query, /ORDER BY rollout_ordinal DESC/);
  assert.match(calls[0].query, /LIMIT 1/);
});

test('returns null when a thread has no turns', async () => {
  const receipts = createNativeUIReceipts({runSQLite: async () => []});
  assert.equal(await receipts.readLatestTurn(threadId), null);
});

test('reads only model settings from the state database', async () => {
  const calls = [];
  const receipts = createNativeUIReceipts({
    stateDatabase: '/tmp/test-state.sqlite',
    runSQLite: async (database, query, parameters) => {
      calls.push({database, query, parameters});
      return [{
        model: 'gpt-5.6-sol', reasoning_effort: 'high',
        cwd: '/private/project', preview: 'private conversation',
      }];
    },
  });

  assert.deepEqual(await receipts.readThreadSettings(threadId), {
    model: 'gpt-5.6-sol', reasoning_effort: 'high',
  });
  assert.equal(calls[0].database, '/tmp/test-state.sqlite');
  assert.match(calls[0].query, /^SELECT model, reasoning_effort/);
  assert.doesNotMatch(calls[0].query, /cwd|preview/);
});

test('returns null for missing thread settings', async () => {
  const receipts = createNativeUIReceipts({runSQLite: async () => []});
  assert.equal(await receipts.readThreadSettings(threadId), null);
});

test('rejects invalid thread, message, and baseline inputs before querying', async () => {
  let calls = 0;
  const receipts = createNativeUIReceipts({runSQLite: async () => {
    calls += 1;
    return [];
  }});

  await assert.rejects(receipts.readBaseline("' OR 1=1 --"), /Invalid thread ID/);
  await assert.rejects(receipts.findReceipt(threadId, '', 0), /Message must contain/);
  await assert.rejects(receipts.findReceipt(threadId, 'message', 1.5), /Invalid baseline/);
  await assert.rejects(receipts.readThreadSettings('not-a-thread'), /Invalid thread ID/);
  assert.equal(calls, 0);
});

test('sqlite runner uses readonly JSON mode and bound CLI parameters', async () => {
  const calls = [];
  const runner = createSQLiteRunner((file, args, options, callback) => {
    calls.push({file, args, options});
    callback(null, '[{"value":1}]');
  });

  assert.deepEqual(await runner('/tmp/database.sqlite', 'SELECT @thread_id;', {
    '@thread_id': threadId,
    '@after_ordinal': 12,
  }), [{value: 1}]);
  assert.equal(calls[0].file, 'sqlite3');
  assert.deepEqual(calls[0].args.slice(0, 4), ['-readonly', '-json', '-cmd', '.parameter init']);
  assert.ok(calls[0].args.includes(`.parameter set @thread_id '${threadId}'`));
  assert.ok(calls[0].args.includes('.parameter set @after_ordinal 12'));
  assert.deepEqual(calls[0].args.slice(-2), ['/tmp/database.sqlite', 'SELECT @thread_id;']);
});
