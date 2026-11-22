package session

import (
	"reflect"
	"testing"
	"time"
)

// group_order = "actionable" orders sibling groups by their most actionable
// session, subgroups included, without splitting a subtree; "manual" (the
// default) keeps the stored order.
func TestGroupOrder_Actionable(t *testing.T) {
	t.Cleanup(func() { SetGroupOrderMode("manual") })
	now := time.Now()
	instances := []*Instance{
		{ID: "a", GroupPath: "alpha", Status: StatusIdle, LastAccessedAt: now},
		{ID: "b", GroupPath: "bravo", Status: StatusStopped, LastAccessedAt: now},
		{ID: "c", GroupPath: "charlie", Status: StatusStopped, LastAccessedAt: now},
		{ID: "c1", GroupPath: "charlie/one", Status: StatusIdle, LastAccessedAt: now.Add(-time.Hour)},
		{ID: "c2", GroupPath: "charlie/two", Status: StatusWaiting, LastAccessedAt: now.Add(-2 * time.Hour)},
		{ID: "d", GroupPath: "delta", Status: StatusRunning, LastAccessedAt: now.Add(-3 * time.Hour)},
		{ID: "e", GroupPath: "echo", Status: StatusIdle, LastAccessedAt: now.Add(time.Minute)},
	}
	paths := func(tree *GroupTree) []string {
		out := make([]string, 0, len(tree.GroupList))
		for _, g := range tree.GroupList {
			out = append(out, g.Path)
		}
		return out
	}

	SetGroupOrderMode("manual")
	manual := []string{"alpha", "bravo", "charlie", "charlie/one", "charlie/two", "delta", "echo"}
	if got := paths(NewGroupTree(instances)); !reflect.DeepEqual(got, manual) {
		t.Fatalf("manual order = %v, want %v", got, manual)
	}

	SetGroupOrderMode("actionable")
	// charlie leads on its waiting subgroup session, which also leads inside
	// charlie; delta is running; echo and alpha are idle, echo used later.
	actionable := []string{"charlie", "charlie/two", "charlie/one", "delta", "echo", "alpha", "bravo"}
	if got := paths(NewGroupTree(instances)); !reflect.DeepEqual(got, actionable) {
		t.Fatalf("actionable order = %v, want %v", got, actionable)
	}

	SetGroupOrderMode("garbage")
	if got := currentGroupOrderMode(); got != "manual" {
		t.Fatalf("an unknown mode must normalize to manual, got %q", got)
	}
}

// A pinned group (conductor) stays on top however quiet it is.
func TestGroupOrder_ActionableKeepsPinnedGroupsOnTop(t *testing.T) {
	t.Cleanup(func() { SetGroupOrderMode("manual") })
	SetGroupOrderMode("actionable")
	tree := NewGroupTree([]*Instance{
		{ID: "w", GroupPath: "work", Status: StatusError},
		{ID: "c", GroupPath: "conductor", Status: StatusStopped},
	})
	if got := tree.GroupList[0].Path; got != "conductor" {
		t.Fatalf("conductor group must stay first, got %q", got)
	}
}

// group_order = "recent" ignores status: the group whose session was active
// or opened last leads, and an errored group does not jump the queue.
func TestGroupOrder_Recent(t *testing.T) {
	t.Cleanup(func() { SetGroupOrderMode("manual") })
	SetGroupOrderMode("recent")
	now := time.Now()
	stale := &Instance{ID: "x", GroupPath: "errored", Status: StatusError, CreatedAt: now.Add(-48 * time.Hour)}
	opened := &Instance{ID: "o", GroupPath: "opened", Status: StatusIdle, CreatedAt: now.Add(-72 * time.Hour), LastAccessedAt: now.Add(-time.Hour)}
	active := &Instance{ID: "a", GroupPath: "worked/sub", Status: StatusStopped, CreatedAt: now.Add(-72 * time.Hour)}
	active.lastActivityAt = now.Add(-time.Minute)

	tree := NewGroupTree([]*Instance{stale, opened, active})
	got := make([]string, 0, len(tree.GroupList))
	for _, g := range tree.GroupList {
		got = append(got, g.Path)
	}
	want := []string{"worked", "worked/sub", "opened", "errored"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recent order = %v, want %v", got, want)
	}
}
