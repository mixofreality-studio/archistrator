/**
 * The value DynamicViewFlow's `build()` puts on a CURRENT call's canvas chip
 * (EdgeSeqChip, flowDecor.tsx) — fix round 1 on the call-chain rollout Task 5
 * review (FINDING 1, the same-call contradiction): the alt-aware label is
 * FRAGMENT-MODE ONLY.
 *
 * Outside fragment mode — the self-paced step-through (an ArchitectureView
 * session before/without a walkthrough focus) AND DynamicViewFlow's other two
 * callers, ServiceContractView and ScenarioBrowser's test-scenario views,
 * which never drive fragment mode at all — StepBar's own caption already
 * states THIS SAME call's position as "Step N of Total" off its raw global
 * `seq`. Putting a compressed alt label ("3") on the canvas chip while the
 * caption right beneath it reads "Step 22 of 22" would contradict itself over
 * one call. Only fragment mode's FragmentBar caption already prints the alt
 * label alongside the true global position on the SAME row
 * (fragmentRowLabel, fragmentCaption.ts) — so only there does the chip echo
 * it too.
 */
export function seqChipLabel(
  fragmentMode: boolean,
  call: { seq: number; altLabel?: string }
): number | string {
  return fragmentMode ? (call.altLabel ?? call.seq) : call.seq;
}
