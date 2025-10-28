package log

import (
	"strings"

	"github.com/anacrolix/log"
	"github.com/rs/zerolog"
)

var _ log.Handler = &Torrent{}

type Torrent struct {
	L zerolog.Logger
}

func (l *Torrent) Handle(r log.Record) {
	// Downgrade noisy WebRTC tracker state transitions that spam logs under normal operation.
	if strings.Contains(r.Text(), "webrtc PeerConnection state changed to closed") ||
		strings.Contains(r.Text(), "webrtc PeerConnection state changed to connecting") ||
		strings.Contains(r.Text(), "webrtc PeerConnection state changed to failed") ||
		strings.Contains(r.Text(), "unhandled announce response") {
		l.L.Debug().Msgf(r.Text())
		return
	}

	e := l.L.Info()
	switch r.Level {
	case log.Debug:
		e = l.L.Debug()
	case log.Info:
		e = l.L.Debug().Str("error-type", "info")
	case log.Warning:
		e = l.L.Warn()
	case log.Error:
		e = l.L.Warn().Str("error-type", "error")
	case log.Critical:
		e = l.L.Warn().Str("error-type", "critical")
	}

	e.Msgf(r.Text())
}
