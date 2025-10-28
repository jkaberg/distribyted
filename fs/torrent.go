package fs

import (
	"context"
	"io"
	"path"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/torrent"
	"github.com/jkaberg/distribyted/iio"
)

var _ Filesystem = &Torrent{}

type Torrent struct {
	mu          sync.RWMutex
	ts          map[string]*torrent.Torrent
	s           *storage
	loaded      bool
	readTimeout int
	poolSize    int
	readahead   int64
	// registered tracks torrents already registered into storage by hash
	registered map[string]bool
}

func NewTorrent(readTimeout int) *Torrent {
	return &Torrent{
		s:           newStorage(SupportedFactories),
		ts:          make(map[string]*torrent.Torrent),
		readTimeout: readTimeout,
		poolSize:    4,
		readahead:   2 * 1024 * 1024,
		registered:  make(map[string]bool),
	}
}

func (fs *Torrent) SetReaderPoolSize(n int) {
	if n <= 0 {
		n = 1
	}
	fs.mu.Lock()
	fs.poolSize = n
	fs.mu.Unlock()
}

func (fs *Torrent) SetReadaheadBytes(b int64) {
	if b < 0 {
		b = 0
	}
	fs.mu.Lock()
	fs.readahead = b
	fs.mu.Unlock()
}

func (fs *Torrent) AddTorrent(t *torrent.Torrent) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.loaded = false
	fs.ts[t.InfoHash().HexString()] = t
}

func (fs *Torrent) RemoveTorrent(h string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.s.Clear()

	fs.loaded = false

	delete(fs.ts, h)
	delete(fs.registered, h)
}

func (fs *Torrent) load() {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	for h, t := range fs.ts {
		if fs.registered[h] {
			continue
		}
		if t.Info() == nil {
			continue
		}

		files := t.Files()
		wrapInRoot := len(files) == 1
		rootName := ""
		if wrapInRoot {
			rootName = t.Info().Name
		}

		for _, file := range files {
			_ = fs.s.Add(&torrentFile{
				readerFunc:     file.NewReader,
				len:            file.Length(),
				timeout:        fs.readTimeout,
				poolTarget:     fs.poolSize,
				readaheadBytes: fs.readahead,
			}, func() string {
				p := file.Path()
				if wrapInRoot {
					return path.Join(rootName, p)
				}
				return p
			}())
		}
		fs.registered[h] = true
	}
}

func (fs *Torrent) Open(filename string) (File, error) {
	fs.load()
	return fs.s.Get(filename)
}

func (fs *Torrent) ReadDir(path string) (map[string]File, error) {
	fs.load()
	return fs.s.Children(path)
}

type reader interface {
	iio.Reader
	missinggo.ReadContexter
}

type readAtWrapper struct {
	timeout int
	mu      sync.Mutex

	torrent.Reader
	io.ReaderAt
	io.Closer
}

func newReadAtWrapper(r torrent.Reader, timeout int) reader {
	return &readAtWrapper{Reader: r, timeout: timeout}
}

func (rw *readAtWrapper) ReadAt(p []byte, off int64) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	_, err := rw.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}

	return readAtLeast(rw, rw.timeout, p, len(p))
}

func readAtLeast(r missinggo.ReadContexter, timeout int, buf []byte, min int) (n int, err error) {
	if len(buf) < min {
		return 0, io.ErrShortBuffer
	}
	for n < min && err == nil {
		var nn int

		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(
			time.Duration(timeout)*time.Second,
			func() {
				cancel()
			},
		)

		nn, err = r.ReadContext(ctx, buf[n:])
		n += nn

		timer.Stop()
	}
	if n >= min {
		err = nil
	} else if n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (rw *readAtWrapper) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.Reader.Close()
}

var _ File = &torrentFile{}

type torrentFile struct {
	readerFunc func() torrent.Reader
	reader     reader
	pool       chan reader
	poolInit   sync.Once
	poolAll    []reader
	poolTarget int
	len        int64
	timeout    int
	// readahead
	readaheadBytes int64
}

func (d *torrentFile) load() {
	if d.reader != nil {
		return
	}
	d.reader = newReadAtWrapper(d.readerFunc(), d.timeout)
	// initialize pool lazily
	d.poolInit.Do(func() {
		size := d.poolTarget
		if size <= 0 {
			size = 4
		}
		d.pool = make(chan reader, size)
		for i := 0; i < size; i++ {
			pr := newReadAtWrapper(d.readerFunc(), d.timeout)
			d.poolAll = append(d.poolAll, pr)
			d.pool <- pr
		}
		// default if not set
		if d.readaheadBytes == 0 {
			d.readaheadBytes = 2 * 1024 * 1024
		}
	})
}

func (d *torrentFile) Size() int64 {
	return d.len
}

func (d *torrentFile) IsDir() bool {
	return false
}

func (d *torrentFile) Close() error {
	var err error
	if d.reader != nil {
		err = d.reader.Close()
	}

	d.reader = nil

	// close pooled readers
	for _, r := range d.poolAll {
		_ = r.Close()
	}
	d.poolAll = nil
	d.pool = nil

	return err
}

func (d *torrentFile) Read(p []byte) (n int, err error) {
	d.load()
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(
		time.Duration(d.timeout)*time.Second,
		func() {
			cancel()
		},
	)

	defer timer.Stop()
	n, err = d.reader.ReadContext(ctx, p)
	if n > 0 && err == nil {
		d.prefetch(int64(n))
	}
	return n, err
}

func (d *torrentFile) ReadAt(p []byte, off int64) (n int, err error) {
	d.load()
	// Use pooled readers to allow concurrent ReadAt calls
	if d.pool != nil {
		r := <-d.pool
		// Ensure reader is returned to pool
		defer func() { d.pool <- r }()
		n, err = r.ReadAt(p, off)
		if n > 0 && err == nil {
			d.prefetchAt(off + int64(n))
		}
		return n, err
	}
	n, err = d.reader.ReadAt(p, off)
	if n > 0 && err == nil {
		d.prefetchAt(off + int64(n))
	}
	return n, err
}

// prefetch issues an asynchronous read of the next window after current sequential read.
func (d *torrentFile) prefetch(bytesRead int64) {
	// Estimate the next offset as current position; we don't have explicit pos here,
	// so only use ReadAt-based prefetch which passes explicit offsets.
}

func (d *torrentFile) prefetchAt(nextOff int64) {
	if d.readaheadBytes <= 0 || d.pool == nil {
		return
	}
	// pull a reader without blocking the foreground if none available
	select {
	case r := <-d.pool:
		go func(rd reader) {
			defer func() { d.pool <- rd }()
			bufSize := int(d.readaheadBytes)
			if nextOff >= d.len {
				return
			}
			if remain := d.len - nextOff; remain < int64(bufSize) {
				bufSize = int(remain)
			}
			if bufSize <= 0 {
				return
			}
			buf := make([]byte, bufSize)
			_, _ = rd.ReadAt(buf, nextOff)
		}(r)
	default:
		// no spare reader; skip prefetch
	}
}

// (Removed stub/lazy materialization; original load-based behavior restored)
