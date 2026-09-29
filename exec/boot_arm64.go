// Copyright (c) The armory-boot authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package exec

import (
	"errors"
	"fmt"

	"github.com/usbarmory/tamago/dma"
)

// defined in boot_arm64.s
func cleanInvalidateDataCacheRange(addr uint, size uint)
func copyPayload(addr uint, buf []byte)
func currentEL() uint
func exec(kernel uint, params uint)

func boot(kernel uint, params uint, cleanup func(), region *dma.Region) (err error) {
	if level := currentEL(); level != 1 {
		return fmt.Errorf("boot requires EL1, current EL%d", level)
	}

	if region != nil && region.Start()+region.Size() < region.Start() {
		return errors.New("boot memory Region overflows address space")
	}

	if cleanup != nil {
		cleanup()
	}

	if region != nil {
		cleanInvalidateDataCacheRange(region.Start(), region.Size())
	}

	exec(kernel, params)

	return errors.New("boot failure")
}
