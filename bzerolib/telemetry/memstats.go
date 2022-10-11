package telemetry

import "runtime"

type MemoryStats struct {
	// ref: https://pkg.go.dev/runtime#MemStats
	// Alloc is bytes of allocated heap objects.
	//
	// This is the same as HeapAlloc (see below).
	Alloc uint64 `json:"alloc"`

	// TotalAlloc is cumulative bytes allocated for heap objects.
	//
	// TotalAlloc increases as heap objects are allocated, but
	// unlike Alloc and HeapAlloc, it does not decrease when
	// objects are freed.
	TotalAlloc uint64 `json:"totalAlloc"`

	// Sys is the total bytes of memory obtained from the OS.
	//
	// Sys is the sum of the XSys fields below. Sys measures the
	// virtual address space reserved by the Go runtime for the
	// heap, stacks, and other internal data structures. It's
	// likely that not all of the virtual address space is backed
	// by physical memory at any given moment, though in general
	// it all was at some point.
	Sys uint64 `json:"sys"`

	// Mallocs is the cumulative count of heap objects allocated.
	// The number of live objects is Mallocs - Frees.
	Mallocs uint64 `json:"mallocs"`

	// Frees is the cumulative count of heap objects freed.
	Frees uint64 `json:"frees"`

	// Below not a part of the golang Memory stats

	// Mallocs - Frees
	LiveObjects uint64 `json:"liveObjects"`

	// Another helpful stat provided for free by golang
	// Total number of go routines in the entire process
	NumGoRoutines int `json:"numGoRoutines"`
}

func GetMemoryStats() MemoryStats {
	// Read our current memory statistics
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return MemoryStats{
		Alloc:         mem.Alloc,
		TotalAlloc:    mem.TotalAlloc,
		Sys:           mem.Sys,
		Mallocs:       mem.Mallocs,
		Frees:         mem.Frees,
		LiveObjects:   mem.Mallocs - mem.Frees,
		NumGoRoutines: runtime.NumGoroutine(),
	}
}
