# v3 development plan

The [architecture guide](architecture.md) describes the current foundation and
extension points. The [original plan](archive/rewrite-plan.md) and
[early evaluation](archive/rewrite-plan-evaluation.md) are historical snapshots;
their unchecked tasks and stack descriptions are not current project status.

The foundation audit covers bulk-job atomicity, atomic default-filter updates,
deployment prefixes, recoverable startup/query failures, targeted invalidation,
cancellable subscriptions, compatibility validation, module boundaries,
accessibility guardrails, and source/documentation cleanup.

For each new feature:

1. Identify the existing extension point and propose changes to user-visible
   behavior. Preserve the global pinch/double-tap zoom policy unless explicitly
   approved otherwise.
2. Maintain existing v2.5 client behavior and data representations. v3 route
   names may evolve independently. Put additional Go logic in fork-owned files.
3. Cover changed contracts with regression tests and run the relevant validation
   targets. Test media gestures and native fullscreen on target browsers when
   changing playback behavior.
