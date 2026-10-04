package main

import (
	"os"
	"reflect"
	"testing"
)

func TestIsSatisfiedLibLevel(t *testing.T) {
	// Level 0: None skipped
	for _, lib := range []string{"libc.so", "libm.so", "liblog.so", "libvulkan.so", "libbase.so", "libcustom.so"} {
		if IsSatisfiedLibLevel(lib, 0, 0, nil) {
			t.Errorf("Level 0 should not satisfy %s", lib)
		}
	}

	// Level 1: Only Bionic libc and related NDK libs
	level1Expected := []string{"libc.so", "libm.so", "libdl.so", "libstdc++.so", "libc++.so", "libc++_shared.so"}
	for _, lib := range level1Expected {
		if !IsSatisfiedLibLevel(lib, 1, 0, nil) {
			t.Errorf("Level 1 should satisfy %s", lib)
		}
	}
	level1NotExpected := []string{"liblog.so", "libvulkan.so", "libandroid.so", "libbase.so", "libcustom.so"}
	for _, lib := range level1NotExpected {
		if IsSatisfiedLibLevel(lib, 1, 0, nil) {
			t.Errorf("Level 1 should NOT satisfy %s", lib)
		}
	}

	// Level 2: All Android NDK libs (default)
	level2Expected := []string{"libc.so", "libm.so", "liblog.so", "libvulkan.so", "libandroid.so", "libEGL.so", "libcutils.so"}
	for _, lib := range level2Expected {
		if !IsSatisfiedLibLevel(lib, 2, 0, nil) {
			t.Errorf("Level 2 should satisfy %s", lib)
		}
	}
	level2NotExpected := []string{"libbase.so", "libhidlbase.so", "libutils.so", "libcustom.so"}
	for _, lib := range level2NotExpected {
		if IsSatisfiedLibLevel(lib, 2, 0, nil) {
			t.Errorf("Level 2 should NOT satisfy %s", lib)
		}
	}

	// Level 3: All NDK + VNDK libs
	level3Expected := []string{"libc.so", "liblog.so", "libvulkan.so", "libbase.so", "libhidlbase.so", "libutils.so"}
	for _, lib := range level3Expected {
		if !IsSatisfiedLibLevel(lib, 3, 0, nil) {
			t.Errorf("Level 3 should satisfy %s", lib)
		}
	}
	if IsSatisfiedLibLevel("libcompletely_custom_nonexistent.so", 3, 0, nil) {
		t.Errorf("Level 3 should NOT satisfy unknown library")
	}

	// Backward-compatible IsSatisfiedLib should match Level 2
	if !IsSatisfiedLib("libc.so") || !IsSatisfiedLib("liblog.so") {
		t.Errorf("IsSatisfiedLib should satisfy libc.so and liblog.so")
	}
	if IsSatisfiedLib("libbase.so") {
		t.Errorf("IsSatisfiedLib should not satisfy libbase.so")
	}
}

func TestParseCustomSkipAbis(t *testing.T) {
	entries := []string{
		"/system/lib64/libcustom1.so",
		"libmapped.so:/vendor/lib/libactual.so",
		"libplain.so",
		"libcomma1.so,libcomma2.so,/path/to/libcomma3.so",
	}

	custom := parseCustomSkipAbis(entries)
	if custom == nil {
		t.Fatalf("expected non-nil map from parseCustomSkipAbis")
	}

	expectedLibs := []string{
		"libcustom1.so",
		"libmapped.so",
		"libplain.so",
		"libcomma1.so",
		"libcomma2.so",
		"libcomma3.so",
	}

	for _, lib := range expectedLibs {
		if !custom[lib] {
			t.Errorf("expected custom map to contain %s", lib)
		}
	}

	if custom["libnotthere.so"] {
		t.Errorf("unexpected library in custom map: libnotthere.so")
	}

	// Verify custom override works even at Level 0
	if !IsSatisfiedLibLevel("libcustom1.so", 0, 0, custom) {
		t.Errorf("custom skip should satisfy libcustom1.so even at Level 0")
	}
	if !IsSatisfiedLibLevel("libmapped.so", 0, 0, custom) {
		t.Errorf("custom skip should satisfy libmapped.so even at Level 0")
	}
	if IsSatisfiedLibLevel("libc.so", 0, 0, custom) {
		t.Errorf("Level 0 should NOT satisfy libc.so when not in custom list")
	}
}

func TestPreProcessArgsSkipAbiFlags(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "flags before install with skip-abi-level",
			input:    []string{"apexm", "--skip-abi-level", "1", "install", "foo"},
			expected: []string{"apexm", "--skip-abi-level", "1", "-S", "foo"},
		},
		{
			name:     "flags before install with skip-abi-custom",
			input:    []string{"apexm", "--skip-abi-custom", "/path/to/libfoo.so", "install", "bar"},
			expected: []string{"apexm", "--skip-abi-custom", "/path/to/libfoo.so", "-S", "bar"},
		},
		{
			name:     "flags before search with disable-skip-abis",
			input:    []string{"apexm", "--disable-skip-abis", "search", "baz"},
			expected: []string{"apexm", "--disable-skip-abis", "-S", "-s", "baz"},
		},
		{
			name:     "multiple custom flags before download",
			input:    []string{"apexm", "--skip-abi-custom", "lib1.so", "--skip-abi-custom", "lib2.so", "download", "qux"},
			expected: []string{"apexm", "--skip-abi-custom", "lib1.so", "--skip-abi-custom", "lib2.so", "-S", "-d", "qux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()

			os.Args = append([]string{}, tt.input...)
			preProcessArgs()

			if !reflect.DeepEqual(os.Args, tt.expected) {
				t.Errorf("preProcessArgs() = %v, want %v", os.Args, tt.expected)
			}
		})
	}
}
