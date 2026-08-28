package domain

import "time"

type Consent struct {
	UserSubject   UserID
	ApplicationID ApplicationID
	Scopes        []string
	Remember      bool
	GrantedAt     time.Time
	LastUsedAt    time.Time
}

func (c Consent) Clone() Consent {
	copy := c
	copy.Scopes = append([]string(nil), c.Scopes...)
	return copy
}
