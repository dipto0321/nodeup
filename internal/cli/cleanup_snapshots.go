package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/packages"
	"github.com/dipto0321/nodeup/internal/ui"
)

// newCleanupCmd implements `nodeup packages cleanup` — delete the
// snapshot JSON files that accumulate in <DataDir>/snapshots.
//
// # WHY THIS EXISTS
//
// Snapshots are written by `nodeup upgrade` (one per installed Node
// version) and by `nodeup packages snapshot`, but nothing in nodeup ever
// deleted one. The set therefore only ever grows: one file per
// (manager, Node version) pair the user has ever had installed, plus
// the orphan left behind when an upgrade fails partway or is killed
// mid-flight. Once the cleanup prompt uninstalls an old Node version,
// its snapshot can never be rewritten — the file is pure disk weight.
//
// This command gives the user explicit control: pick the files to
// delete one by one, or delete them all at once.
//
// The migration reports in <DataDir>/reports/ leak the same way (one
// random-suffixed file per migration attempt, never overwritten, never
// deleted), so this command cleans those up too. They're offered
// separately from the snapshots because they're a different kind of
// artifact — a record of what happened, versus a rollback payload.
func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete old snapshot and report files",
		Long: `Delete the snapshot and migration-report files nodeup keeps on disk.

nodeup writes a snapshot of your global npm packages before every upgrade
so it can roll back or replay the migration, and deletes it again once
the migration has replayed the packages onto the new Node version.

What remains are the snapshots a FAILED upgrade left behind — those are
the rollback payload the "interrupted upgrade" warning points at — plus
the ones from Node versions you have since uninstalled. This command
lists what's on disk and lets you choose what to delete.

The upgrade marker (upgrade-in-progress.json) lives outside the
snapshots directory, so it is not listed above. A marker whose snapshot
no longer exists is stale and is cleared automatically; use
--stale-sentinel to clear one explicitly.

Examples:
  nodeup packages cleanup              Select snapshots to delete
  nodeup packages cleanup --all        Delete every snapshot (asks first)
  nodeup packages cleanup --all --yes  Delete every snapshot, no prompt
  nodeup packages cleanup --reports    Clean up migration reports instead
  nodeup packages cleanup --list       Show what would be deleted, delete nothing
  nodeup packages cleanup --stale-sentinel  Clear a stale upgrade marker`,
		RunE: runCleanup,
	}

	cmd.Flags().Bool("all", false, "delete every snapshot instead of selecting individually")
	cmd.Flags().Bool("reports", false, "clean up migration reports instead of snapshots")
	cmd.Flags().Bool("list", false, "list the files that would be deleted and exit")
	cmd.Flags().Bool("yes", false, "skip the confirmation prompt (implies --all)")
	cmd.Flags().Bool("stale-sentinel", false, "clear a stale upgrade marker (one whose snapshot no longer exists) and exit")

	return cmd
}

func runCleanup(cmd *cobra.Command, args []string) error {
	w := writerFromCmd(cmd)

	all, _ := cmd.Flags().GetBool("all")
	reports, _ := cmd.Flags().GetBool("reports")
	listOnly, _ := cmd.Flags().GetBool("list")
	yes, _ := cmd.Flags().GetBool("yes")
	staleSentinel, _ := cmd.Flags().GetBool("stale-sentinel")

	// --stale-sentinel is its own narrow operation: clear the upgrade
	// marker if it's stale, and do nothing else. It exists because the
	// sentinel lives at <DataDir>/upgrade-in-progress.json — OUTSIDE
	// the snapshots and reports directories this command otherwise
	// works on — so without an explicit flag there was no way for the
	// user to dismiss a stale marker by hand.
	//
	// We handle it BEFORE the snapshot/report listing so it works on a
	// machine with no snapshots at all (which is exactly the state a
	// user hits after the snapshot was reclaimed).
	if staleSentinel {
		return clearStaleSentinel(w)
	}

	// --yes implies --all: an unattended run can't answer a
	// multi-select question, so it has to mean "delete them all".
	if yes {
		all = true
	}

	var files []packages.SnapshotFile
	var kind string
	// removeFile is the deletion routine for the kind we're cleaning:
	// DeleteReportFile for reports, DeleteSnapshotFile for snapshots.
	// (Named removeFile rather than delete, which shadows the
	// predeclared builtin.)
	var removeFile func(string) error

	if reports {
		kind = "migration report"
		f, err := packages.ListReportFiles()
		if err != nil {
			return fmt.Errorf("list migration reports: %w", err)
		}
		files = f
		removeFile = packages.DeleteReportFile
	} else {
		kind = "snapshot"
		f, err := packages.ListSnapshotFiles()
		if err != nil {
			return fmt.Errorf("list snapshots: %w", err)
		}
		files = f
		removeFile = packages.DeleteSnapshotFile
	}

	if len(files) == 0 {
		w.Info(fmt.Sprintf("No %s files found.", kind))
		return nil
	}

	w.Info(fmt.Sprintf("%s files on disk (%d):", kind, len(files)))
	for _, f := range files {
		w.Println(fmt.Sprintf("  %s  %s  %s", f.Name, formatBytes(f.Size), f.ModTime.Format("2006-01-02 15:04")))
	}

	if listOnly {
		w.Info("Nothing deleted (--list).")
		return nil
	}

	// Build the selection.
	var selected []packages.SnapshotFile
	switch {
	case all:
		selected = files
	default:
		// Interactive multi-select. The prompt is constructed here
		// (rather than passed in) because this command owns its own
		// interaction; the upgrade command's prompt is built around
		// the cleanup flow it shares with Node version deletion.
		prompt := ui.NewPrompt(w.Mode(), cmd.InOrStdin(), cmd.OutOrStdout())
		options := make([]string, len(files))
		for i, f := range files {
			options[i] = f.Name
		}
		picked, err := prompt.MultiSelect(fmt.Sprintf("Select %ss to delete (blank = none):", kind), options)
		if err != nil {
			return fmt.Errorf("cleanup prompt: %w", err)
		}
		if len(picked) == 0 {
			w.Info("Nothing selected — nothing deleted.")
			return nil
		}
		// Map the picked labels back to files. Labels are the file
		// names, which are unique within the directory.
		byName := make(map[string]packages.SnapshotFile, len(files))
		for _, f := range files {
			byName[f.Name] = f
		}
		for _, name := range picked {
			if f, ok := byName[name]; ok {
				selected = append(selected, f)
			}
		}
	}

	if len(selected) == 0 {
		w.Info("Nothing selected — nothing deleted.")
		return nil
	}

	// Confirm before deleting. --all --yes skips this (the user already
	// said "delete them all, don't ask"); every other path asks.
	if !yes {
		prompt := ui.NewPrompt(w.Mode(), cmd.InOrStdin(), cmd.OutOrStdout())
		ok, err := prompt.Confirm(fmt.Sprintf("Delete %d %s file(s)?", len(selected), kind), false)
		if err != nil {
			return fmt.Errorf("cleanup confirmation: %w", err)
		}
		if !ok {
			w.Info("Cleanup canceled.")
			return nil
		}
	}

	// Delete. A failure on one file doesn't stop the rest — we report
	// every failure at the end so the user knows the full picture.
	var deleted int
	var failures []string
	for _, f := range selected {
		if err := removeFile(f.Path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", f.Name, err))
			continue
		}
		deleted++
		w.Success(fmt.Sprintf("Deleted %s", f.Name))
	}

	w.Info(fmt.Sprintf("Deleted %d of %d %s file(s).", deleted, len(selected), kind))
	if len(failures) > 0 {
		sort.Strings(failures)
		for _, msg := range failures {
			w.Warn(fmt.Sprintf("Failed to delete %s", msg))
		}
		// Return an error so CI scripts notice a partial failure.
		return fmt.Errorf("failed to delete %d of %d %s file(s)", len(failures), len(selected), kind)
	}
	return nil
}

// formatBytes renders a byte count in a human-readable form.
//
// We use the binary (1024-based) units because that's what `ls -h` shows
// on macOS, and what users comparing against `du` will expect.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// clearStaleSentinel implements the `--stale-sentinel` path: it removes
// the upgrade marker IF the snapshot it points at is gone.
//
// We deliberately refuse to clear a LIVE sentinel. A sentinel whose
// snapshot still exists represents a genuine interrupted migration —
// the user's global npm packages may not have made it across to the new
// Node version, and the marker is the only breadcrumb telling them
// how to replay it. Wiping that on request would be data loss, not
// cleanup.
//
// The compare-and-delete primitive (RemoveSentinelIfUnchanged) means we
// cannot clobber a fresh sentinel armed by a concurrent `nodeup upgrade`
// between our read and our delete.
func clearStaleSentinel(w ui.Writer) error {
	s, err := packages.OrphanedSentinel()
	if err != nil {
		return fmt.Errorf("read upgrade marker: %w", err)
	}
	if s == nil {
		w.Info("No upgrade marker found.")
		return nil
	}

	if packages.SentinelSnapshotExists(s) {
		w.Warn(fmt.Sprintf("The upgrade marker still has a snapshot (%s) — that is a genuine interrupted upgrade, not stale state.", s.SnapshotPath))
		w.Warn(fmt.Sprintf("Replay it with: `nodeup packages restore --from %s`", s.SnapshotPath))
		return nil
	}

	removed, rerr := packages.RemoveSentinelIfUnchanged(s)
	if rerr != nil {
		return fmt.Errorf("clear upgrade marker: %w", rerr)
	}
	if !removed {
		// The marker changed under us — a concurrent upgrade armed a
		// fresh one. Don't touch it.
		w.Info("The upgrade marker changed while we were reading it — left it alone.")
		return nil
	}

	w.Success("Cleared the stale upgrade marker.")
	return nil
}
