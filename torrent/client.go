package torrent

import (
	"fmt"
	"net"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	tlog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	"github.com/jkaberg/distribyted/config"
	dlog "github.com/jkaberg/distribyted/log"
)

func NewClient(st storage.ClientImpl, fis bep44.Store, cfg *config.TorrentGlobal, id [20]byte) (*torrent.Client, *rate.Limiter, *rate.Limiter, error) {
	torrentCfg := torrent.NewDefaultClientConfig()
	torrentCfg.Seed = true
	torrentCfg.PeerID = string(id[:])
	torrentCfg.DefaultStorage = st
	if cfg.ListenPort > 0 {
		// Prefer fixed listen port if provided
		torrentCfg.ListenPort = cfg.ListenPort
	}
	torrentCfg.DisableIPv6 = cfg.DisableIPv6
	torrentCfg.DisableTCP = cfg.DisableTCP
	torrentCfg.DisableUTP = cfg.DisableUTP

	if cfg.IP != "" {
		ip := net.ParseIP(cfg.IP)
		if ip == nil {
			return nil, nil, nil, fmt.Errorf("invalid provided IP: %q", cfg.IP)
		}

		torrentCfg.PublicIp4 = ip
	}

	l := log.Logger.With().Str("component", "torrent-client").Logger()

	tl := tlog.NewLogger()
	tl.SetHandlers(&dlog.Torrent{L: l})
	torrentCfg.Logger = tl

	torrentCfg.ConfigureAnacrolixDhtServer = func(cfg *dht.ServerConfig) {
		cfg.Store = fis
		cfg.Exp = 2 * time.Hour
		cfg.NoSecurity = false
	}

	// Initialize unlimited rate limiters by default; can be adjusted at runtime
	dl := rate.NewLimiter(rate.Inf, 0)
	ul := rate.NewLimiter(rate.Inf, 0)
	torrentCfg.DownloadRateLimiter = dl
	torrentCfg.UploadRateLimiter = ul

	c, err := torrent.NewClient(torrentCfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return c, dl, ul, nil
}
