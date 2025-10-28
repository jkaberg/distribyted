package fs

import (
	"os"
	"path"
	"time"
)

// Overlay merges cached directory listings with a base filesystem. It delegates
// content reads to the base. On open misses where a cached entry exists, an
// optional materializer can be invoked to make the base capable of serving it
// (e.g., trigger torrent metadata fetch) before retrying.
type Overlay struct {
	base        Filesystem
	lister      func(path string) (map[string]File, error)
	materialize func(name string) error
}

func NewOverlay(base Filesystem, lister func(path string) (map[string]File, error)) *Overlay {
	return &Overlay{base: base, lister: lister}
}

func NewOverlayWithMaterializer(base Filesystem, lister func(path string) (map[string]File, error), materialize func(name string) error) *Overlay {
	return &Overlay{base: base, lister: lister, materialize: materialize}
}

func (o *Overlay) Open(filename string) (f File, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, os.ErrInvalid
		}
	}()

	if f, err = o.base.Open(filename); err == nil {
		return f, nil
	}
	if !os.IsNotExist(err) || o.lister == nil {
		return nil, err
	}
	dir, name := path.Split(filename)
	entries, e := o.lister(path.Clean(dir))
	if e != nil || entries == nil {
		return nil, err
	}
	if _, ok := entries[name]; !ok {
		return nil, err
	}
	if o.materialize != nil {
		go o.materialize(filename)
		// Retry a few times to allow registration
		for i := 0; i < 5; i++ {
			time.Sleep(100 * time.Millisecond)
			if f2, e2 := o.base.Open(filename); e2 == nil {
				return f2, nil
			}
		}
	}
	if ph, ok := entries[name]; ok && ph != nil {
		return ph, nil
	}
	return nil, err
}

func (o *Overlay) ReadDir(path string) (map[string]File, error) {
	// Start with base entries if available
	baseEntries, err := o.base.ReadDir(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if baseEntries == nil {
		baseEntries = make(map[string]File)
	}
	// Merge cached listing without overwriting base
	if o.lister != nil {
		if cached, e := o.lister(path); e == nil && cached != nil {
			for name, f := range cached {
				if _, exists := baseEntries[name]; !exists {
					baseEntries[name] = f
				}
			}
		}
	}
	return baseEntries, nil
}
