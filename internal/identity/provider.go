package identity

import "context"

type UserSnapshot struct {
	Subject     string `json:"subject"`
	DiscourseID int64  `json:"discourse_id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	TrustLevel  int    `json:"trust_level"`
	Active      bool   `json:"active"`
	Silenced    bool   `json:"silenced"`
	Suspended   bool   `json:"suspended"`
	Reviewer    bool   `json:"connect_reviewer"`
	Admin       bool   `json:"connect_admin"`
}

func (u UserSnapshot) CanAuthenticate() bool {
	return u.Active && !u.Suspended
}

func (u UserSnapshot) CanManageApplications() bool {
	return u.CanAuthenticate() && !u.Silenced && (u.TrustLevel >= 1 || u.Admin)
}

type StatusSnapshot struct {
	Subject    string `json:"subject"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
	TrustLevel int    `json:"trust_level"`
	Active     bool   `json:"active"`
	Silenced   bool   `json:"silenced"`
	Suspended  bool   `json:"suspended"`
	Reviewer   bool   `json:"connect_reviewer"`
	Admin      bool   `json:"connect_admin"`
}

func (s StatusSnapshot) CanAuthenticate() bool {
	return s.Active && !s.Suspended
}

func (s StatusSnapshot) CanManageApplications() bool {
	return s.CanAuthenticate() && !s.Silenced && (s.TrustLevel >= 1 || s.Admin)
}

type Provider interface {
	FetchUser(context.Context, int64) (UserSnapshot, error)
}

type StatusLookup interface {
	CurrentStatus(context.Context, string) (StatusSnapshot, error)
}
