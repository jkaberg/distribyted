package fuse

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/jkaberg/distribyted/fs"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	fuseAllowOther bool
	path           string

	host *fuse.FileSystemHost
}

func NewHandler(fuseAllowOther bool, path string) *Handler {
	return &Handler{
		fuseAllowOther: fuseAllowOther,
		path:           path,
	}
}

func (s *Handler) Mount(cfs *fs.ContainerFs) error {
	folder := s.path
	// On windows, the folder must don't exist
	if runtime.GOOS == "windows" {
		folder = filepath.Dir(s.path)
	}

	if filepath.VolumeName(folder) == "" {
		if err := os.MkdirAll(folder, 0744); err != nil && !os.IsExist(err) {
			return err
		}
	}

	host := fuse.NewFileSystemHost(NewFS(cfs))

	go func() {
		var config []string

		if s.fuseAllowOther {
			config = append(config, "-o", "allow_other")
		}

		// Increase read sizes for higher throughput on Linux
		if runtime.GOOS == "linux" {
			config = append(config, "-o", "big_writes")
			config = append(config, "-o", "max_read=1048576")
		}

		// Improve kernel cache behavior and watcher compatibility for tools like
		// Jellyfin/Plex. These options do not guarantee inotify for out-of-band
		// updates, but they reduce caching and provide stable inode numbers so
		// downstream scanners behave more predictably.
		config = append(config, "-o", "use_ino")
		config = append(config, "-o", "attr_timeout=0")
		config = append(config, "-o", "entry_timeout=0")
		config = append(config, "-o", "negative_timeout=0")
		config = append(config, "-o", "fsname=distribyted")
		config = append(config, "-o", "subtype=distribyted")

		ok := host.Mount(s.path, config)
		if !ok {
			log.Error().Str("path", s.path).Msg("error trying to mount filesystem")
		}
	}()

	s.host = host

	log.Info().Str("path", s.path).Msg("starting FUSE mount")

	return nil
}

func (s *Handler) Unmount() {
	if s.host == nil {
		return
	}

	ok := s.host.Unmount()
	if !ok {
		log.Error().Str("path", s.path).Msg("unmount failed")
	}
}
