package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Level 1: Bionic libc and related libs provided by android-ndk
var bionicABI = map[string]bool{
	"libc.so":          true,
	"libm.so":          true,
	"libdl.so":         true,
	"libstdc++.so":     true,
	"libc++.so":        true,
	"libc++_shared.so": true,
}

// Level 2: All Android NDK libraries (plus bionicABI)
var ndkABI = map[string]bool{
	"libaaudio.so":         true,
	"libamidi.so":          true,
	"libandroid.so":        true,
	"libbinder_ndk.so":     true,
	"libcamera2ndk.so":     true,
	"libcutils.so":         true,
	"libEGL.so":            true,
	"libGLESv1_CM.so":      true,
	"libGLESv2.so":         true,
	"libGLESv3.so":         true,
	"libhardware.so":       true,
	"libicu.so":            true,
	"libjnigraphics.so":    true,
	"liblog.so":            true,
	"libmediandk.so":       true,
	"libnativehelper.so":   true,
	"libnativewindow.so":   true,
	"libneuralnetworks.so": true,
	"libOpenMAXAL.so":      true,
	"libOpenSLES.so":       true,
	"libsync.so":           true,
	"libvulkan.so":         true,
	"libz.so":              true,
}

// Level 3 built-in fallback: VNDK libraries
var vndkBuiltinABI = map[string]bool{
	"android.frameworks.cameraservice.common-V1-ndk.so": true,
	"android.frameworks.cameraservice.device-V1-ndk.so": true,
	"android.frameworks.cameraservice.service-V1-ndk.so": true,
	"android.hardware.audio.common@2.0.so":              true,
	"android.hardware.common.fmq-V1-ndk.so":             true,
	"android.hardware.common-V2-ndk.so":                 true,
	"android.hardware.configstore@1.0.so":               true,
	"android.hardware.configstore@1.1.so":               true,
	"android.hardware.configstore-utils.so":             true,
	"android.hardware.confirmationui-support-lib.so":    true,
	"android.hardware.graphics.allocator@2.0.so":        true,
	"android.hardware.graphics.allocator@3.0.so":        true,
	"android.hardware.graphics.allocator@4.0.so":        true,
	"android.hardware.graphics.allocator-V2-ndk.so":     true,
	"android.hardware.graphics.bufferqueue@1.0.so":      true,
	"android.hardware.graphics.bufferqueue@2.0.so":      true,
	"android.hardware.graphics.common@1.0.so":           true,
	"android.hardware.graphics.common@1.1.so":           true,
	"android.hardware.graphics.common@1.2.so":           true,
	"android.hardware.graphics.common-V4-ndk.so":        true,
	"android.hardware.graphics.composer3-V1-ndk.so":     true,
	"android.hardware.graphics.mapper@2.0.so":           true,
	"android.hardware.graphics.mapper@2.1.so":           true,
	"android.hardware.graphics.mapper@3.0.so":           true,
	"android.hardware.graphics.mapper@4.0.so":           true,
	"android.hardware.media@1.0.so":                     true,
	"android.hardware.media.bufferpool@2.0.so":          true,
	"android.hardware.media.omx@1.0.so":                 true,
	"android.hardware.memtrack@1.0.so":                  true,
	"android.hardware.memtrack-V1-ndk.so":               true,
	"android.hardware.renderscript@1.0.so":              true,
	"android.hardware.soundtrigger@2.0-core.so":         true,
	"android.hardware.soundtrigger@2.0.so":              true,
	"android.hidl.memory@1.0-impl.so":                   true,
	"android.hidl.memory@1.0.so":                        true,
	"android.hidl.memory.token@1.0.so":                  true,
	"android.hidl.safe_union@1.0.so":                    true,
	"android.hidl.token@1.0.so":                         true,
	"android.hidl.token@1.0-utils.so":                   true,
	"android.system.suspend@1.0.so":                     true,
	"android.system.suspend-V1-ndk.so":                  true,
	"libandroid_net.so":                                 true,
	"libaudioroute.so":                                  true,
	"libaudioutils.so":                                  true,
	"libbase.so":                                        true,
	"libbcinfo.so":                                      true,
	"libbinder.so":                                      true,
	"libblas.so":                                        true,
	"libbufferqueueconverter.so":                        true,
	"libcamera_metadata.so":                             true,
	"libcap.so":                                         true,
	"libcgrouprc.so":                                    true,
	"libcn-cbor.so":                                     true,
	"libcodec2.so":                                      true,
	"libcom.android.tethering.connectivity_native.so":   true,
	"libcompiler_rt.so":                                 true,
	"libcrypto.so":                                      true,
	"libcrypto_utils.so":                                true,
	"libcurl.so":                                        true,
	"libdiskconfig.so":                                  true,
	"libdmabufheap.so":                                  true,
	"libdumpstateutil.so":                               true,
	"libevent.so":                                       true,
	"libexif.so":                                        true,
	"libexpat.so":                                       true,
	"libfmq.so":                                         true,
	"libft2.so":                                         true,
	"libgatekeeper.so":                                  true,
	"libgralloctypes.so":                                true,
	"libgui.so":                                         true,
	"libhardware_legacy.so":                             true,
	"libhidlallocatorutils.so":                          true,
	"libhidlbase.so":                                    true,
	"libhidlmemory.so":                                  true,
	"libion.so":                                         true,
	"libjpeg.so":                                        true,
	"libjsoncpp.so":                                     true,
	"libldacBT_abr.so":                                  true,
	"libldacBT_enc.so":                                  true,
	"liblz4.so":                                         true,
	"liblzma.so":                                        true,
	"libmedia_helper.so":                                true,
	"libmedia_omx.so":                                   true,
	"libmemtrack.so":                                    true,
	"libminijail.so":                                    true,
	"libmkbootimg_abi_check.so":                         true,
	"libnetutils.so":                                    true,
	"libnl.so":                                          true,
	"libpcre2.so":                                       true,
	"libpiex.so":                                        true,
	"libpng.so":                                         true,
	"libpower.so":                                       true,
	"libprocessgroup.so":                                true,
	"libprocinfo.so":                                    true,
	"libradio_metadata.so":                              true,
	"libRSCpuRef.so":                                    true,
	"libRSDriver.so":                                    true,
	"libRS_internal.so":                                 true,
	"libRS.so":                                          true,
	"libselinux.so":                                     true,
	"libspeexresampler.so":                              true,
	"libsqlite.so":                                      true,
	"libssl.so":                                         true,
	"libstagefright_bufferpool@2.0.so":                  true,
	"libstagefright_bufferqueue_helper.so":              true,
	"libstagefright_foundation.so":                      true,
	"libstagefright_omx.so":                             true,
	"libstagefright_omx_utils.so":                       true,
	"libstagefright_xmlparser.so":                       true,
	"libsysutils.so":                                    true,
	"libtinyalsa.so":                                    true,
	"libtinyxml2.so":                                    true,
	"libui.so":                                          true,
	"libunwindstack.so":                                 true,
	"libusbhost.so":                                     true,
	"libutilscallstack.so":                              true,
	"libutils.so":                                       true,
	"libvndksupport.so":                                 true,
	"libwifi-system-iface.so":                           true,
	"libxml2.so":                                        true,
	"libyuv.so":                                         true,
	"libziparchive.so":                                  true,
}

var (
	vndkCacheLock sync.RWMutex
	vndkCacheMap  = make(map[int]map[string]bool)
)

// scanVndkDir reads all *.so files from a VNDK directory and populates the map.
func scanVndkDir(dir string, m map[string]bool) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".so") {
			m[d.Name()] = true
		}
		return nil
	})
}

// getVndkLibs discovers VNDK libraries for the given apiLevel or returns the built-in list.
func getVndkLibs(apiLevel int) map[string]bool {
	vndkCacheLock.RLock()
	if cached, ok := vndkCacheMap[apiLevel]; ok {
		vndkCacheLock.RUnlock()
		return cached
	}
	vndkCacheLock.RUnlock()

	vndkCacheLock.Lock()
	defer vndkCacheLock.Unlock()

	if cached, ok := vndkCacheMap[apiLevel]; ok {
		return cached
	}

	result := make(map[string]bool)
	// Seed with built-in VNDK libraries
	for k, v := range vndkBuiltinABI {
		result[k] = v
	}

	// Potential SDK/VNDK root directories
	var sdkRoots []string
	if env := os.Getenv("ANDROID_SDK_ROOT"); env != "" {
		sdkRoots = append(sdkRoots, env)
	}
	if env := os.Getenv("ANDROID_HOME"); env != "" {
		sdkRoots = append(sdkRoots, env)
	}
	sdkRoots = append(sdkRoots, "/opt/android-sdk")

	// Possible version folders: if apiLevel > 0, check v<apiLevel> first; otherwise check available v*
	var versionPatterns []string
	if apiLevel > 0 {
		versionPatterns = append(versionPatterns, fmt.Sprintf("v%d", apiLevel))
	}

	for _, root := range sdkRoots {
		vndkBase := filepath.Join(root, "vndk")
		if _, err := os.Stat(vndkBase); err != nil {
			continue
		}

		if len(versionPatterns) > 0 {
			for _, vp := range versionPatterns {
				vDir := filepath.Join(vndkBase, vp)
				if _, err := os.Stat(vDir); err == nil {
					scanVndkDir(vDir, result)
				}
			}
		} else {
			// Find all v* directories under vndkBase
			entries, err := os.ReadDir(vndkBase)
			if err == nil {
				for _, e := range entries {
					if e.IsDir() && strings.HasPrefix(e.Name(), "v") {
						scanVndkDir(filepath.Join(vndkBase, e.Name()), result)
					}
				}
			}
		}
	}

	vndkCacheMap[apiLevel] = result
	return result
}

// parseCustomSkipAbis parses user arguments passed to --skip-abi-custom.
// Supports:
// - /path/to/libfoo.so (uses base name libfoo.so)
// - libfoo.so:/path/to/libfoo.so (uses target libfoo.so)
// - libfoo.so
// - directory path (scans all *.so in directory)
// - comma-separated list of the above
func parseCustomSkipAbis(entries []string) map[string]bool {
	if len(entries) == 0 {
		return nil
	}

	custom := make(map[string]bool)
	for _, raw := range entries {
		items := strings.Split(raw, ",")
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}

			// Format: lib<name>.so:/path/to/lib<name>.so
			if idx := strings.Index(item, ":"); idx > 0 {
				libName := strings.TrimSpace(item[:idx])
				if libName != "" {
					custom[filepath.Base(libName)] = true
				}
				continue
			}

			// Check if item is an existing directory
			if fi, err := os.Stat(item); err == nil && fi.IsDir() {
				scanVndkDir(item, custom)
				continue
			}

			// Otherwise, extract the base library name
			custom[filepath.Base(item)] = true
		}
	}
	return custom
}

// IsSatisfiedLibLevel checks if a library target is provided by the base Android system
// according to the specified skip ABI level (0..3) and any custom skipped libraries.
func IsSatisfiedLibLevel(target string, level int, apiLevel int, custom map[string]bool) bool {
	if custom != nil && custom[target] {
		return true
	}
	switch level {
	case 0:
		return false
	case 1:
		return bionicABI[target]
	case 2:
		return bionicABI[target] || ndkABI[target]
	default: // 3 and above
		return bionicABI[target] || ndkABI[target] || getVndkLibs(apiLevel)[target]
	}
}

// IsSatisfiedLib checks if a library target is provided by the base Android system (default Level 2).
func IsSatisfiedLib(target string) bool {
	return IsSatisfiedLibLevel(target, 2, 0, nil)
}
