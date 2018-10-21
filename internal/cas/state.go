package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Errors related to the state.
var (
	ErrCASFailure  = errors.New("CAS failure")
	ErrKeyNotFound = errors.New("key not found")
)

// State provides a key-value store with compare-and-swap mutability.
// Persistence is achieved by manually invoking Commit (and Restore).
type State struct {
	mtx            sync.RWMutex
	data           map[string][]byte
	commitCount    int64
	lastCommitHash []byte
}

// NewState returns a new, empty state.
// Load persisted data, if any, via Restore.
func NewState() *State {
	return &State{
		data: map[string][]byte{},
	}
}

// Get the value associated with the key.
// Returns ErrKeyNotFound if not found.
func (s *State) Get(key string) ([]byte, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return v, nil
}

// CompareAndSwap sets key to new if and only if its current value is old.
// Returns ErrCASFailure if the current value is not old.
func (s *State) CompareAndSwap(key string, old, new []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if current := s.data[key]; !bytes.Equal(current, old) {
		return ErrCASFailure
	}
	s.data[key] = new
	return nil
}

// Commit the current state to the WriteCloser. On success, close the
// WriteCloser, increment the commit count, and update the last commit hash.
func (s *State) Commit(wc io.WriteCloser) (err error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	var (
		shasum = sha256.New()
		multi  = io.MultiWriter(wc, shasum)
	)
	err = json.NewEncoder(multi).Encode(serializationFormat{
		Data:        s.data,
		CommitCount: s.commitCount + 1,
	})
	if err == nil {
		err = wc.Close()
	}
	if err == nil {
		s.commitCount++
		s.lastCommitHash = shasum.Sum(nil)
	}
	return err
}

// Restore state from the Reader, overwriting any current state. On success,
// update commit count from the serialized data, and writes the last commit hash
// based on its own computation of a hash.
func (s *State) Restore(r io.Reader) (err error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	var (
		intermediate serializationFormat
		shasum       = sha256.New()
		tee          = io.TeeReader(r, shasum)
	)
	err = json.NewDecoder(tee).Decode(&intermediate)
	if err == nil {
		s.data = intermediate.Data
		s.commitCount = intermediate.CommitCount
		s.lastCommitHash = shasum.Sum(nil)
	}
	return err
}

// Commits returns the number of successful commits.
// This value is persisted.
func (s *State) Commits() int64 {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.commitCount
}

// Hash returns a SHA256 hash of the state at time of last commit.
// This value is not persisted, but is recalculated on Restore.
func (s *State) Hash() []byte {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.lastCommitHash
}

type serializationFormat struct {
	Data        map[string][]byte `json:"data"`
	CommitCount int64             `json:"commit_count"`
}

func copyState(dst, src *State) {
	src.mtx.RLock()
	defer src.mtx.RUnlock()
	dst.mtx.Lock()
	defer dst.mtx.Unlock()
	dst.data = make(map[string][]byte, len(src.data))
	for k, v := range src.data {
		dst.data[k] = v
	}
	dst.commitCount = src.commitCount
	dst.lastCommitHash = src.lastCommitHash
}
