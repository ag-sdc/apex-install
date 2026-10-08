package main

import (
	"os"
	"reflect"
	"testing"
)

func TestDeriveShortAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"com.google.android.mediaprovider", "mediaprovider"},
		{"org.blisslabs.ag.ffmpeg.dev", "ffmpeg.dev"},
		{"com.android.runtime", "runtime"},
		{"org.blisslabs.camera", "camera"},
		{"com.shadichy.makeapex", "makeapex"},
		{"standalone", "standalone"},
	}

	for _, tc := range tests {
		actual := deriveShortAlias(tc.input)
		if actual != tc.expected {
			t.Errorf("deriveShortAlias(%q) = %q, want %q", tc.input, actual, tc.expected)
		}
	}
}

func TestParseApexManifestPb(t *testing.T) {
	// Construct protobuf wire bytes manually:
	// Field 1 (name): tag (1<<3 | 2) = 0x0a, length, "com.test.pkg"
	// Field 2 (versionCode): tag (2<<3 | 0) = 0x10, varint 12345 (0xb9, 0x60)
	// Field 3 (versionName): tag (3<<3 | 2) = 0x1a, length, "1.2.3"
	pkgName := "com.test.pkg"
	verName := "1.2.3"
	var buf []byte

	// Tag 1
	buf = append(buf, 0x0a, byte(len(pkgName)))
	buf = append(buf, []byte(pkgName)...)

	// Tag 2
	buf = append(buf, 0x10, 0xb9, 0x60) // 12345 in varint

	// Tag 3
	buf = append(buf, 0x1a, byte(len(verName)))
	buf = append(buf, []byte(verName)...)

	name, verCode, vName := parseApexManifestPb(buf)
	if name != pkgName {
		t.Errorf("parseApexManifestPb name = %q, want %q", name, pkgName)
	}
	if verCode != "12345" {
		t.Errorf("parseApexManifestPb verCode = %q, want %q", verCode, "12345")
	}
	if vName != verName {
		t.Errorf("parseApexManifestPb vName = %q, want %q", vName, verName)
	}
}

func TestPreProcessArgsFilesFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "subcommand_files",
			args:     []string{"apexm", "files", "bin/tool"},
			expected: []string{"apexm", "-F", "bin/tool"},
		},
		{
			name:     "flags_before_subcommand_files",
			args:     []string{"apexm", "-c", "apex.conf", "files", "bin/tool"},
			expected: []string{"apexm", "-c", "apex.conf", "-F", "bin/tool"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()

			os.Args = append([]string{}, tc.args...)
			preProcessArgs()

			if !reflect.DeepEqual(os.Args, tc.expected) {
				t.Errorf("preProcessArgs() = %v, want %v", os.Args, tc.expected)
			}
		})
	}
}
