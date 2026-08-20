//go:build !darwin

package daemon

import "fmt"

var errLaunchdMacOnly = fmt.Errorf("launchd install is only supported on macOS")

func LaunchdPlistPath() string { return "" }

func IsLaunchdInstalled() bool { return false }

func InstallLaunchd(interval *int) (string, error) {
	return "", errLaunchdMacOnly
}

func UninstallLaunchd() (bool, error) {
	return false, errLaunchdMacOnly
}
