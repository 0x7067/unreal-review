// Gate Pi's turn end on the repo's Bend law proof.
//
// Pi's turn_end event cannot refuse a stop (only tool_call and friends return a
// decision), so this does what Cursor's followup_message does: when the proof is
// red it pushes a user message, which forces another turn.
//
// hooks/prove-stop.sh bounds its own retries - three consecutive failures, then
// it stands down and exits 0 - so this cannot loop forever. Only exit 2 is a
// block; 0 means green, or the gate has given up and CI takes over.
//
// The gate is resolved from this file's own location, never from ctx.cwd: a
// relative "hooks/prove-stop.sh" exits 127 whenever Pi runs from a subdirectory,
// and 127 is indistinguishable from a pass here.
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const GATE = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'hooks', 'prove-stop.sh');

export default function proveStop(pi) {
  pi.on('turn_end', async () => {
    const r = await pi.exec('bash', [GATE]);
    if (r.code !== 2) return;
    const text = (r.stderr || '').trim();
    if (!text) return;
    pi.sendUserMessage(text, { deliverAs: 'followUp' });
  });
}
