// Copyright (c) The armory-boot authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !arm64

package exec

import "github.com/usbarmory/tamago/dma"

func writeELFSegment(region *dma.Region, offset int, segment []byte) (err error) {
	region.Write(region.Start(), offset, segment)
	return
}
