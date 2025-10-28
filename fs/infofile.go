package fs

import "io"

// InfoFile is a lightweight file that exposes a size for directory listings
// but does not contain data. It is intended only for overlay listings.
type InfoFile struct {
	size int64
}

func NewInfoFile(size int64) *InfoFile { return &InfoFile{size: size} }

func (f *InfoFile) Size() int64                             { return f.size }
func (f *InfoFile) IsDir() bool                             { return false }
func (f *InfoFile) Close() error                            { return nil }
func (f *InfoFile) Read(p []byte) (int, error)              { return 0, io.EOF }
func (f *InfoFile) ReadAt(p []byte, off int64) (int, error) { return 0, io.EOF }
