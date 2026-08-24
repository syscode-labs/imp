package noderuntime

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/types"
)

const defaultStatePath = "/var/lib/imp/runtime"

var errInventoryNotFound = errors.New("runtime inventory record not found")

type inventoryRecord struct {
	UID       types.UID `json:"uid"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	PID       int64     `json:"pid"`
	IP        string    `json:"ip"`
}

type inventory struct{ path string }

func (b *Backend) inventory() inventory {
	path := b.StatePath
	if path == "" {
		path = defaultStatePath
	}
	return inventory{path: path}
}

func (i inventory) save(record inventoryRecord) error {
	if err := os.MkdirAll(i.path, 0o750); err != nil {
		return fmt.Errorf("create runtime state path: %w", err)
	}
	temporary, err := os.CreateTemp(i.path, ".inventory-*")
	if err != nil {
		return fmt.Errorf("create inventory record: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName) //nolint:errcheck // renamed records no longer exist at this path
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set inventory record permissions: %w", err)
	}
	if err := json.NewEncoder(temporary).Encode(record); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode inventory record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close inventory record: %w", err)
	}
	if err := os.Rename(temporaryName, i.recordPath(record.UID)); err != nil { //nolint:gosec // UID is encoded before becoming a filename
		return fmt.Errorf("publish inventory record: %w", err)
	}
	return nil
}

func (i inventory) load(uid types.UID) (inventoryRecord, error) {
	file, err := os.Open(i.recordPath(uid))
	if errors.Is(err, os.ErrNotExist) {
		return inventoryRecord{}, errInventoryNotFound
	}
	if err != nil {
		return inventoryRecord{}, fmt.Errorf("open inventory record: %w", err)
	}
	defer file.Close() //nolint:errcheck // read is complete
	var record inventoryRecord
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		return inventoryRecord{}, fmt.Errorf("decode inventory record: %w", err)
	}
	return record, nil
}

func (i inventory) remove(uid types.UID) error {
	err := os.Remove(i.recordPath(uid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (i inventory) recordPath(uid types.UID) string {
	return filepath.Join(i.path, base64.RawURLEncoding.EncodeToString([]byte(uid))+".json")
}
