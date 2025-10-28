package torrent

import (
	"os"
	"path"
	"strings"

	dfs "github.com/jkaberg/distribyted/fs"
)

// overlayIndexes holds precomputed cached entries for a route.
type overlayIndexes struct {
	files   map[string]dfs.File // relative path -> placeholder file
	dirSize map[string]int64    // relative dir -> aggregated size
}

// buildOverlayIndexes constructs files and dirSize maps from cached summaries.
func buildOverlayIndexes(cached map[string]*cachedState) map[string]*overlayIndexes {
	out := make(map[string]*overlayIndexes)
	for _, st := range cached {
		route := st.summary.Route
		name := st.summary.Name
		if route == "" || name == "" {
			continue
		}
		oi, ok := out[route]
		if !ok {
			oi = &overlayIndexes{files: make(map[string]dfs.File), dirSize: make(map[string]int64)}
			out[route] = oi
		}
		// files under torrent root
		if len(st.summary.Files) > 0 {
			for _, f := range st.summary.Files {
				rp := path.Join(name, f.Path)
				oi.files[rp] = dfs.NewInfoFile(f.Length)
				// accumulate to parents
				d := rp
				for {
					d = path.Dir(d)
					if d == "." || d == "/" {
						break
					}
					oi.dirSize[d] += f.Length
				}
			}
		}
		// root folder size
		total := oi.dirSize[name]
		if total == 0 {
			if st.summary.SizeBytes > 0 {
				total = st.summary.SizeBytes
			} else if st.summary.PieceBytes > 0 && st.summary.TotalPieces > 0 {
				total = int64(st.summary.PieceBytes) * int64(st.summary.TotalPieces)
			}
		}
		oi.files[name] = dfs.NewInfoDir(total)
	}
	return out
}

// routeLister returns a lister function for a route using precomputed indexes.
func routeLister(oi *overlayIndexes) func(prefix string) (map[string]dfs.File, error) {
	return func(prefix string) (map[string]dfs.File, error) {
		out := make(map[string]dfs.File)
		basePrefix := strings.TrimPrefix(prefix, "/")
		for p, f := range oi.files {
			if basePrefix == "" {
				if i := strings.Index(p, string(os.PathSeparator)); i >= 0 {
					name := p[:i]
					if _, exists := out[name]; !exists {
						out[name] = dfs.NewInfoDir(oi.dirSize[name])
					}
				} else {
					out[p] = f
				}
			} else {
				if strings.HasPrefix(p+string(os.PathSeparator), basePrefix+string(os.PathSeparator)) {
					rest := strings.TrimPrefix(p, basePrefix+string(os.PathSeparator))
					if rest == p {
						continue
					}
					if j := strings.Index(rest, string(os.PathSeparator)); j >= 0 {
						name := rest[:j]
						if _, exists := out[name]; !exists {
							sub := path.Join(basePrefix, name)
							out[name] = dfs.NewInfoDir(oi.dirSize[sub])
						}
					} else if rest != "" {
						out[rest] = f
					}
				}
			}
		}
		return out, nil
	}
}

// materializer returns a function that ensures a file path under a route is
// materialized by asking the service to make the base FS ready to serve it.
func (s *Service) materializer(route string) func(name string) error {
	return func(name string) error {
		folder := path.Join("/", route)
		s.mu.Lock()
		tfs, _ := s.fss[folder]
		rmag := s.routeMagnet[route]
		rfile := s.routeFile[route]
		s.mu.Unlock()
		if tfs == nil {
			return nil
		}

		// Try open to nudge registration if torrent already exists
		rel := strings.TrimPrefix(name, folder)
		if _, err := tfs.Open(rel); err == nil {
			return nil
		}

		// Best-effort specific mapping: infer torrent root folder name to choose a hash
		seg := rel
		if i := strings.Index(seg, string(os.PathSeparator)); i >= 0 {
			seg = seg[:i]
		}
		s.mu.Lock()
		var targetHash string
		for h, cs := range s.cached {
			if cs.Route == route && cs.Name == seg {
				targetHash = h
				break
			}
		}
		s.mu.Unlock()
		if targetHash != "" {
			if p := rfile[targetHash]; p != "" {
				if _, err := s.AddTorrentPath(route, p); err == nil {
					goto NUDGE
				}
			}
			if m := rmag[targetHash]; m != "" {
				if err := s.AddMagnet(route, m); err == nil {
					goto NUDGE
				}
			}
		}
		// Fallback: iterate cached magnets then torrent files for this route
		for _, m := range rmag {
			if err := s.AddMagnet(route, m); err == nil {
				break
			}
		}
		for _, p := range rfile {
			if _, err := s.AddTorrentPath(route, p); err == nil {
				break
			}
		}
	NUDGE:
		// Final nudge
		tfs.Open(rel)
		return nil
	}
}
