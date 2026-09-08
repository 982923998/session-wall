'use strict';

const {execFile} = require('node:child_process');
const {mkdtemp, readFile, rm, writeFile, chmod} = require('node:fs/promises');
const {tmpdir} = require('node:os');
const {join} = require('node:path');

const controllerApp = '/Users/chenmayao/Library/Application Support/SessionWall/Session Wall Controller.app';
const controllerTimeoutMs = 15_000;

let controllerCalls = Promise.resolve();

function executeController(args) {
  return new Promise((resolve, reject) => {
    execFile('/usr/bin/open', args, {
      timeout: controllerTimeoutMs,
      killSignal: 'SIGKILL',
    }, (error) => error ? reject(error) : resolve());
  });
}

async function readText(path) {
  try {
    return await readFile(path, 'utf8');
  } catch (error) {
    if (error?.code === 'ENOENT') return '';
    throw error;
  }
}

async function invokeController(request) {
  if (!request || typeof request !== 'object' || Array.isArray(request)) {
    throw new TypeError('Controller request must be an object');
  }

  const directory = await mkdtemp(join(tmpdir(), 'session-wall-controller-'));
  const requestPath = join(directory, 'request.json');
  const stdoutPath = join(directory, 'stdout.json');
  const stderrPath = join(directory, 'stderr.txt');

  try {
    await chmod(directory, 0o700);
    await writeFile(requestPath, JSON.stringify(request), {encoding: 'utf8', flag: 'wx', mode: 0o600});

    let executionError;
    try {
      await executeController([
        '-g',
        '-W',
        '-a', controllerApp,
        '--stdout', stdoutPath,
        '--stderr', stderrPath,
        '--args', 'request', requestPath,
      ]);
    } catch (error) {
      executionError = error;
    }

    const output = (await readText(stdoutPath)).trim();
    if (!output) {
      const stderr = (await readText(stderrPath)).trim();
      if (executionError) {
        if (stderr) executionError.message += `: ${stderr.slice(0, 4000)}`;
        throw executionError;
      }
      throw new Error(stderr || 'Controller returned no response');
    }

    let response;
    try {
      response = JSON.parse(output);
    } catch {
      throw new Error('Controller returned invalid JSON');
    }
    if (!response || typeof response !== 'object' || Array.isArray(response)) {
      throw new Error('Controller returned an invalid response');
    }
    if (typeof response.error === 'string' && response.error) {
      throw new Error(response.error);
    }
    if (executionError) throw executionError;
    return response;
  } finally {
    await rm(directory, {recursive: true, force: true});
  }
}

function runController(request) {
  const call = controllerCalls.then(() => invokeController(request));
  controllerCalls = call.catch(() => {});
  return call;
}

function findNode(snapshot, predicate) {
  if (!snapshot || !Array.isArray(snapshot.nodes)) {
    throw new TypeError('Controller snapshot must contain nodes');
  }
  if (typeof predicate !== 'function') {
    throw new TypeError('Node predicate must be a function');
  }

  const matches = snapshot.nodes.filter(predicate);
  if (matches.length === 0) throw new Error('No matching accessibility node');
  if (matches.length > 1) throw new Error('Accessibility node match is ambiguous');
  return matches[0];
}

function actionFor(node, op, extra = {}) {
  if (!node || !Array.isArray(node.path) || typeof node.role !== 'string' || !node.role) {
    throw new TypeError('Accessibility node identity is incomplete');
  }
  if (typeof op !== 'string' || !op) throw new TypeError('Action operation is required');
  if (!extra || typeof extra !== 'object' || Array.isArray(extra)) {
    throw new TypeError('Action extras must be an object');
  }

  return {
    ...extra,
    op,
    path: [...node.path],
    role: node.role,
    title: typeof node.title === 'string' ? node.title : '',
    description: typeof node.description === 'string' ? node.description : '',
    identifier: typeof node.identifier === 'string' ? node.identifier : '',
  };
}

module.exports = {runController, findNode, actionFor};
