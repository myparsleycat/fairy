//go:build windows

#include "textflag.h"

// Entered from DirectWrite with the Windows x64 calling convention.
// DrawGlyphRun(this, ctx, float x, float y, ...) passes x and y in XMM2/XMM3;
// move them into the integer registers the Go callback trampoline spills.
TEXT ·glyphRunStub(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X2, R8
	MOVQ X3, R9
	MOVQ ·glyphRunTarget(SB), AX
	JMP  AX

TEXT ·glyphRunStubAddr(SB), NOSPLIT, $0-8
	LEAQ ·glyphRunStub(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// DrawUnderline and DrawStrikethrough have the same float layout.
TEXT ·decorationStub(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X2, R8
	MOVQ X3, R9
	MOVQ ·decorationTarget(SB), AX
	JMP  AX

TEXT ·decorationStubAddr(SB), NOSPLIT, $0-8
	LEAQ ·decorationStub(SB), AX
	MOVQ AX, ret+0(FP)
	RET

// DrawInlineObject passes its origin the same way.
TEXT ·inlineObjectStub(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X2, R8
	MOVQ X3, R9
	MOVQ ·inlineObjectTarget(SB), AX
	JMP  AX

TEXT ·inlineObjectStubAddr(SB), NOSPLIT, $0-8
	LEAQ ·inlineObjectStub(SB), AX
	MOVQ AX, ret+0(FP)
	RET
