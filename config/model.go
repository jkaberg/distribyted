package config

// Root is the main yaml config object
type Root struct {
	HTTPGlobal *HTTPGlobal    `yaml:"http"`
	WebDAV     *WebDAVGlobal  `yaml:"webdav"`
	Torrent    *TorrentGlobal `yaml:"torrent"`
	Fuse       *FuseGlobal    `yaml:"fuse"`
	Log        *Log           `yaml:"log"`

	Health *Health `yaml:"health"`

	Routes []*Route `yaml:"routes"`
}

type Log struct {
	Debug      bool   `yaml:"debug"`
	MaxBackups int    `yaml:"max_backups"`
	MaxSize    int    `yaml:"max_size"`
	MaxAge     int    `yaml:"max_age"`
	Path       string `yaml:"path"`
}

type TorrentGlobal struct {
	ReadTimeout            int     `yaml:"read_timeout,omitempty"`
	ContinueWhenAddTimeout bool    `yaml:"continue_when_add_timeout,omitempty"`
	AddTimeout             int     `yaml:"add_timeout,omitempty"`
	GlobalCacheSize        int64   `yaml:"global_cache_size,omitempty"`
	MetadataFolder         string  `yaml:"metadata_folder,omitempty"`
	DisableIPv6            bool    `yaml:"disable_ipv6,omitempty"`
	DisableTCP             bool    `yaml:"disable_tcp,omitempty"`
	DisableUTP             bool    `yaml:"disable_utp,omitempty"`
	IP                     string  `yaml:"ip,omitempty"`
	ListenPort             int     `yaml:"listen_port,omitempty"`
	DownloadLimitMbit      float64 `yaml:"download_limit_mbit,omitempty"`
	UploadLimitMbit        float64 `yaml:"upload_limit_mbit,omitempty"`
	ReadaheadMB            int     `yaml:"readahead_mb,omitempty"`
	ReaderPoolSize         int     `yaml:"reader_pool_size,omitempty"`
	// Seed gathering: optional list of extra trackers and/or URL to fetch a list
	ExtraTrackers    []string `yaml:"extra_trackers,omitempty" json:"extra_trackers,omitempty"`
	ExtraTrackersURL string   `yaml:"extra_trackers_url,omitempty" json:"extra_trackers_url,omitempty"`
}

type WebDAVGlobal struct {
	Port int    `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type HTTPGlobal struct {
	Port   int    `yaml:"port"`
	IP     string `yaml:"ip"`
	HTTPFS bool   `yaml:"httpfs"`
	// QbittorrentAPI enables the optional qBittorrent-compatible API under /api/v2
	QbittorrentAPI bool `yaml:"qbittorrent_api,omitempty"`
}

type FuseGlobal struct {
	AllowOther bool   `yaml:"allow_other,omitempty"`
	Path       string `yaml:"path"`
}

// Health configuration for periodic torrent health checks and Arr integration
type Health struct {
	Enabled         bool `yaml:"enabled"`
	IntervalMinutes int  `yaml:"interval_minutes"`
	GraceMinutes    int  `yaml:"grace_minutes"`

	MinSeeders int `yaml:"min_seeders"`

	GoodSeeders      int `yaml:"good_seeders"`
	ExcellentSeeders int `yaml:"excellent_seeders"`

	Arr []*ArrInstance `yaml:"arr"`
}

type ArrType string

const (
	ArrRadarr ArrType = "radarr"
	ArrSonarr ArrType = "sonarr"
	ArrLidarr ArrType = "lidarr"
)

type ArrInstance struct {
	Name    string  `yaml:"name" json:"name"`
	Type    ArrType `yaml:"type" json:"type"`
	BaseURL string  `yaml:"base_url" json:"base_url"`
	APIKey  string  `yaml:"api_key" json:"api_key"`
	// Optional: trust invalid TLS certs
	Insecure bool `yaml:"insecure,omitempty" json:"insecure,omitempty"`
}

type Route struct {
	Name          string     `yaml:"name"`
	Torrents      []*Torrent `yaml:"torrents"`
	TorrentFolder string     `yaml:"torrent_folder"`
}

type Torrent struct {
	MagnetURI   string `yaml:"magnet_uri,omitempty"`
	TorrentPath string `yaml:"torrent_path,omitempty"`
}

func AddDefaults(r *Root) *Root {
	if r.Torrent == nil {
		r.Torrent = &TorrentGlobal{}
	}

	if r.Torrent.AddTimeout == 0 {
		r.Torrent.AddTimeout = 60
	}

	if r.Torrent.ReadTimeout == 0 {
		r.Torrent.ReadTimeout = 120
	}

	if r.Torrent.GlobalCacheSize == 0 {
		r.Torrent.GlobalCacheSize = 2048 // 2GB
	}

	if r.Torrent.ReadaheadMB == 0 {
		r.Torrent.ReadaheadMB = 2
	}
	if r.Torrent.ReaderPoolSize == 0 {
		r.Torrent.ReaderPoolSize = 4
	}

	if r.Torrent.MetadataFolder == "" {
		r.Torrent.MetadataFolder = metadataFolder
	}

	if r.Fuse != nil {
		if r.Fuse.Path == "" {
			r.Fuse.Path = mountFolder
		}
	}

	if r.HTTPGlobal == nil {
		r.HTTPGlobal = &HTTPGlobal{}
	}

	if r.HTTPGlobal.IP == "" {
		r.HTTPGlobal.IP = "0.0.0.0"
	}

	if r.Log == nil {
		r.Log = &Log{}
	}

	if r.Health == nil {
		r.Health = &Health{
			Enabled:          false,
			IntervalMinutes:  60,
			GraceMinutes:     30,
			MinSeeders:       2,
			GoodSeeders:      5,
			ExcellentSeeders: 10,
		}
	}

	return r
}
