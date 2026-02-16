package vectorstore

// CGO linker directives for LanceDB native library.
// The pre-built shared library lives at gateway/lib/linux_amd64/liblancedb_go.so
// and the C headers at gateway/include/lancedb.h.
//
// These flags tell the Go linker where to find the native symbols
// (simple_lancedb_connect, simple_lancedb_init, etc.) at build time.
// At runtime, LD_LIBRARY_PATH or rpath must include the lib directory.

// #cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../../../lib/linux_amd64 -llancedb_go -Wl,-rpath,${SRCDIR}/../../../lib/linux_amd64
// #cgo linux,amd64 CFLAGS: -I${SRCDIR}/../../../include
// #cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../../../lib/darwin_amd64 -llancedb_go -Wl,-rpath,${SRCDIR}/../../../lib/darwin_amd64
// #cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../../../lib/darwin_arm64 -llancedb_go -Wl,-rpath,${SRCDIR}/../../../lib/darwin_arm64
import "C"
