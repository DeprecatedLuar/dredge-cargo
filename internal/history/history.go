package history

import (
	"time"
)

// Version describes one distinct encrypted Git blob, newest-first within a
// path history.
type Version struct {
	CommitID  string
	BlobID    string
	Size      int64
	Timestamp time.Time
}

// Item contains the Git-derived history for one Dredge ID.
type Item struct {
	ID              string
	ItemVersions    []Version
	StorageVersions []Version
	Live            bool
	DeletedAt       *time.Time
}

// Limits constrain retained distinct blobs for one path.
type Limits struct {
	MaxVersions int
	MaxBytes    int64
}

// Policy contains independent item and storage retention limits.
type Policy struct {
	Items     Limits
	Storage   Limits
	RetainFor time.Duration
}

// Retention is the deterministic set of versions retained for one ID.
type Retention struct {
	ID              string
	ItemVersions    []Version
	StorageVersions []Version
	Expired         bool
}

// PlanRetention selects distinct blobs newest-first. The newest blob is
// mandatory for live IDs and for deleted IDs still inside their grace period,
// even when it alone exceeds a configured limit.
func PlanRetention(item Item, policy Policy, now time.Time) Retention {
	expired := !item.Live && (item.DeletedAt == nil ||
		!now.Before(item.DeletedAt.Add(policy.RetainFor)))
	plan := Retention{ID: item.ID, Expired: expired}
	if expired {
		return plan
	}
	plan.ItemVersions = selectVersions(item.ItemVersions, policy.Items)
	plan.StorageVersions = selectVersions(item.StorageVersions, policy.Storage)
	return plan
}

func selectVersions(versions []Version, limits Limits) []Version {
	if len(versions) == 0 {
		return nil
	}
	unique := distinctNewest(versions)
	if len(unique) == 0 {
		return nil
	}

	selected := []Version{unique[0]}
	total := unique[0].Size
	for _, version := range unique[1:] {
		if len(selected) >= limits.MaxVersions || total+version.Size > limits.MaxBytes {
			break
		}
		selected = append(selected, version)
		total += version.Size
	}
	return selected
}

func distinctNewest(versions []Version) []Version {
	seen := make(map[string]bool, len(versions))
	distinct := make([]Version, 0, len(versions))
	for _, version := range versions {
		if seen[version.BlobID] {
			continue
		}
		seen[version.BlobID] = true
		distinct = append(distinct, version)
	}
	return distinct
}
