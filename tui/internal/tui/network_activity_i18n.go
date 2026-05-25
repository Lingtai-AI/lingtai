package tui

import (
	"strings"

	"github.com/anthropics/lingtai-tui/i18n"
)

func networkActivityLabel() string {
	return i18n.T("network_activity.label")
}

func networkActivityShortLabel() string {
	return i18n.T("network_activity.short_label")
}

func networkActivityStatusLabel(status string) string {
	key := "network_activity.status." + strings.ToLower(status)
	label := i18n.T(key)
	if label == key {
		return status
	}
	return label
}
