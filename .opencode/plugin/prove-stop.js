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
//
// The gate runs asynchronously: a plugin runs inside the OpenCode process, so a
// synchronous spawn would freeze the editor for as long as bend takes.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const run = promisify(execFile);

// this file lives at <repo>/.opencode/plugin/, so two directories up is <repo>
const GATE = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'hooks', 'prove-stop.sh');

// Built per invocation and closed over its own ctx, so two entry points (or two
// concurrent sessions) cannot clobber each other's client.
function makeHooks(ctx) {
  return {
    event: async ({ event }) => {
      if (event?.type !== 'session.idle') return;
      const sessionID = event?.properties?.sessionID;
      if (!sessionID) return;

      let code = 0;
      let stderr = '';
      try {
        await run('bash', [GATE], { timeout: 300000 });
      } catch (err) {
        code = typeof err?.code === 'number' ? err.code : -1;
        stderr = err?.stderr ?? '';
      }
      if (code !== 2) return;
      const text = String(stderr).trim();
      if (!text) return;

      await ctx.client.session.prompt({ sessionID, parts: [{ type: 'text', text }] });
    },
  };
}

export default {
  id: 'prove-stop',
  server: async (ctx) => makeHooks(ctx),
  setup: async (ctx) => makeHooks(ctx),
};
