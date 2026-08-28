package web

import (
	"strings"
	"unicode"
)

// Layout carries the small amount of identity and navigation state shared by
// authenticated Portal pages. It intentionally contains no credential data.
type Layout struct {
	Active      string
	DisplayName string
	Username    string
	AvatarURL   string
	Subject     string
	TrustLevel  int
	IsReviewer  bool
	IsAdmin     bool
	CSRFToken   string
}

func (l Layout) UserLabel() string {
	for _, value := range []string{l.DisplayName, l.Username, l.Subject} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "Connect 用户"
}

func (l Layout) UserInitial() string {
	value := []rune(l.UserLabel())
	if len(value) == 0 {
		return "C"
	}
	return string([]rune{unicode.ToUpper(value[0])})
}
