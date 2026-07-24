package commands

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DeprecatedLuar/dredge-cargo/internal/config"
	"github.com/DeprecatedLuar/dredge-cargo/internal/git"
	historymodel "github.com/DeprecatedLuar/dredge-cargo/internal/history"
	"github.com/DeprecatedLuar/dredge-cargo/internal/storage"
)

const historyTimeFormat = "2006-01-02 15:04:05Z"

// HandleHistory inspects encrypted Git objects without decrypting them.
func HandleHistory(args []string) error {
	vaultDir, err := storage.GetDredgeDir()
	if err != nil {
		return fmt.Errorf("failed to get dredge directory: %w", err)
	}
	items, err := git.ReadHistory(vaultDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(vaultDir)
	if err != nil {
		return err
	}
	policy := historyPolicy(cfg)
	now := time.Now().UTC()

	if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
		printHistorySummary(items, policy)
		return nil
	}
	if len(args) == 1 && args[0] == "deleted" {
		printDeletedHistory(items, policy, now)
		return nil
	}
	if len(args) == 1 {
		return printItemHistory(items, args[0], policy, now)
	}
	return fmt.Errorf("usage: dredge history [list|deleted|<id>]")
}

func historyPolicy(cfg config.Config) historymodel.Policy {
	return historymodel.Policy{
		Items: historymodel.Limits{
			MaxVersions: cfg.History.Items.MaxVersions,
			MaxBytes:    cfg.History.Items.MaxBytesPerItem,
		},
		Storage: historymodel.Limits{
			MaxVersions: cfg.History.Storage.MaxVersions,
			MaxBytes:    cfg.History.Storage.MaxBytesPerItem,
		},
		RetainFor: cfg.History.Deleted.RetainFor,
	}
}

func printHistorySummary(items []historymodel.Item, policy historymodel.Policy) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tSTATUS\tITEM VERSIONS\tSTORAGE VERSIONS\tEXPIRATION")
	for _, item := range items {
		status := historyStatus(item)
		expiration := "-"
		if item.DeletedAt != nil {
			expiration = item.DeletedAt.Add(policy.RetainFor).UTC().Format(historyTimeFormat)
		}
		fmt.Fprintf(writer, "%s\t%s\t%d\t%d\t%s\n",
			item.ID, status, len(item.ItemVersions), len(item.StorageVersions), expiration)
	}
	writer.Flush()
}

func printDeletedHistory(items []historymodel.Item, policy historymodel.Policy, now time.Time) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tDELETED\tEXPIRES\tITEM VERSIONS\tSTORAGE VERSIONS")
	count := 0
	for _, item := range items {
		if item.Live || item.DeletedAt == nil {
			continue
		}
		plan := historymodel.PlanRetention(item, policy, now)
		if plan.Expired || len(plan.ItemVersions) == 0 {
			continue
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%d\n",
			item.ID,
			item.DeletedAt.UTC().Format(historyTimeFormat),
			item.DeletedAt.Add(policy.RetainFor).UTC().Format(historyTimeFormat),
			len(plan.ItemVersions),
			len(plan.StorageVersions),
		)
		count++
	}
	if count == 0 {
		fmt.Fprintln(writer, "(no retained deleted items)")
	}
	writer.Flush()
}

func printItemHistory(items []historymodel.Item, id string, policy historymodel.Policy, now time.Time) error {
	var found *historymodel.Item
	for index := range items {
		if items[index].ID == id {
			found = &items[index]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no Git history found for %q", id)
	}

	plan := historymodel.PlanRetention(*found, policy, now)
	retainedItems := blobSet(plan.ItemVersions)
	retainedStorage := blobSet(plan.StorageVersions)
	fmt.Printf("%s: %s\n", found.ID, historyStatus(*found))
	if found.DeletedAt != nil {
		fmt.Printf("Deleted: %s\n", found.DeletedAt.UTC().Format(historyTimeFormat))
		fmt.Printf("Expires: %s\n", found.DeletedAt.Add(policy.RetainFor).UTC().Format(historyTimeFormat))
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "PATH\tTIMESTAMP\tSIZE\tBLOB\tCOMMIT\tPOLICY")
	printVersions := func(pathName string, versions []historymodel.Version, retained map[string]bool) {
		for _, version := range versions {
			disposition := "drop"
			if retained[version.BlobID] {
				disposition = "retain"
			}
			fmt.Fprintf(writer, "%s\t%s\t%d\t%s\t%s\t%s\n",
				pathName,
				version.Timestamp.UTC().Format(historyTimeFormat),
				version.Size,
				shortObjectID(version.BlobID),
				shortObjectID(version.CommitID),
				disposition,
			)
		}
	}
	printVersions("items/"+found.ID, found.ItemVersions, retainedItems)
	printVersions("storage/"+found.ID, found.StorageVersions, retainedStorage)
	writer.Flush()
	return nil
}

func historyStatus(item historymodel.Item) string {
	switch {
	case item.Live:
		return "live"
	case item.DeletedAt != nil:
		return "deleted"
	default:
		return "orphaned"
	}
}

func blobSet(versions []historymodel.Version) map[string]bool {
	set := make(map[string]bool, len(versions))
	for _, version := range versions {
		set[version.BlobID] = true
	}
	return set
}

func shortObjectID(id string) string {
	const shortLength = 12
	if len(id) <= shortLength {
		return id
	}
	return strings.Clone(id[:shortLength])
}
