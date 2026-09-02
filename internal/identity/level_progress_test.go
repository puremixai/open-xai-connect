package identity

import (
	"strings"
	"testing"
	"time"
)

func validLevelProgressSnapshot() LevelProgressSnapshot {
	requirement := LevelRequirement{
		Key: "activity", Label: "Activity", Group: "usage", Scope: "account_lifetime",
		Current: 5, Target: 10, Operator: "at_least", Unit: "count", Met: false,
	}
	periodDays := 30
	requirement.PeriodDays = &periodDays
	return LevelProgressSnapshot{
		SchemaVersion:      1,
		DiscourseID:        42,
		CurrentLevel:       LevelInfo{ID: 1, Key: "basic", Label: "基础用户"},
		NextLevel:          &LevelInfo{ID: 2, Key: "member", Label: "成员"},
		PromotionMode:      "automatic",
		RequirementsMet:    boolPtr(false),
		Requirements:       []LevelRequirement{requirement},
		BlockingConditions: []LevelBlockingCondition{{Key: "blocked", Label: "Blocked", Met: false}},
		GeneratedAt:        time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	}
}

func boolPtr(v bool) *bool { return &v }

func TestLevelProgressSnapshotValidateFor(t *testing.T) {
	t.Run("valid snapshot", func(t *testing.T) {
		if err := validLevelProgressSnapshot().ValidateFor(42); err != nil {
			t.Fatalf("ValidateFor() error = %v", err)
		}
	})

	tests := []struct {
		name    string
		mutate  func(*LevelProgressSnapshot)
		wantErr string
	}{
		{
			name:    "schema version",
			mutate:  func(s *LevelProgressSnapshot) { s.SchemaVersion = 2 },
			wantErr: "schema version",
		},
		{
			name:    "discourse id",
			mutate:  func(s *LevelProgressSnapshot) { s.DiscourseID = 99 },
			wantErr: "discourse id mismatch",
		},
		{
			name:    "current level low",
			mutate:  func(s *LevelProgressSnapshot) { s.CurrentLevel.ID = -1 },
			wantErr: "current level",
		},
		{
			name:    "current level high",
			mutate:  func(s *LevelProgressSnapshot) { s.CurrentLevel.ID = 5 },
			wantErr: "out of range",
		},
		{
			name:    "next level mismatch",
			mutate:  func(s *LevelProgressSnapshot) { s.NextLevel.ID = 3 },
			wantErr: "does not match",
		},
		{
			name:    "unknown promotion mode",
			mutate:  func(s *LevelProgressSnapshot) { s.PromotionMode = "surprise" },
			wantErr: "unknown promotion mode",
		},
		{
			name: "tl3 to tl4 rejects automatic promotion",
			mutate: func(s *LevelProgressSnapshot) {
				s.CurrentLevel = LevelInfo{ID: 3, Key: "pro", Label: "进阶用户"}
				s.NextLevel = &LevelInfo{ID: 4, Key: "expert", Label: "专家用户"}
				s.PromotionMode = "automatic"
				s.RequirementsMet = boolPtr(false)
			},
			wantErr: "must use manual or locked promotion mode",
		},
		{
			name: "manual promotion on automatic transition",
			mutate: func(s *LevelProgressSnapshot) {
				s.PromotionMode = "manual"
				s.RequirementsMet = nil
			},
			wantErr: "must use automatic or locked promotion mode",
		},
		{
			name:    "automatic without requirements met",
			mutate:  func(s *LevelProgressSnapshot) { s.RequirementsMet = nil },
			wantErr: "requires requirements_met",
		},
		{
			name:    "too many requirements",
			mutate:  func(s *LevelProgressSnapshot) { s.Requirements = make([]LevelRequirement, 65) },
			wantErr: "max is 64",
		},
		{
			name: "negative metric values",
			mutate: func(s *LevelProgressSnapshot) {
				s.Requirements[0].Current = -1
			},
			wantErr: "current value must be non-negative",
		},
		{
			name: "invalid scope",
			mutate: func(s *LevelProgressSnapshot) {
				s.Requirements[0].Scope = "session"
			},
			wantErr: "unsupported scope",
		},
		{
			name: "invalid operator",
			mutate: func(s *LevelProgressSnapshot) {
				s.Requirements[0].Operator = "greater_than"
			},
			wantErr: "unsupported operator",
		},
		{
			name: "invalid unit",
			mutate: func(s *LevelProgressSnapshot) {
				s.Requirements[0].Unit = "points"
			},
			wantErr: "unsupported unit",
		},
		{
			name: "missing key",
			mutate: func(s *LevelProgressSnapshot) {
				s.Requirements[0].Key = ""
			},
			wantErr: "must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validLevelProgressSnapshot()
			tt.mutate(&snapshot)
			if err := snapshot.ValidateFor(42); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateFor() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLevelProgressSnapshotValidateForLevel4OmitsNextLevel(t *testing.T) {
	snapshot := validLevelProgressSnapshot()
	snapshot.CurrentLevel.ID = 4
	snapshot.CurrentLevel.Key = "expert"
	snapshot.CurrentLevel.Label = "专家用户"
	snapshot.NextLevel = nil
	snapshot.RequirementsMet = nil
	snapshot.PromotionMode = "none"
	if err := snapshot.ValidateFor(42); err != nil {
		t.Fatalf("ValidateFor() error = %v", err)
	}
}

func TestLevelProgressSnapshotValidateForRejectsNextLevelOnLevel4(t *testing.T) {
	snapshot := validLevelProgressSnapshot()
	snapshot.CurrentLevel.ID = 4
	snapshot.CurrentLevel.Key = "expert"
	snapshot.CurrentLevel.Label = "专家用户"
	snapshot.PromotionMode = "none"
	snapshot.RequirementsMet = nil
	if err := snapshot.ValidateFor(42); err == nil || !strings.Contains(err.Error(), "level 4 must not include next_level") {
		t.Fatalf("ValidateFor() error = %v", err)
	}
}

func TestLevelProgressSnapshotValidateForLevel4RequiresNonePromotion(t *testing.T) {
	snapshot := validLevelProgressSnapshot()
	snapshot.CurrentLevel = LevelInfo{ID: 4, Key: "expert", Label: "专家用户"}
	snapshot.NextLevel = nil
	snapshot.PromotionMode = "locked"
	snapshot.RequirementsMet = nil
	if err := snapshot.ValidateFor(42); err == nil || !strings.Contains(err.Error(), "level 4 must use none promotion mode") {
		t.Fatalf("ValidateFor() error = %v", err)
	}
}

func TestLevelProgressSnapshotValidateForAllowsManualTl3ToTl4(t *testing.T) {
	snapshot := validLevelProgressSnapshot()
	snapshot.CurrentLevel = LevelInfo{ID: 3, Key: "pro", Label: "进阶用户"}
	snapshot.NextLevel = &LevelInfo{ID: 4, Key: "expert", Label: "专家用户"}
	snapshot.PromotionMode = "manual"
	snapshot.RequirementsMet = nil
	if err := snapshot.ValidateFor(42); err != nil {
		t.Fatalf("ValidateFor() error = %v", err)
	}
}

func TestLevelProgressSnapshotValidateForAllowsLockedNonTerminalTransitions(t *testing.T) {
	tests := []struct {
		name    string
		current LevelInfo
		next    LevelInfo
	}{
		{
			name:    "locked tl2 to tl3",
			current: LevelInfo{ID: 2, Key: "member", Label: "成员"},
			next:    LevelInfo{ID: 3, Key: "regular", Label: "常规"},
		},
		{
			name:    "locked tl3 to tl4",
			current: LevelInfo{ID: 3, Key: "regular", Label: "常规"},
			next:    LevelInfo{ID: 4, Key: "leader", Label: "领袖"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validLevelProgressSnapshot()
			snapshot.CurrentLevel = tt.current
			snapshot.NextLevel = &tt.next
			snapshot.PromotionMode = "locked"
			snapshot.RequirementsMet = nil
			if err := snapshot.ValidateFor(42); err != nil {
				t.Fatalf("ValidateFor() error = %v", err)
			}
		})
	}
}
