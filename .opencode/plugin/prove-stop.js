// Gate OpenCode's turn end on the repo's Bend law proof.
//
// No OpenCode plugin hook can refuse a turn end: `session.idle` only arrives
// through the public event stream. So this nudges instead - it pushes a
// follow-up user message, the same shape as Cursor's followup_message.
//
// hooks/prove-stop.sh bounds its own retries (three consecutive failures, then it
// stands down and exits 0), so this cannot loop forever. Only exit 2 is a block.
//
// Loaded from .opencode/plugin/ - the project-level discovery path.
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

export default {
  id: 'prove-stop',
  async setup(ctx) {
    const controller = new AbortController();

    // Subscribe to the public server event stream. V2 carries the session id
    // under event.data; the hook returns a cleanup that aborts this loop.
    void (async () => {
      try {
        for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
          if (event?.type !== 'session.idle') continue;
          const sessionID = event?.data?.sessionID ?? event?.sessionID;
          if (!sessionID) continue;

          let code = 0;
          let stderr = '';
          try {
            await run('bash', [GATE], { timeout: 300000 });
          } catch (err) {
            code = typeof err?.code === 'number' ? err.code : -1;
            stderr = err?.stderr ?? '';
          }
          if (code !== 2) continue;
          const text = String(stderr).trim();
          if (!text) continue;

          await ctx.session.prompt({ sessionID, text });
        }
      } catch {
        // Aborted on shutdown - nothing to do.
      }
    })();

    return () => controller.abort();
  },
};
