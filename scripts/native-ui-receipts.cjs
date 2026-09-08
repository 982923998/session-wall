'use strict';

const {execFile} = require('node:child_process');
const {homedir} = require('node:os');
const {join} = require('node:path');

const threadIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const maximumMessageBytes = 64 * 1024;

function validateThreadID(threadId) {
  if (typeof threadId !== 'string' || !threadIDPattern.test(threadId)) {
    throw new TypeError('Invalid thread ID');
  }
  return threadId.toLowerCase();
}

function sqliteLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function parameterCommand(name, value) {
  const literal = typeof value === 'number' ? String(value) : sqliteLiteral(value);
  return `.parameter set ${name} ${literal}`;
}

function createSQLiteRunner(execFileImpl = execFile) {
  if (typeof execFileImpl !== 'function') throw new TypeError('execFile must be a function');

  return (database, query, parameters) => new Promise((resolve, reject) => {
    const args = ['-readonly', '-json', '-cmd', '.parameter init'];
    for (const [name, value] of Object.entries(parameters)) {
      args.push('-cmd', parameterCommand(name, value));
    }
    args.push(database, query);

    execFileImpl('sqlite3', args, {
      timeout: 5_000,
      maxBuffer: 1024 * 1024,
      encoding: 'utf8',
    }, (error, stdout) => {
      if (error) {
        reject(error);
        return;
      }
      try {
        const text = String(stdout || '').trim();
        const rows = text ? JSON.parse(text) : [];
        if (!Array.isArray(rows)) throw new Error('SQLite returned invalid JSON');
        resolve(rows);
      } catch (error) {
        reject(error);
      }
    });
  });
}

function messageText(item) {
  if (!item || item.type !== 'userMessage' || !Array.isArray(item.content)) return null;
  return item.content
    .filter(part => part?.type === 'text' && typeof part.text === 'string')
    .map(part => part.text)
    .join('\n');
}

function receiptState(status) {
  if (status === 'inProgress') return 'started';
  if (status === 'completed' || status === 'interrupted' || status === 'failed') return status;
  throw new Error(`Unsupported Codex turn status: ${status || 'missing'}`);
}

function createNativeUIReceipts(options = {}) {
  if (!options || typeof options !== 'object' || Array.isArray(options)) {
    throw new TypeError('Receipt options must be an object');
  }
  const codexHome = options.codexHome || process.env.CODEX_HOME || join(homedir(), '.codex');
  const historyDatabase = options.historyDatabase || join(codexHome, 'thread_history_1.sqlite');
  const stateDatabase = options.stateDatabase || join(codexHome, 'state_5.sqlite');
  const runSQLite = options.runSQLite || createSQLiteRunner(options.execFile);
  if (typeof runSQLite !== 'function') throw new TypeError('runSQLite must be a function');

  async function readBaseline(threadId) {
    const normalizedThreadId = validateThreadID(threadId);
    const rows = await runSQLite(historyDatabase, `SELECT COALESCE(MAX(rollout_ordinal), -1) AS max_ordinal
FROM thread_items
WHERE thread_id = @thread_id;`, {'@thread_id': normalizedThreadId});
    const ordinal = Number(rows[0]?.max_ordinal);
    return Number.isSafeInteger(ordinal) && ordinal >= -1 ? ordinal : -1;
  }

  async function findReceipt(threadId, message, afterOrdinal) {
    const normalizedThreadId = validateThreadID(threadId);
    if (typeof message !== 'string' || message.length === 0 || Buffer.byteLength(message) > maximumMessageBytes) {
      throw new TypeError('Message must contain at most 64 KiB');
    }
    if (!Number.isSafeInteger(afterOrdinal) || afterOrdinal < -1) {
      throw new TypeError('Invalid baseline ordinal');
    }

    const rows = await runSQLite(historyDatabase, `SELECT i.item_json, i.turn_id, i.rollout_ordinal, t.status
FROM thread_items AS i
JOIN thread_turns AS t
  ON t.thread_id = i.thread_id AND t.turn_id = i.turn_id
WHERE i.thread_id = @thread_id
  AND i.item_type = 'userMessage'
  AND i.rollout_ordinal > @after_ordinal
ORDER BY i.rollout_ordinal ASC
LIMIT 100;`, {
      '@thread_id': normalizedThreadId,
      '@after_ordinal': afterOrdinal,
    });

    for (const row of rows) {
      let item;
      try {
        item = JSON.parse(row.item_json);
      } catch {
        continue;
      }
      const persistedMessage = messageText(item);
      if (persistedMessage !== message && persistedMessage !== `${message}\n`) continue;
      const nativeMessageID = typeof item.id === 'string' && item.id
        ? item.id
        : typeof item.clientId === 'string' && item.clientId
          ? item.clientId
          : null;
      if (!nativeMessageID || typeof row.turn_id !== 'string' || !row.turn_id) continue;
      return {
        native_message_id: nativeMessageID,
        turn_id: row.turn_id,
        state: receiptState(row.status),
      };
    }
    return null;
  }

  async function readLatestTurn(threadId) {
    const normalizedThreadId = validateThreadID(threadId);
    const rows = await runSQLite(historyDatabase, `SELECT turn_id, status
FROM thread_turns
WHERE thread_id = @thread_id
ORDER BY rollout_ordinal DESC
LIMIT 1;`, {'@thread_id': normalizedThreadId});
    if (!rows[0]) return null;
    if (typeof rows[0].turn_id !== 'string' || !rows[0].turn_id) {
      throw new Error('Codex latest turn is missing its ID');
    }
    return {
      turn_id: rows[0].turn_id,
      state: receiptState(rows[0].status),
    };
  }

  async function readThreadSettings(threadId) {
    const normalizedThreadId = validateThreadID(threadId);
    const rows = await runSQLite(stateDatabase, `SELECT model, reasoning_effort
FROM threads
WHERE id = @thread_id
LIMIT 1;`, {'@thread_id': normalizedThreadId});
    if (!rows[0]) return null;
    return {
      model: typeof rows[0].model === 'string' ? rows[0].model : '',
      reasoning_effort: typeof rows[0].reasoning_effort === 'string' ? rows[0].reasoning_effort : '',
    };
  }

  return {readBaseline, findReceipt, readLatestTurn, readThreadSettings};
}

const {readBaseline, findReceipt, readLatestTurn, readThreadSettings} = createNativeUIReceipts();

module.exports = {
  readBaseline,
  findReceipt,
  readLatestTurn,
  readThreadSettings,
  createNativeUIReceipts,
  createSQLiteRunner,
};
