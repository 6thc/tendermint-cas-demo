package cas

import (
	"bytes"
	"testing"
)

func TestState(t *testing.T) {
	s := NewState()

	if err := s.CompareAndSwap("a", nil, []byte("alpha one")); err != nil {
		t.Errorf("CAS(a): %v", err)
	}
	if err := s.CompareAndSwap("b", nil, []byte("beta one")); err != nil {
		t.Errorf("CAS(b): %v", err)
	}
	if err := s.CompareAndSwap("a", []byte("alpha one"), []byte("alpha two")); err != nil {
		t.Errorf("CAS(a): %v", err)
	}
	if want, have := ErrCASFailure, s.CompareAndSwap("b", nil, []byte("should not work")); want != have {
		t.Errorf("CAS(b): want %v, have %v", want, have)
	}

	b, err := s.Get("b")
	if err != nil {
		t.Errorf("Get(b): %v", err)
	}
	if want, have := []byte("beta one"), b; !bytes.Equal(want, have) {
		t.Errorf("Get(b): want %q, have %q", string(want), string(have))
	}

	var buf bytes.Buffer
	if err = s.Commit(newNopWriteCloser(&buf)); err != nil {
		t.Errorf("Commit: %v", err)
	}
	if want, have := int64(1), s.Commits(); want != have {
		t.Errorf("Commits: want %d, have %d", want, have)
	}

	other := NewState()
	if err := other.Restore(&buf); err != nil {
		t.Errorf("Load: %v", err)
	}
	if want, have := s.Hash(), other.Hash(); !bytes.Equal(want, have) {
		t.Fatal("hash: inconsistent")
	}
	if want, have := int64(1), other.Commits(); want != have {
		t.Errorf("Commits: want %d, have %d", want, have)
	}

	a, err := other.Get("a")
	if err != nil {
		t.Errorf("Get(a): %v", err)
	}
	if want, have := []byte("alpha two"), a; !bytes.Equal(want, have) {
		t.Errorf("Get(a): want %q, have %q", string(want), string(have))
	}

	x, err := other.Get("x")
	if want, have := ErrKeyNotFound, err; want != have {
		t.Errorf("Get(x): want %v, have %v", want, have)
	}
	if want, have := []byte(nil), x; !bytes.Equal(want, have) {
		t.Errorf("Get(x): want %v, have %v", want, have)
	}
}
