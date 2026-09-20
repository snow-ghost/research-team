package coddy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type Store struct{ dir string }

func OpenStore(dir string) (*Store, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.New("invalid state directory")
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, errors.New("cannot create state directory")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("state directory must be private (0700)")
	}
	return &Store{dir: absolute}, nil
}
func (s *Store) Load(id string) (*Record, error) {
	if !idPattern.MatchString(id) {
		return nil, errors.New("invalid delegation id")
	}
	file, err := os.Open(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecord+1))
	if err != nil || len(data) > maxRecord {
		return nil, ErrLimit
	}
	var r Record
	if json.Unmarshal(data, &r) != nil || r.ID != id || r.Schema != 1 {
		return nil, errors.New("invalid local delegation record")
	}
	return &r, nil
}
func (s *Store) save(r *Record) error {
	if !idPattern.MatchString(r.ID) {
		return errors.New("invalid delegation id")
	}
	r.Revision++
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil || len(data) > maxRecord {
		return ErrLimit
	}
	return s.atomic(r.ID+".json", data)
}
func (s *Store) atomic(name string, data []byte) error {
	file, err := os.CreateTemp(s.dir, ".state-")
	if err != nil {
		return errors.New("cannot prepare state file")
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot persist state file")
	}
	if err = os.Rename(file.Name(), filepath.Join(s.dir, name)); err != nil {
		return errors.New("cannot replace state file")
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return errors.New("cannot sync state directory")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return errors.New("cannot sync state directory")
	}
	return nil
}
func (s *Store) saveCandidate(c Candidate) (string, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil || len(data) > maxRecord {
		return "", ErrLimit
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	name := "candidate-" + id + ".json"
	existing, err := os.ReadFile(filepath.Join(s.dir, name))
	if err == nil {
		if !bytes.Equal(existing, data) {
			return "", errors.New("candidate content mismatch")
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return "", errors.New("cannot inspect candidate file")
	}
	if err = s.atomic(name, data); err != nil {
		return "", err
	}
	return id, nil
}
