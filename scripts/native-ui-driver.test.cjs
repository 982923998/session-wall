'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const {actionFor, findNode, runController} = require('./native-ui-driver.cjs');

test('exports the controller entry point without invoking it', () => {
  assert.equal(typeof runController, 'function');
});

test('findNode requires exactly one match', () => {
  const first = {path: [0], role: 'AXButton', title: 'Send'};
  const second = {path: [1], role: 'AXButton', title: 'Stop'};
  const snapshot = {nodes: [first, second]};

  assert.equal(findNode(snapshot, node => node.title === 'Send'), first);
  assert.throws(() => findNode(snapshot, node => node.role === 'AXButton'), /ambiguous/);
  assert.throws(() => findNode(snapshot, node => node.title === 'Missing'), /No matching/);
});

test('actionFor binds the complete node identity and protects it from extras', () => {
  const node = {
    path: [2, 4],
    role: 'AXTextArea',
    title: 'Prompt',
    description: 'Message Codex',
    identifier: 'composer',
  };

  assert.deepEqual(actionFor(node, 'setValue', {
    value: 'hello',
    role: 'AXButton',
    path: [99],
  }), {
    value: 'hello',
    op: 'setValue',
    path: [2, 4],
    role: 'AXTextArea',
    title: 'Prompt',
    description: 'Message Codex',
    identifier: 'composer',
  });
});
