//go:build !windows && !darwin

package i18n

func systemLocale() string { return "" }
