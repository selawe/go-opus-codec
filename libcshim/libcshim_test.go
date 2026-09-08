package libcshim

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestTLSAllocFree(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	p1 := tls.Alloc(32)
	if p1 == 0 {
		t.Fatal("expected non-zero pointer from Alloc")
	}
	if p1%16 != 0 {
		t.Fatalf("expected 16-byte aligned pointer, got %x", p1)
	}

	p2 := tls.Alloc(64)
	if p2 == 0 {
		t.Fatal("expected non-zero pointer from Alloc")
	}
	if p2%16 != 0 {
		t.Fatalf("expected 16-byte aligned pointer, got %x", p2)
	}
	if p2 <= p1 {
		t.Fatalf("expected p2 > p1, got p1=%x, p2=%x", p1, p2)
	}

	// Free in LIFO order
	tls.Free(64)
	tls.Free(32)

	if tls.sp != 0 {
		t.Fatalf("expected sp to be 0 after frees, got %d", tls.sp)
	}
}

func TestTLSStackExpansion(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	// Initial stack cap is 64KB. Allocating 128KB should expand without panic.
	p := tls.Alloc(128 << 10)
	if p == 0 {
		t.Fatal("expected non-zero pointer for 128KB alloc")
	}
	if p%16 != 0 {
		t.Fatalf("expected 16-byte alignment, got %x", p)
	}
	tls.Free(128 << 10)
}

func TestTLSAllocPointerStability(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	// Allocate a pointer in the first chunk and store a sentinel value
	p1 := tls.Alloc(32)
	StoreInt32(p1, 0x12345678)
	StoreInt32(p1+4, 0x778899AA)

	// Now allocate enough memory to force transition to a second chunk (64KB+)
	p2 := tls.Alloc(128 << 10)
	if p2 == 0 {
		t.Fatal("expected non-zero p2")
	}
	StoreInt32(p2, 0x55AA55AA)

	// p1 must NOT have moved and must retain its exact sentinel values!
	if val := LoadInt32(p1); val != 0x12345678 {
		t.Fatalf("p1 corrupted after chunk allocation: got %x, want %x", val, 0x12345678)
	}
	if val := LoadInt32(p1 + 4); val != int32(int64(0x778899AA)) {
		t.Fatalf("p1+4 corrupted after chunk allocation: got %x, want %x", val, 0x778899AA)
	}

	// Free in reverse order
	tls.Free(128 << 10)
	tls.Free(32)

	if tls.sp != 0 {
		t.Fatalf("expected sp to be 0 after all frees, got %d", tls.sp)
	}
}

func TestXmallocAndXfree(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	ptr := Xmalloc(tls, 100)
	if ptr == 0 {
		t.Fatal("Xmalloc returned 0")
	}
	if ptr%16 != 0 {
		t.Fatalf("expected 16-byte alignment, got %x", ptr)
	}

	// Check that we can write to and read from the allocated memory
	StoreInt32(ptr, 12345)
	StoreInt32(ptr+4, 67890)
	if LoadInt32(ptr) != 12345 {
		t.Fatalf("data mismatch at 0: want 12345, got %d", LoadInt32(ptr))
	}
	if LoadInt32(ptr+4) != 67890 {
		t.Fatalf("data mismatch at 4: want 67890, got %d", LoadInt32(ptr+4))
	}

	Xfree(tls, ptr)

	// Freeing again or freeing 0 should not panic
	Xfree(tls, 0)
}

func TestMemoryOps(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	b1 := make([]byte, 64)
	b2 := make([]byte, 64)
	p1 := PtrByte(b1)
	p2 := PtrByte(b2)

	// Xmemset
	Xmemset(tls, p1, 0xAB, 64)
	for i, b := range b1 {
		if b != 0xAB {
			t.Fatalf("Xmemset failed at %d: got 0x%02x", i, b)
		}
	}

	// Xmemcpy
	Xmemcpy(tls, p2, p1, 64)
	if !bytes.Equal(b1, b2) {
		t.Fatal("Xmemcpy data mismatch between source and dest")
	}

	// Xmemmove overlapping
	b1[0] = 1
	b1[1] = 2
	b1[2] = 3
	Xmemmove(tls, p1+1, p1, 3)
	if b1[1] != 1 || b1[2] != 2 || b1[3] != 3 {
		t.Fatalf("Xmemmove overlap failed: %v", b1[:4])
	}
}

func TestPthreadTLS(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	const testKey uint32 = 0x12345
	if got := Xpthread_getspecific(tls, testKey); got != 0 {
		t.Fatalf("expected 0 for unset key, got %x", got)
	}

	val := uintptr(0xDEADBEEF)
	if ret := Xpthread_setspecific(tls, testKey, val); ret != 0 {
		t.Fatalf("Xpthread_setspecific returned %d", ret)
	}

	if got := Xpthread_getspecific(tls, testKey); got != val {
		t.Fatalf("expected %x, got %x", val, got)
	}
}

func TestVaList(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	bp := tls.Alloc(64)
	defer tls.Free(64)

	VaList(bp, int32(42), uint32(100), uintptr(0xCAFE))

	ap := bp
	if v := VaInt32(&ap); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	if v := VaInt32(&ap); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}
	if v := VaUintptr(&ap); v != 0xCAFE {
		t.Fatalf("expected 0xCAFE, got %x", v)
	}
}

func TestMathAndBitHelpers(t *testing.T) {
	tls := NewTLS()
	defer tls.Close()

	if Xabs(tls, -42) != 42 || Xabs(tls, 42) != 42 {
		t.Fatal("Xabs failed")
	}

	if Xlrintf(tls, 2.4) != 2 || Xlrintf(tls, 2.6) != 3 {
		t.Fatal("Xlrintf failed")
	}

	// Ties to even: 2.5 -> 2, 3.5 -> 4
	if Xlrintf(tls, 2.5) != 2 || Xlrintf(tls, 3.5) != 4 {
		t.Fatalf("Xlrintf ties-to-even failed: 2.5->%d, 3.5->%d", Xlrintf(tls, 2.5), Xlrintf(tls, 3.5))
	}

	if X__builtin_clz(tls, 0) != 32 {
		t.Fatalf("X__builtin_clz(0) expected 32, got %d", X__builtin_clz(tls, 0))
	}
	if X__builtin_clz(tls, 1) != 31 {
		t.Fatalf("X__builtin_clz(1) expected 31, got %d", X__builtin_clz(tls, 1))
	}
	if X__builtin_clz(tls, 0x80000000) != 0 {
		t.Fatalf("X__builtin_clz(0x80000000) expected 0, got %d", X__builtin_clz(tls, 0x80000000))
	}
}

func TestPointerAndLoadStoreHelpers(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	ptr := PtrByte(b)
	if ptr == 0 {
		t.Fatal("expected non-zero PtrByte")
	}

	empty := []byte{}
	if PtrByte(empty) != 0 {
		t.Fatal("expected 0 for empty slice")
	}

	var val int32 = 0
	pVal := uintptr(unsafe.Pointer(&val))
	StoreInt32(pVal, 999)
	if LoadInt32(pVal) != 999 {
		t.Fatalf("expected 999, got %d", LoadInt32(pVal))
	}

	var uval uint32 = 0
	pUVal := uintptr(unsafe.Pointer(&uval))
	StoreUint32(pUVal, 0xDeadBeef)
	if LoadUint32(pUVal) != 0xDeadBeef {
		t.Fatalf("expected 0xDeadBeef, got 0x%x", LoadUint32(pUVal))
	}
}

func TestGoString(t *testing.T) {
	cStr := []byte{'H', 'e', 'l', 'l', 'o', 0}
	p := uintptr(unsafe.Pointer(&cStr[0]))
	if s := GoString(p); s != "Hello" {
		t.Fatalf("expected 'Hello', got %q", s)
	}
	if GoString(0) != "" {
		t.Fatal("expected empty string for nil pointer")
	}
}

func TestLoadStoreUintptrAtNullBase(t *testing.T) {
	// Must safely return 0 / no-op when base pointer is 0, without crashing
	if val := LoadUintptrAt(0, 16); val != 0 {
		t.Fatalf("expected 0 from LoadUintptrAt(0, 16), got %x", val)
	}
	StoreUintptrAt(0, 16, 0x1234)
}

