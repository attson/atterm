//go:build linux

package main

import "os"

func configurePlatformEnvironment() {
	applyLinuxWebKitEnvironment(os.LookupEnv, os.Setenv)
}
