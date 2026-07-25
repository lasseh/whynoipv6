// Package crawler is the scanning daemon's engine room
// (04-lifecycle-scheduling.md §12–§15, 03-state-machine.md §5–§16): the
// claim loop that leases due domains, the per-domain slot body that runs
// the checker and maps observations, and the commit machine that turns one
// scan into the next state under the lease fence.
//
// The files: frontier.go — claim loop and slot pool; worker.go — per-domain
// slot body; commit.go — the pure state machine (ComputeCommit) plus the
// fenced flush (Committer); schedule.go — lane selection and cadence bands;
// tick.go — the daily tick; coordinator.go — the singleton schedules;
// sweep.go — the lifecycle sweep; resourcesweep.go — the resource-host
// sweep; livecheck.go — the check-job consumers and reaper; metrics.go —
// run checkpoints; configfrom.go — registry binding.
package crawler
