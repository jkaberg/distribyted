package fs

import (
	"os"
	"path"
)

// Overlay is a filesystem wrapper that merges cached directory listings from a
// lister function with the base filesystem. It never serves file contents
// itself; Open is always delegated to the base.
type Overlay struct {
	base   Filesystem
	lister func(path string) (map[string]File, error)
}

func NewOverlay(base Filesystem, lister func(path string) (map[string]File, error)) *Overlay {
	return &Overlay{base: base, lister: lister}
}

func (o *Overlay) Open(filename string) (File, error) {
	f, err := o.base.Open(filename)
	if err == nil {
		return f, nil
	}
	if !os.IsNotExist(err) || o.lister == nil {
		return nil, err
	}
	// Consult cached listing for a placeholder
	dir, name := path.Split(filename)
	entries, e := o.lister(path.Clean(dir))
	if e != nil {
		return nil, err
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
