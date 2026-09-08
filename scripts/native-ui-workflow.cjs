'use strict';

const {runController, findNode} = require('./native-ui-driver.cjs');

const focusCommand = '聚焦主聊天输入框';
const emptyEditorValues = new Set(['', '\n随心输入']);
const threadIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const missingNodeMessage = 'No matching accessibility node';
const maximumReadOnlyRetries = 4;

function clipboardText(snapshot) {
  const results = snapshot?.results;
  if (!Array.isArray(results) || results.length !== 1 || typeof results[0]?.text !== 'string') {
    throw new Error('Controller did not return one clipboard value');
  }
  return results[0].text;
}

function editorNode(snapshot) {
  return findNode(snapshot, node =>
    node?.role === 'AXTextArea' && node?.description === '随心输入');
}

function commandMenuNode(snapshot) {
  return findNode(snapshot, node =>
    node?.role === 'AXComboBox' && node?.title === '命令菜单');
}

function hasExactCommand(node) {
  return [node?.title, node?.description, node?.value].some(value => value === focusCommand);
}

function pathStartsWith(path, parent) {
  return Array.isArray(path) && parent.every((part, index) => path[index] === part);
}

function commandSuggestionNode(snapshot, combo) {
  const containerPath = combo.path.slice(0, -1);
  const candidates = snapshot.nodes
    .filter(node => Array.isArray(node?.path) &&
      node.path.length > containerPath.length &&
      pathStartsWith(node.path, containerPath) &&
      !hasSamePath(node.path, combo.path) &&
      hasExactCommand(node))
    .sort((left, right) => left.path.length - right.path.length);
  const outermost = candidates.filter((node, index) =>
    !candidates.slice(0, index).some(parent => pathStartsWith(node.path, parent.path)));
  return findNode({nodes: outermost}, () => true);
}

function hasSamePath(left, right) {
  return left.length === right.length && pathStartsWith(left, right);
}

function editorIsEmpty(node) {
  return emptyEditorValues.has(node.value);
}

function stageError(stage, error) {
  const reason = error instanceof Error ? error.message : 'operation failed';
  const wrapped = new Error(`${stage}: ${reason}`);
  wrapped.cause = error;
  return wrapped;
}

function createPrepareDraft(controller = runController) {
  if (typeof controller !== 'function') throw new TypeError('runController must be a function');

  let transactions = Promise.resolve();

  async function call(actions) {
    return controller({
      preserveClipboard: true,
      actions: [{op: 'activate'}, ...actions],
    });
  }

  async function stageCall(stage, actions) {
    try {
      return await call(actions);
    } catch (error) {
      throw stageError(stage, error);
    }
  }

  async function findAfter(stage, initialSnapshot, finder) {
    let snapshot = initialSnapshot;
    for (let retry = 0; ; retry += 1) {
      try {
        return {snapshot, node: finder(snapshot)};
      } catch (error) {
        if (error?.message !== missingNodeMessage || retry >= maximumReadOnlyRetries) {
          throw stageError(stage, error);
        }
      }
      snapshot = await stageCall(stage, [{op: 'wait', seconds: 0.3}]);
    }
  }

  function clipboardAtStage(stage, snapshot) {
    try {
      return clipboardText(snapshot);
    } catch (error) {
      throw stageError(stage, error);
    }
  }

  async function prepare(threadId, message) {
    if (typeof threadId !== 'string' || !threadIDPattern.test(threadId)) {
      throw new TypeError('Invalid thread ID');
    }
    if (typeof message !== 'string' || message.length === 0 || Buffer.byteLength(message) > 64 * 1024) {
      throw new TypeError('Draft message must contain at most 64 KiB');
    }

    const normalizedThreadId = threadId.toLowerCase();
    const expectedLink = `codex://threads/${normalizedThreadId}`;

    await stageCall('open-thread', [
      {op: 'openThread', threadId: normalizedThreadId},
      {op: 'activate'},
      {op: 'wait', seconds: 1},
    ]);

    const identity = await stageCall('verify-thread', [
      {op: 'key', key: 37, modifiers: ['command', 'option']},
      {op: 'wait', seconds: 0.2},
      {op: 'readClipboard'},
    ]);
    if (clipboardAtStage('verify-thread', identity) !== expectedLink) {
      throw new Error('verify-thread: Codex opened a different thread');
    }
    const initialEditor = await findAfter('verify-empty-draft', identity, editorNode);
    if (!editorIsEmpty(initialEditor.node)) {
      throw new Error('verify-empty-draft: The thread already contains a draft');
    }

    const openedMenu = await stageCall('open-focus-command', [
      {op: 'key', key: 40, modifiers: ['command']},
      {op: 'wait', seconds: 0.2},
    ]);
    await findAfter('open-focus-command', openedMenu, commandMenuNode);

    const selectedCommand = await stageCall('select-focus-command', [
      {op: 'writeClipboard', text: focusCommand},
      {op: 'key', key: 0, modifiers: ['command']},
      {op: 'key', key: 9, modifiers: ['command']},
      {op: 'wait', seconds: 0.2},
    ]);
    await findAfter('select-focus-command', selectedCommand, snapshot => {
      const combo = commandMenuNode(snapshot);
      if (combo.value !== focusCommand) throw new Error('Codex command menu text did not match');
      commandSuggestionNode(snapshot, combo);
      return combo;
    });

    const focusedEditor = await stageCall('focus-editor', [
      {op: 'key', key: 36, modifiers: []},
      {op: 'wait', seconds: 0.2},
    ]);
    const emptyFocusedEditor = await findAfter('focus-editor', focusedEditor, editorNode);
    if (!editorIsEmpty(emptyFocusedEditor.node)) {
      throw new Error('focus-editor: The thread acquired a draft before writing');
    }

    const written = await stageCall('write-draft', [
      {op: 'writeClipboard', text: message},
      {op: 'key', key: 0, modifiers: ['command']},
      {op: 'key', key: 9, modifiers: ['command']},
      {op: 'wait', seconds: 0.2},
      {op: 'key', key: 0, modifiers: ['command']},
      {op: 'key', key: 8, modifiers: ['command']},
      {op: 'readClipboard'},
      {op: 'key', key: 124, modifiers: []},
    ]);
    if (clipboardAtStage('write-draft', written) !== message) {
      throw new Error('write-draft: Codex draft verification failed');
    }

    return {state: 'draft', thread_id: normalizedThreadId, message};
  }

  return function prepareDraft(threadId, message) {
    const transaction = transactions.then(() => prepare(threadId, message));
    transactions = transaction.catch(() => {});
    return transaction;
  };
}

const prepareDraft = createPrepareDraft();

module.exports = {prepareDraft, createPrepareDraft};
