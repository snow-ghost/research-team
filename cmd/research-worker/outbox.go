package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func deliverSaved(ctx context.Context, c client, dir string) error {
	r, err := loadDelivery(dir, c.config)
	if err != nil || r.Acknowledged {
		return err
	}
	if err := c.post(ctx, "/attempts/"+r.Attempt+"/result", r.Request, nil); err != nil {
		return err
	}
	r.Acknowledged = true
	return saveDelivery(dir, r)
}

type deliveryRecord struct {
	Version       int                       `json:"version"`
	Server        string                    `json:"server"`
	Worker        string                    `json:"worker"`
	Attempt       string                    `json:"attempt"`
	Request       researchweb.WorkerRequest `json:"request"`
	RequestSHA256 string                    `json:"request_sha256"`
	Acknowledged  bool                      `json:"acknowledged"`
}

func deliveryDigest(r researchweb.WorkerRequest) string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checkDeliveryDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("delivery directory must be private and regular")
	}
	return nil
}

func saveDelivery(dir string, r deliveryRecord) error {
	if err := checkDeliveryDirectory(dir); err != nil {
		return err
	}
	r.RequestSHA256 = deliveryDigest(r.Request)
	body, err := json.Marshal(r)
	if err != nil || len(body) > 2<<20 {
		return errors.New("delivery record exceeds bound")
	}
	f, err := os.CreateTemp(dir, ".delivery-")
	if err != nil {
		return errors.New("cannot create delivery record")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(body); err != nil {
		f.Close()
		return errors.New("cannot write delivery record")
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return errors.New("cannot sync delivery record")
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(dir, "result.json")); err != nil {
		return errors.New("cannot commit delivery record")
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func loadDelivery(dir string, conf config) (deliveryRecord, error) {
	var r deliveryRecord
	if err := checkDeliveryDirectory(dir); err != nil {
		return r, err
	}
	p := filepath.Join(dir, "result.json")
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 2<<20 {
		return r, errors.New("delivery record unavailable or not private")
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) > 2<<20 {
		return r, errors.New("delivery record unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF {
		return r, errors.New("invalid delivery record")
	}
	id := regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	if r.Version != 1 || r.Server != conf.Server || r.Worker != conf.Worker || r.Request.Result == nil || !id.MatchString(r.Attempt) || r.Request.Result.AttemptID != r.Attempt || r.RequestSHA256 != deliveryDigest(r.Request) || !id.MatchString(r.Request.RequestID) || r.Request.Epoch != r.Request.Result.LeaseEpoch || r.Request.Snapshot != r.Request.Result.Snapshot {
		return r, errors.New("delivery record binding differs")
	}
	return r, nil
}
