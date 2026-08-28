package domain

import "time"

type User struct {
	Subject     UserID
	DiscourseID int64
	Username    string
	Name        string
	AvatarURL   string
	TrustLevel  int
	Active      bool
	Silenced    bool
	Suspended   bool
	Reviewer    bool
	Admin       bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (u User) CanManageApplications() bool {
	return u.Active && !u.Silenced && !u.Suspended && u.TrustLevel >= 1
}

func (u User) CanAuthenticate() bool {
	return u.Active && !u.Suspended
}
