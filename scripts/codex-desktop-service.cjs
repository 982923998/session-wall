#!/opt/homebrew/bin/node
'use strict';

const {execFileSync, spawn} = require('node:child_process');
const app = '/Applications/ChatGPT.app/Contents/MacOS/ChatGPT';
let child = null;
let lastStatus = '';
let checking = false;
function log(status) {
  if (status !== lastStatus) { console.log(new Date().toISOString(), status); lastStatus = status; }
}
async function tick() {
  if (checking) return;
  checking=true;
  try {
    const processes=execFileSync('/bin/ps',['-axo','pid=,comm='],{encoding:'utf8',timeout:3000});
    if (processes.split('\n').some(line=>line.trim().endsWith(app))) {
      log('Codex desktop is running; preserving its native tool startup');
      return;
    }
    if (child) return;
    const env={...process.env};
    delete env.CODEX_CLI_PATH;
    delete env.CODEX_APP_SERVER_FORCE_CLI;
    delete env.CODEX_APP_SERVER_WS_URL;
    child=spawn(app,[],{env,stdio:['ignore','inherit','inherit']});
    child.on('error',e=>{child=null;log('Desktop launch failed: '+e.message);});
    child.on('exit',()=>{child=null;log('Desktop exited; automatic restart pending');});
    log('Started Codex desktop with native configuration');
  } catch(e) { log('Supervisor check failed: '+e.message); }
  finally { checking=false; }
}
// The supervisor never terminates or replaces an existing desktop process.
tick();
setInterval(tick,5000);
