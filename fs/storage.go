package fs

import (
	"os"
	"path"
	"strings"
)

const separator = "/"

type FsFactory func(f File) (Filesystem, error)

var SupportedFactories = map[string]FsFactory{
	".zip": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &Zip{}), nil
	},
	".rar": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &Rar{}), nil
	},
	".7z": func(f File) (Filesystem, error) {
		return NewArchive(f, f.Size(), &SevenZip{}), nil
	},
}

type storage struct {
	factories map[string]FsFactory

	files       map[string]File
	filesystems map[string]Filesystem
	children    map[string]map[string]File

	// cache of filesystem mount prefixes sorted longest-first for faster lookup
	mounts []string
}

func newStorage(factories map[string]FsFactory) *storage {
	return &storage{
		files:       make(map[string]File),
		children:    make(map[string]map[string]File),
		filesystems: make(map[string]Filesystem),
		factories:   factories,
	}
}

func (s *storage) Clear() {
	s.files = make(map[string]File)
	s.children = make(map[string]map[string]File)
	s.filesystems = make(map[string]Filesystem)
	s.mounts = nil

	s.Add(&Dir{}, "/")
}

func (s *storage) Has(path string) bool {
	path = clean(path)

	f := s.files[path]
	if f != nil {
		return true
	}

	if f, _ := s.getFileFromFs(path); f != nil {
		return true
	}

	return false
}

func (s *storage) AddFS(fs Filesystem, p string) error {
	p = clean(p)
	if s.Has(p) {
		if dir, err := s.Get(p); err == nil {
			if !dir.IsDir() {
				return os.ErrExist
			}
		}

		return nil
	}

	s.filesystems[p] = fs
	s.refreshMounts()
	return s.createParent(p, &Dir{})
}

// RemoveFS removes a mounted filesystem and its directory entry from the
// container. It is safe to call even if the mount doesn't exist.
func (s *storage) RemoveFS(p string) error {
	p = clean(p)
	delete(s.filesystems, p)
	s.refreshMounts()
	base, filename := path.Split(p)
	base = clean(base)
	if ch, ok := s.children[base]; ok {
		delete(ch, filename)
	}
	return nil
}

func (s *storage) Add(f File, p string) error {
	p = clean(p)
	if s.Has(p) {
		if dir, err := s.Get(p); err == nil {
			if !dir.IsDir() {
				return os.ErrExist
			}
		}

		return nil
	}

	ext := path.Ext(p)
	if ffs := s.factories[ext]; ffs != nil {
		fs, err := ffs(f)
		if err != nil {
			return err
		}

		s.filesystems[p] = fs
	} else {
		s.files[p] = f
	}

	return s.createParent(p, f)
}

func (s *storage) createParent(p string, f File) error {
	base, filename := path.Split(p)
	base = clean(base)

	if err := s.Add(&Dir{}, base); err != nil {
		return err
	}

	if _, ok := s.children[base]; !ok {
		s.children[base] = make(map[string]File)
	}

	if filename != "" {
		s.children[base][filename] = f

		// Propagate leaf file size to ancestor directories
		if !f.IsDir() {
			s.addSizeToAncestors(base, f.Size())
		}
	}

	return nil
}

// addSizeToAncestors increments the size of the directory at start and all of its
// ancestor directories (up to and including "/") by size.
func (s *storage) addSizeToAncestors(start string, size int64) {
	cur := clean(start)
	for {
		if file, ok := s.files[cur]; ok {
			if dir, ok := file.(*Dir); ok {
				dir.size += size
			}
		}
		if cur == "/" {
			break
		}
		parent, _ := path.Split(cur)
		cur = clean(parent)
	}
}

func (s *storage) Children(path string) (map[string]File, error) {
	path = clean(path)

	files, err := s.getDirFromFs(path)
	if err == nil {
		return files, nil
	}

	if !os.IsNotExist(err) {
		return nil, err
	}

	l := make(map[string]File)
	for n, f := range s.children[path] {
		l[n] = f
	}

	return l, nil
}

func (s *storage) Get(path string) (File, error) {
	path = clean(path)
	if !s.Has(path) {
		return nil, os.ErrNotExist
	}

	file, ok := s.files[path]
	if ok {
		return file, nil
	}

	return s.getFileFromFs(path)
}

func (s *storage) getFileFromFs(p string) (File, error) {
	for _, fsp := range s.mounts {
		if strings.HasPrefix(p, fsp) {
			fs := s.filesystems[fsp]
			if p == fsp {
				return &Dir{}, nil
			}
			return fs.Open(separator + strings.TrimPrefix(p, fsp))
		}
	}

	return nil, os.ErrNotExist
}

func (s *storage) getDirFromFs(p string) (map[string]File, error) {
	for _, fsp := range s.mounts {
		if strings.HasPrefix(p, fsp) {
			fs := s.filesystems[fsp]
			path := strings.TrimPrefix(p, fsp)
			return fs.ReadDir(path)
		}
	}

	return nil, os.ErrNotExist
}

func clean(p string) string {
	return path.Clean(separator + strings.ReplaceAll(p, "\\", "/"))
}

func (s *storage) refreshMounts() {
	s.mounts = s.mounts[:0]
	for k := range s.filesystems {
		s.mounts = append(s.mounts, k)
	}
	// sort longest-first so longest prefix matches first
	for i := 0; i < len(s.mounts)-1; i++ {
		for j := i + 1; j < len(s.mounts); j++ {
			if len(s.mounts[i]) < len(s.mounts[j]) {
				s.mounts[i], s.mounts[j] = s.mounts[j], s.mounts[i]
			}
		}
	}
}
