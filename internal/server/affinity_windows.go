//go:build windows

package server

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modKernel32                          = windows.NewLazySystemDLL("kernel32.dll")
	procSetProcessAffinityMask           = modKernel32.NewProc("SetProcessAffinityMask")
	procGetLogicalProcessorInformationEx = modKernel32.NewProc("GetLogicalProcessorInformationEx")
)

const relationProcessorCore = 0

type systemLogicalProcessorInformationEx struct {
	Relationship uint32
	Size         uint32
}

type processorRelationship struct {
	Flags           byte
	EfficiencyClass byte
	Reserved        [20]byte
	GroupCount      uint16
	// GroupMask follows as flexible array of GROUP_AFFINITY
}

type groupAffinity struct {
	Mask     uintptr
	Group    uint16
	Reserved [3]uint16
}

func applyAffinity(pid int, cpus []int) error {
	if len(cpus) == 0 {
		return nil
	}
	var mask uintptr
	for _, c := range cpus {
		if c < 0 || c >= 64 {
			return fmt.Errorf("CPU %d out of range for SetProcessAffinityMask (0-63)", c)
		}
		mask |= 1 << uint(c)
	}
	if mask == 0 {
		return nil
	}

	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)

	r1, _, e1 := procSetProcessAffinityMask.Call(uintptr(h), mask)
	if r1 == 0 {
		if e1 != windows.ERROR_SUCCESS {
			return fmt.Errorf("SetProcessAffinityMask: %w", e1)
		}
		return fmt.Errorf("SetProcessAffinityMask failed")
	}
	return nil
}

// GetCPUTopology returns HT sibling groups from GetLogicalProcessorInformationEx.
func GetCPUTopology() (CPUTopology, error) {
	pairs, logical, err := windowsProcessorPairs()
	if err != nil || len(pairs) == 0 {
		n := logicalCPUCount()
		return CPUTopology{LogicalCount: n, Pairs: fallbackPairs(n)}, nil
	}
	return CPUTopology{LogicalCount: logical, Pairs: pairs}, nil
}

func windowsProcessorPairs() ([][]int, int, error) {
	if err := procGetLogicalProcessorInformationEx.Find(); err != nil {
		return nil, 0, err
	}

	var size uint32
	r1, _, e1 := procGetLogicalProcessorInformationEx.Call(
		uintptr(relationProcessorCore),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 != 0 || e1 != windows.ERROR_INSUFFICIENT_BUFFER {
		if e1 != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, 0, fmt.Errorf("GetLogicalProcessorInformationEx size: %w", e1)
		}
	}
	if size == 0 {
		return nil, 0, fmt.Errorf("GetLogicalProcessorInformationEx returned empty buffer size")
	}

	buf := make([]byte, size)
	r1, _, e1 = procGetLogicalProcessorInformationEx.Call(
		uintptr(relationProcessorCore),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return nil, 0, fmt.Errorf("GetLogicalProcessorInformationEx: %w", e1)
	}

	var pairs [][]int
	maxCPU := -1
	offset := 0
	for offset+8 <= int(size) {
		hdr := (*systemLogicalProcessorInformationEx)(unsafe.Pointer(&buf[offset]))
		entrySize := int(hdr.Size)
		if entrySize < 8 || offset+entrySize > int(size) {
			break
		}
		if hdr.Relationship == relationProcessorCore {
			rel := (*processorRelationship)(unsafe.Pointer(&buf[offset+8]))
			groupCount := int(rel.GroupCount)
			if groupCount < 1 {
				groupCount = 1
			}
			maskBase := offset + 8 + int(unsafe.Sizeof(processorRelationship{}))
			// On some layouts GroupMask is inline after Reserved; use documented offset:
			// PROCESSOR_RELATIONSHIP is 24 bytes then GROUP_AFFINITY[].
			// Flags(1)+EfficiencyClass(1)+Reserved(20)+GroupCount(2) = 24.
			maskBase = offset + 8 + 24
			var cpus []int
			for g := 0; g < groupCount; g++ {
				gaOff := maskBase + g*int(unsafe.Sizeof(groupAffinity{}))
				if gaOff+int(unsafe.Sizeof(groupAffinity{})) > offset+entrySize {
					break
				}
				ga := (*groupAffinity)(unsafe.Pointer(&buf[gaOff]))
				// Single-group hosts: bit i = logical processor i.
				base := int(ga.Group) * 64
				for bit := 0; bit < 64; bit++ {
					if ga.Mask&(1<<uint(bit)) != 0 {
						cpu := base + bit
						cpus = append(cpus, cpu)
						if cpu > maxCPU {
							maxCPU = cpu
						}
					}
				}
			}
			if len(cpus) > 0 {
				pairs = append(pairs, uniqueSorted(cpus))
			}
		}
		offset += entrySize
	}
	logical := maxCPU + 1
	if logical <= 0 {
		logical = logicalCPUCount()
	}
	return pairs, logical, nil
}
