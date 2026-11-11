//go:build windows

#include "textflag.h"

// ElementProviderFromPoint(this, double x, double y, out) receives x and y in
// XMM1/XMM2; move them into RDX/R8 for the Go callback.
TEXT ·fromPointStub(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X1, DX
	MOVQ X2, R8
	MOVQ ·fromPointTarget(SB), AX
	JMP  AX

TEXT ·fromPointStubAddr(SB), NOSPLIT, $0-8
	LEAQ ·fromPointStub(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// IRangeValueProvider::SetValue(this, double val) receives val in XMM1.
TEXT ·rangeSetStub(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X1, DX
	MOVQ ·rangeSetTarget(SB), AX
	JMP  AX

TEXT ·rangeSetStubAddr(SB), NOSPLIT, $0-8
	LEAQ ·rangeSetStub(SB), AX
	MOVQ AX, ret+0(FP)
	RET
