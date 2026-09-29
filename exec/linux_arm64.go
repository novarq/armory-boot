// Copyright (c) The armory-boot authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package exec

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/usbarmory/tamago/dma"

	arm64image "github.com/u-root/u-root/pkg/boot/image"
	"github.com/u-root/u-root/pkg/dt"
)

// Linux kernel information
// https://docs.kernel.org/arch/arm64/booting.html
const (
	kernelAlignment = 0x200000
	dtbAlignment    = 8
	dtbMaxSize      = 0x200000
	initrdAlignment = 0x40000000
	initrdMaxWindow = 0x800000000
)

// LinuxImage represents a bootable Linux kernel image.
//
// The Region and payloads must not be modified, or accessed by other users
// (e.g. DMA), between Load() and Boot().
type LinuxImage struct {
	// Region is the memory area for image loading, it must not overlap the
	// running program and must be reserved at its start address (see
	// dma.Region.Reserve()).
	Region *dma.Region

	// Kernel is the Linux kernel image.
	Kernel []byte
	// KernelOffset is the Linux kernel offset from Region start address.
	KernelOffset int

	// Image is the kernel parsed by Parse() or Load().
	Image *arm64image.Image

	// DeviceTreeBlob is the Linux kernel dtb file.
	DeviceTreeBlob []byte
	// DeviceTreeBlobOffset is the dtb offset from Region start address.
	DeviceTreeBlobOffset int

	// InitialRamDisk is the Linux kernel initrd file.
	InitialRamDisk []byte
	// InitialRamDiskOffset is the initrd offset from Region start address.
	InitialRamDiskOffset int

	// CmdLine is the Linux kernel command line arguments, it replaces any
	// dtb bootargs when not empty.
	CmdLine string

	// DMA pointers
	entry uint
	dtb   uint
}

type memoryRange struct {
	start uint64
	end   uint64
}

func (r memoryRange) overlaps(other memoryRange) bool {
	return r.start < other.end && other.start < r.end
}

// slice returns the range of size bytes at offset off, if within r.
func (r memoryRange) slice(off int, size uint64) (s memoryRange, ok bool) {
	if off < 0 || uint64(off) > r.end-r.start || size > r.end-r.start-uint64(off) {
		return
	}

	s.start = r.start + uint64(off)
	s.end = s.start + size

	return s, true
}

// deviceTree returns the validated dtb, with CmdLine and initrd location
// applied to its /chosen node when required.
func (image *LinuxImage) deviceTree(initrd memoryRange) (dtb []byte, err error) {
	if len(image.DeviceTreeBlob) > dtbMaxSize {
		return nil, fmt.Errorf("dtb size %#x exceeds %#x", len(image.DeviceTreeBlob), dtbMaxSize)
	}

	fdt, err := dt.ReadFDT(bytes.NewReader(image.DeviceTreeBlob))

	if err != nil {
		return nil, fmt.Errorf("invalid dtb, %v", err)
	}

	if int(fdt.Header.TotalSize) > len(image.DeviceTreeBlob) {
		return nil, fmt.Errorf("invalid dtb size %#x", fdt.Header.TotalSize)
	}

	if len(image.CmdLine) == 0 && len(image.InitialRamDisk) == 0 {
		return image.DeviceTreeBlob, nil
	}

	chosen, ok := fdt.RootNode.LookupChildByName("chosen")

	if !ok {
		return nil, errors.New("dtb has no /chosen node")
	}

	if len(image.CmdLine) > 0 {
		chosen.Update(dt.PropertyString("bootargs", image.CmdLine))
	}

	if len(image.InitialRamDisk) > 0 {
		chosen.Update(dt.PropertyU64("linux,initrd-start", initrd.start))
		chosen.Update(dt.PropertyU64("linux,initrd-end", initrd.end))
	}

	buf := new(bytes.Buffer)

	if _, err = fdt.Write(buf); err != nil {
		return
	}

	if buf.Len() > dtbMaxSize {
		return nil, fmt.Errorf("dtb size %#x exceeds %#x", buf.Len(), dtbMaxSize)
	}

	return buf.Bytes(), nil
}

// Parse parses the Linux kernel image header.
func (image *LinuxImage) Parse() (err error) {
	img, err := arm64image.ParseFromBytes(image.Kernel)

	if err != nil {
		return
	}

	image.Image = img

	return
}

// Load loads a Linux kernel image in memory.
//
// The kernel, dtb and optional initrd are copied only after validating the
// ARM64 Linux boot protocol placement requirements, that no payload overlaps
// another one and that no payload source lies within Region, unless already in
// place. A kernel without image_size is assumed to require 16MB.
//
// The dtb /chosen node is updated with CmdLine and initrd location, if any.
//
// Copies use aligned accesses, supporting Region mapping as Device memory.
func (image *LinuxImage) Load() (err error) {
	image.entry = 0
	image.dtb = 0

	if err = image.Parse(); err != nil {
		return
	}

	if image.Region == nil {
		return errors.New("image memory Region must be assigned")
	}

	size, ok := image.Region.UsedBlocks()[image.Region.Start()]

	if !ok {
		return errors.New("image memory Region must be reserved")
	}

	region := memoryRange{
		start: uint64(image.Region.Start()),
		end:   uint64(image.Region.Start()) + uint64(size),
	}

	if region.end < region.start {
		return errors.New("image memory Region overflows address space")
	}

	header := image.Image.Header
	kernel, ok := region.slice(image.KernelOffset, max(uint64(len(image.Kernel)), header.ImageSize))

	if !ok {
		return errors.New("kernel exceeds image memory Region")
	}

	if kernel.start < header.TextOffset || (kernel.start-header.TextOffset)%kernelAlignment != 0 || kernel.start%4 != 0 {
		return fmt.Errorf("kernel address %#x is not text_offset %#x from a 2MB aligned base", kernel.start, header.TextOffset)
	}

	var initrd memoryRange

	if n := len(image.InitialRamDisk); n > 0 {
		if initrd, ok = region.slice(image.InitialRamDiskOffset, uint64(n)); !ok {
			return errors.New("initrd exceeds image memory Region")
		}

		base := min(kernel.start, initrd.start) &^ (initrdAlignment - 1)

		if max(kernel.end, initrd.end)-base > initrdMaxWindow {
			return errors.New("kernel and initrd must be within a 1GB aligned window of up to 32GB")
		}
	}

	dtb, err := image.deviceTree(initrd)

	if err != nil {
		return
	}

	params, ok := region.slice(image.DeviceTreeBlobOffset, uint64(len(dtb)))

	if !ok {
		return errors.New("dtb exceeds image memory Region")
	}

	if params.start%dtbAlignment != 0 {
		return fmt.Errorf("dtb address %#x is not 8 bytes aligned", params.start)
	}

	if kernel.overlaps(params) || initrd.overlaps(kernel) || initrd.overlaps(params) {
		return errors.New("kernel, dtb and initrd must not overlap")
	}

	payloads := []struct {
		buf         []byte
		destination memoryRange
	}{
		{image.Kernel, kernel},
		{dtb, params},
		{image.InitialRamDisk, initrd},
	}

	for _, payload := range payloads {
		if len(payload.buf) == 0 {
			continue
		}

		_, addr := image.Region.Reserved(payload.buf)
		source := memoryRange{uint64(addr), uint64(addr) + uint64(len(payload.buf))}

		if source.overlaps(region) && source.start != payload.destination.start {
			return errors.New("payload source must not overlap image memory Region")
		}
	}

	for _, payload := range payloads {
		copyPayload(uint(payload.destination.start), payload.buf)
	}

	image.DeviceTreeBlob = dtb
	image.entry = uint(kernel.start)
	image.dtb = uint(params.start)

	return
}

// Entry returns the image entry address.
func (image *LinuxImage) Entry() uint {
	return image.entry
}

// DTB returns the image DTB address.
func (image *LinuxImage) DTB() uint {
	return image.dtb
}

// Boot calls a loaded Linux kernel image.
//
// The cleanup function must stop any Region user other than the CPU (e.g.
// DMA). Region data is then cleaned to the point of coherency and the kernel
// is entered at EL1 with interrupts masked, MMU and caches disabled. The
// platform must satisfy the remaining ARM64 Linux boot protocol requirements
// (e.g. timer and interrupt controller state). Boot does not return on success.
func (image *LinuxImage) Boot(cleanup func()) (err error) {
	if image.entry == 0 {
		return errors.New("Load() kernel before Boot()")
	}

	return boot(image.entry, image.dtb, cleanup, image.Region)
}
