// Copyright (c) The armory-boot authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "textflag.h"

// func copyPayload(addr uint, buf []byte)
TEXT ·copyPayload(SB),NOSPLIT|NOFRAME,$0-32
	MOVD	addr+0(FP), R0
	MOVD	buf_base+8(FP), R1
	MOVD	buf_len+16(FP), R2
	CMP	R0, R1
	BEQ	copy_done

	// Device memory requires aligned accesses, including the last bytes.
	ORR	R0, R1, R3
	TST	$7, R3
	BNE	copy_bytes

copy_words:
	CMP	$8, R2
	BLO	copy_bytes
	MOVD.P	8(R1), R3
	MOVD.P	R3, 8(R0)
	SUB	$8, R2
	B	copy_words

copy_bytes:
	CBZ	R2, copy_done
	MOVBU.P	1(R1), R3
	MOVB.P	R3, 1(R0)
	SUB	$1, R2
	B	copy_bytes

copy_done:
	RET

// func cleanInvalidateDataCacheRange(addr uint, size uint)
TEXT ·cleanInvalidateDataCacheRange(SB),NOSPLIT|NOFRAME,$0-16
	MOVD	addr+0(FP), R0
	MOVD	size+8(FP), R1
	CBZ	R1, clean_done

	// CTR_EL0.DminLine is log2(words per data-cache line).
	MRS	CTR_EL0, R2
	LSR	$16, R2, R2
	AND	$0xf, R2, R2
	MOVD	$4, R3
	LSL	R2, R3, R3

	ADD	R0, R1, R6
	SUB	$1, R3, R4
	BIC	R4, R0, R5

clean_loop:
	DC	CIVAC, R5
	ADD	R3, R5, R5
	CMP	R6, R5
	BLO	clean_loop

clean_done:
	DSB	$0xf
	RET

// func currentEL() uint
TEXT ·currentEL(SB),NOSPLIT|NOFRAME,$0-8
	MRS	CurrentEL, R0
	LSR	$2, R0, R0
	MOVD	R0, ret+0(FP)
	RET

// func exec(kernel uint, params uint)
TEXT ·exec(SB),NOSPLIT|NOFRAME,$0-16
	MOVD	kernel+0(FP), R16
	MOVD	params+8(FP), R17

	// Mask asynchronous exceptions and discard stale loaded instructions.
	MSR	$0b1111, DAIFSet
	DSB	$0xf
	SYS	$0x7500 // IC IALLU
	DSB	$0xf
	ISB	$0xf

	// Disable instruction cache, data cache, alignment checks, and translation.
	MRS	SCTLR_EL1, R4
	BIC	$1<<12, R4
	BIC	$1<<2, R4
	BIC	$1<<1, R4
	BIC	$1<<0, R4
	MSR	R4, SCTLR_EL1
	ISB	$0xf
	TLBI	VMALLE1
	DSB	$0xf
	ISB	$0xf

	MOVD	R17, R0
	MOVD	$0, R1
	MOVD	$0, R2
	MOVD	$0, R3

	JMP	(R16)
