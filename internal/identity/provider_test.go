package identity

import "testing"

func TestUserSnapshotExposesOnlyApprovedStatusFields(t *testing.T) {
	snapshot := UserSnapshot{
		Subject: "sub_1", DiscourseID: 42, Username: "alice", Name: "Alice",
		AvatarURL: "https://forum.example/avatar.png", TrustLevel: 1,
		Active: true, Silenced: false, Suspended: false,
	}
	if !snapshot.CanAuthenticate() || !snapshot.CanManageApplications() {
		t.Fatal("active TL1 user should authenticate and manage applications")
	}
	snapshot.Silenced = true
	if snapshot.CanManageApplications() || !snapshot.CanAuthenticate() {
		t.Fatal("silenced user should authenticate but not manage applications")
	}
	snapshot.Suspended = true
	if snapshot.CanAuthenticate() {
		t.Fatal("suspended user should not authenticate")
	}
}
