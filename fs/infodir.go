package fs

// InfoDir is a read-only directory entry that carries an aggregate size for
// listings. It is used by overlays to present cached directory sizes.
type InfoDir struct {
	size int64
}

func NewInfoDir(size int64) *InfoDir { return &InfoDir{size: size} }

func (d *InfoDir) Size() int64                                   { return d.size }
func (d *InfoDir) IsDir() bool                                   { return true }
func (d *InfoDir) Close() error                                  { return nil }
func (d *InfoDir) Read(p []byte) (n int, err error)              { return 0, nil }
func (d *InfoDir) ReadAt(p []byte, off int64) (n int, err error) { return 0, nil }
