// Gate OpenCode's turn end on the repo's Bend law proof.
//
// No OpenCode plugin hook can refuse a turn end: every hook returns void and
// session.idle only arrives through the observer. So this nudges instead - it
// pushes a follow-up user message, the same shape as Cursor's followup_message.
//
// hooks/prove-stop.sh bounds its own retries (three consecutive failures, then it
// stands down and exits 0), so this cannot loop forever. Only exit 2 is a block.
//
// Loaded from .opencode/plugin/ - the project-level discovery path. A "plugins"
// entry in opencode.json does NOT load a local file (it is for npm packages).
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

// this file lives at <repo>/.opencode/plugin/, so the gate is two levels up
const GATE = join(dirname(dirname(dirname(fileURLToPath(import.meta.url)))), 'hooks', 'prove-stop.sh');

function gate() {
  return spawnSync('bash', [GATE], { encoding: 'utf8', timeout: 300000 });
}

const hooks = {
  event: async ({ event }) => {
    if (event?.type !== 'session.idle') return;
    const sessionID = event?.properties?.sessionID;
    if (!sessionID) return;

    const r = gate();
    if (r.status !== 2) return;
    const text = (r.stderr || '').trim();
    if (!text) return;

    await ctxRef.client.session.prompt({ sessionID, parts: [{ type: 'text', text }] });
  },
};

let ctxRef = {};
export default {
  id: 'prove-stop',
  server: async (ctx) => { ctxRef = ctx; return hooks; },
  setup: async (ctx) => { ctxRef = ctx; return hooks; },
};
