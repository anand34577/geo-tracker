package store

import (
	"context"
	"testing"
)

// A group that shares nothing must not turn another group's approximate sharing exact.
func TestAccessPrecisionAcrossGroups(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sub, _ := s.CreateFirstAdmin(ctx, "s@example.com", "S", "x")
	viewer, _ := s.CreateUser(ctx, "v@example.com", "V", "x", "user")
	share := func(m Member) {
		g, err := s.CreateGroup(ctx, "g", sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.InviteMember(ctx, g, viewer.ID, sub.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateSharing(ctx, g, viewer.ID, Member{Precision: "exact"}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateSharing(ctx, g, sub.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	share(Member{ShareHistoryD: 7, Precision: "approx"})
	share(Member{Precision: "exact"}) // grants nothing
	acc, err := s.AccessTo(ctx, viewer.ID, sub.ID)
	if err != nil || !acc.Approx || acc.Live {
		t.Fatalf("access = %+v, err = %v; want approx history only", acc, err)
	}
}
