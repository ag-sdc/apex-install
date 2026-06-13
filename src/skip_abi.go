package main

// Stable ABI libraries (NDK stable)
var stableABI = map[string]bool{
	"libandroid.so": true, "libcamera2ndk.so": true, "libc.so": true,
	"libdl.so": true, "libEGL.so": true, "libGLESv1_CM.so": true,
	"libGLESv2.so": true, "libGLESv3.so": true, "libicu.so": true,
	"libjnigraphics.so": true, "liblog.so": true, "libmediandk.so": true,
	"libm.so": true, "libnativewindow.so": true, "libOpenMAXAL.so": true,
	"libOpenSLES.so": true, "libstdc++.so": true, "libsync.so": true,
	"libvulkan.so": true, "libz.so": true, "libaaudio.so": true,
	"libamidi.so": true, "libbinder_ndk.so": true, "libneuralnetworks.so": true,
}

// Unstable ABI libraries (System or internal use)
var unstableABI = map[string]bool{
	"libc++.so": true, "libc++_shared.so": true, "libcutils.so": true,
	"libhardware.so": true, "libnativehelper.so": true,
}

// IsSatisfiedLib checks if a library target is provided by the base Android system.
func IsSatisfiedLib(target string) bool {
	return stableABI[target] || unstableABI[target]
}
