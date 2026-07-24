package history

import (
	"testing"
	"time"
)

func TestPlanRetentionCountsDistinctBlobsAndAppliesLimits(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	versions := []Version{
		{CommitID: "c4", BlobID: "new", Size: 6, Timestamp: now},
		{CommitID: "c3", BlobID: "new", Size: 6, Timestamp: now.Add(-time.Hour)},
		{CommitID: "c2", BlobID: "middle", Size: 4, Timestamp: now.Add(-2 * time.Hour)},
		{CommitID: "c1", BlobID: "old", Size: 1, Timestamp: now.Add(-3 * time.Hour)},
	}
	item := Item{ID: "abc", Live: true, ItemVersions: versions, StorageVersions: versions}
	plan := PlanRetention(item, Policy{
		Items:   Limits{MaxVersions: 3, MaxBytes: 10},
		Storage: Limits{MaxVersions: 2, MaxBytes: 7},
	}, now)

	assertBlobs(t, plan.ItemVersions, "new", "middle")
	assertBlobs(t, plan.StorageVersions, "new")
}

func TestPlanRetentionAlwaysKeepsMandatoryNewestBlob(t *testing.T) {
	now := time.Now().UTC()
	item := Item{
		ID:   "abc",
		Live: true,
		ItemVersions: []Version{
			{BlobID: "oversized", Size: 100, Timestamp: now},
			{BlobID: "old", Size: 1, Timestamp: now.Add(-time.Hour)},
		},
	}
	plan := PlanRetention(item, Policy{
		Items: Limits{MaxVersions: 1, MaxBytes: 5},
	}, now)
	assertBlobs(t, plan.ItemVersions, "oversized")
}

func TestPlanRetentionDeletedGraceAndExpiration(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	deletedAt := now.Add(-6 * 24 * time.Hour)
	item := Item{
		ID:        "gone",
		DeletedAt: &deletedAt,
		ItemVersions: []Version{
			{BlobID: "recovery", Size: 20, Timestamp: deletedAt.Add(-time.Hour)},
		},
	}
	policy := Policy{
		Items:     Limits{MaxVersions: 1, MaxBytes: 1},
		Storage:   Limits{MaxVersions: 1, MaxBytes: 1},
		RetainFor: 7 * 24 * time.Hour,
	}
	inside := PlanRetention(item, policy, now)
	if inside.Expired {
		t.Fatal("item inside grace period marked expired")
	}
	assertBlobs(t, inside.ItemVersions, "recovery")

	atBoundary := PlanRetention(item, policy, deletedAt.Add(policy.RetainFor))
	if !atBoundary.Expired || len(atBoundary.ItemVersions) != 0 {
		t.Fatalf("item at expiration boundary retained: %+v", atBoundary)
	}
}

func TestPlanRetentionUsesCallerClock(t *testing.T) {
	deletedAt := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	item := Item{ID: "gone", DeletedAt: &deletedAt, ItemVersions: []Version{{BlobID: "x"}}}
	policy := Policy{Items: Limits{MaxVersions: 1, MaxBytes: 1}, RetainFor: time.Hour}
	if PlanRetention(item, policy, deletedAt.Add(30*time.Minute)).Expired {
		t.Fatal("caller-supplied time was not used")
	}
}

func TestPlanRetentionUsesHistoryOrderDespiteClockSkew(t *testing.T) {
	item := Item{
		ID:   "abc",
		Live: true,
		ItemVersions: []Version{
			{BlobID: "topological-newest", Size: 1, Timestamp: time.Unix(1, 0)},
			{BlobID: "clock-newest", Size: 1, Timestamp: time.Unix(2, 0)},
		},
	}
	plan := PlanRetention(item, Policy{
		Items: Limits{MaxVersions: 1, MaxBytes: 10},
	}, time.Now())
	assertBlobs(t, plan.ItemVersions, "topological-newest")
}

func assertBlobs(t *testing.T, versions []Version, want ...string) {
	t.Helper()
	if len(versions) != len(want) {
		t.Fatalf("got %d versions, want %d: %+v", len(versions), len(want), versions)
	}
	for i := range want {
		if versions[i].BlobID != want[i] {
			t.Fatalf("version %d blob = %q, want %q", i, versions[i].BlobID, want[i])
		}
	}
}
