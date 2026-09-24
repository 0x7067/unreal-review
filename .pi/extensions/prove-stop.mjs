// Gate Pi's turn end on the repo's Bend law proof.
//
// Pi's turn_end event cannot refuse a stop (only tool_call and friends return a
// decision), so this does what Cursor's followup_message does: when the proof is
// red it pushes a user message, which forces another turn.
//
// hooks/prove-stop.sh bounds its own retries - three consecutive failures, then
// it stands down and exits 0 - so this cannot loop forever. Only exit 2 is a
// block; 0 means green, or the gate has given up and CI takes over.
export default function proveStop(pi) {
  pi.on('turn_end', async (_event, ctx) => {
    const r = await pi.exec('bash', ['hooks/prove-stop.sh'], { cwd: ctx.cwd });
    if (r.code !== 2) return;
    const text = (r.stderr || '').trim();
    if (!text) return;
    pi.sendUserMessage(text, { deliverAs: 'followUp' });
  });
}
