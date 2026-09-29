// Copyright (c) The armory-boot authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package exec

import (
	"errors"

	"github.com/usbarmory/tamago/dma"
)

// writeELFSegment has the Region.Write bounds, with aligned accesses safe for
// Device memory.
func writeELFSegment(region *dma.Region, offset int, segment []byte) (err error) {
	size, ok := region.UsedBlocks()[region.Start()]

	if !ok || offset < 0 || uint(offset) > size || uint(len(segment)) > size-uint(offset) {
		return errors.New("ELF segment exceeds reserved image memory Region")
	}

	copyPayload(region.Start()+uint(offset), segment)

	return
}
