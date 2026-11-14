package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{10485760, "10.0 MiB"},
		{1073741824, "1.0 GiB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectOpFromArgs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-S", "foo"}, "sync"},
		{[]string{"-Sh"}, "sync"},
		{[]string{"--sync"}, "sync"},
		{[]string{"install", "foo"}, "sync"},
		{[]string{"update"}, "sync"},
		{[]string{"download", "foo"}, "sync"},
		{[]string{"-Q", "foo"}, "query"},
		{[]string{"-Qh"}, "query"},
		{[]string{"--query"}, "query"},
		{[]string{"local", "foo"}, "query"},
		{[]string{"list"}, "query"},
		{[]string{"-R", "foo"}, "remove"},
		{[]string{"-Rh"}, "remove"},
		{[]string{"--remove", "foo"}, "remove"},
		{[]string{"remove", "foo"}, "remove"},
		{[]string{"-U", "foo.apex"}, "upgrade"},
		{[]string{"-Uh"}, "upgrade"},
		{[]string{"--upgrade"}, "upgrade"},
		{[]string{"upgrade", "foo.apex"}, "upgrade"},
		{[]string{"-F", "/bin/ls"}, "files"},
		{[]string{"-Fh"}, "files"},
		{[]string{"--files"}, "files"},
		{[]string{"files"}, "files"},
		{[]string{"--init"}, "init"},
		{[]string{"init"}, "init"},
		{[]string{"-h"}, ""},
		{[]string{"--help"}, ""},
		{[]string{"foo"}, ""},
	}

	for _, tt := range tests {
		got := detectOpFromArgs(tt.args)
		if got != tt.want {
			t.Errorf("detectOpFromArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestIsHelpAndVersionRequested(t *testing.T) {
	helpTests := []struct {
		args []string
		want bool
	}{
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"-Sh"}, true},
		{[]string{"-S", "-h"}, true},
		{[]string{"-Qh"}, true},
		{[]string{"-S", "foo"}, false},
	}

	for _, tt := range helpTests {
		got := isHelpRequested(tt.args)
		if got != tt.want {
			t.Errorf("isHelpRequested(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}

	verTests := []struct {
		args []string
		want bool
	}{
		{[]string{"-V"}, true},
		{[]string{"--version"}, true},
		{[]string{"-v"}, false},
		{[]string{"-S"}, false},
	}

	for _, tt := range verTests {
		got := isVersionRequested(tt.args)
		if got != tt.want {
			t.Errorf("isVersionRequested(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestPacmanProgressReader(t *testing.T) {
	data := []byte(strings.Repeat("Hello Pacman World!", 100))
	buf := bytes.NewReader(data)

	pr := NewPacmanProgressReader(buf, "test-pkg@1.0.0.apex", int64(len(data)))
	dest := new(bytes.Buffer)

	n, err := io.Copy(dest, pr)
	if err != nil {
		t.Fatalf("io.Copy failed: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("Copied bytes = %d, want %d", n, len(data))
	}
	if pr.downloaded != int64(len(data)) {
		t.Errorf("pr.downloaded = %d, want %d", pr.downloaded, len(data))
	}

	// Test with NoProgressBar
	NoProgressBar = true
	defer func() { NoProgressBar = false }()

	buf2 := bytes.NewReader(data)
	pr2 := NewPacmanProgressReader(buf2, "silent-pkg@1.0.0.apex", int64(len(data)))
	dest2 := new(bytes.Buffer)
	_, err = io.Copy(dest2, pr2)
	if err != nil {
		t.Fatalf("silent io.Copy failed: %v", err)
	}
	pr2.Finish()
}

func createTestZip(t *testing.T, path string, files map[string][]byte) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		wf, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to add file %s to zip: %v", name, err)
		}
		if _, err := wf.Write(content); err != nil {
			t.Fatalf("failed to write content for %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
}

func TestInspectLocalPackage(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Standard .apex file with apex_manifest.json
	apexPath := filepath.Join(tmpDir, "com.example.test@42.apex")
	apexManifest := []byte(`{"name": "com.example.test", "version": 42, "versionName": "1.2.3"}`)
	createTestZip(t, apexPath, map[string][]byte{
		"apex_manifest.json": apexManifest,
		"apex_payload.img":   []byte("apex-payload-dummy-data"),
	})

	pkg, err := inspectLocalPackage(apexPath)
	if err != nil {
		t.Fatalf("inspectLocalPackage(apexPath) error: %v", err)
	}
	if pkg.Name != "com.example.test" {
		t.Errorf("pkg.Name = %q, want %q", pkg.Name, "com.example.test")
	}
	if pkg.Version != "1.2.3" {
		t.Errorf("pkg.Version = %q, want %q", pkg.Version, "1.2.3")
	}
	if pkg.IsCapex {
		t.Errorf("pkg.IsCapex = true, want false")
	}

	// 2. CAPEX .capex file with original_apex
	innerApexBuf := new(bytes.Buffer)
	innerZip := zip.NewWriter(innerApexBuf)
	innerManifest, _ := innerZip.Create("apex_manifest.json")
	innerManifest.Write([]byte(`{"name": "com.example.compressed", "version": 100, "versionName": "2.0.0"}`))
	innerPayload, _ := innerZip.Create("apex_payload.img")
	innerPayload.Write([]byte("capex-inner-payload-data"))
	innerZip.Close()

	capexPath := filepath.Join(tmpDir, "com.example.compressed.capex")
	createTestZip(t, capexPath, map[string][]byte{
		"original_apex": innerApexBuf.Bytes(),
	})

	capexPkg, err := inspectLocalPackage(capexPath)
	if err != nil {
		t.Fatalf("inspectLocalPackage(capexPath) error: %v", err)
	}
	if capexPkg.Name != "com.example.compressed" {
		t.Errorf("capexPkg.Name = %q, want %q", capexPkg.Name, "com.example.compressed")
	}
	if capexPkg.Version != "2.0.0" {
		t.Errorf("capexPkg.Version = %q, want %q", capexPkg.Version, "2.0.0")
	}
	if !capexPkg.IsCapex {
		t.Errorf("capexPkg.IsCapex = false, want true")
	}

	// 3. Fallback when manifest is absent
	fallbackPath := filepath.Join(tmpDir, "fallback.pkg-3.4.5.apex")
	createTestZip(t, fallbackPath, map[string][]byte{
		"apex_payload.img": []byte("dummy"),
	})
	fallbackPkg, err := inspectLocalPackage(fallbackPath)
	if err != nil {
		t.Fatalf("inspectLocalPackage(fallbackPath) error: %v", err)
	}
	if fallbackPkg.Name != "fallback.pkg" {
		t.Errorf("fallbackPkg.Name = %q, want %q", fallbackPkg.Name, "fallback.pkg")
	}
	if fallbackPkg.Version != "3.4.5" {
		t.Errorf("fallbackPkg.Version = %q, want %q", fallbackPkg.Version, "3.4.5")
	}
}

