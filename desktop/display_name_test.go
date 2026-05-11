package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPackagedDisplayNameIsATTerm(t *testing.T) {
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName string `json:"productName"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Info.ProductName != "AT Term" {
		t.Fatalf("productName = %q; want AT Term", cfg.Info.ProductName)
	}
	if cfg.OutputFilename != "AT Term" {
		t.Fatalf("outputfilename = %q; want AT Term", cfg.OutputFilename)
	}

	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "<key>CFBundleDisplayName</key>") {
			t.Fatalf("%s missing CFBundleDisplayName", path)
		}
		if !strings.Contains(text, "<string>{{.Info.ProductName}}</string>") {
			t.Fatalf("%s does not derive display name from productName", path)
		}
	}

	index, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "<title>AT Term</title>") {
		t.Fatalf("frontend/index.html title is not AT Term")
	}
}

func TestInstallersUseATTermNameAndIcons(t *testing.T) {
	checks := map[string][]string{
		"../.github/scripts/package-linux-deb.sh": {
			`Name=AT Term`,
			`Exec=AT-Term`,
			`Icon=AT-Term`,
			`/usr/share/icons/hicolor/1024x1024/apps/AT-Term.png`,
		},
		"../.github/scripts/package-macos-dmg.sh": {
			`AT Term.app`,
			`AT Term`,
			`/Applications`,
		},
		"../.github/workflows/build.yml": {
			`APP_ARTIFACT_NAME: 'AT-Term'`,
			`AT-Term-linux-${{ matrix.arch }}.tar.gz`,
			`cp -R "at-term.app" "AT Term.app"`,
			`AT Term.app`,
			`AT Term.exe`,
			`at-term-amd64-installer.exe`,
			`AT-Term-windows-amd64.zip`,
		},
		"build/windows/installer/project.nsi": {
			`MUI_ICON "..\icon.ico"`,
			`MUI_UNICON "..\icon.ico"`,
		},
	}
	for path, wants := range checks {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestDarwinPlistsAllowWebViewRelaySockets(t *testing.T) {
	for _, path := range []string{"build/darwin/Info.plist", "build/darwin/Info.dev.plist"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range []string{
			"<key>NSAppTransportSecurity</key>",
			"<key>NSAllowsLocalNetworking</key>",
			"<key>NSAllowsArbitraryLoadsInWebContent</key>",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %s", path, want)
			}
		}
	}
}
