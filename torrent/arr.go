package torrent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	cfgpkg "github.com/jkaberg/distribyted/config"
)

type arrClient struct {
	inst  *cfgpkg.ArrInstance
	httpc *http.Client
}

func newArrClient(inst *cfgpkg.ArrInstance, httpc *http.Client) *arrClient {
	return &arrClient{inst: inst, httpc: httpc}
}

func (c *arrClient) base(prefix string) (string, error) {
	u, err := url.Parse(c.inst.BaseURL)
	if err != nil {
		return "", err
	}
	// ensure path join
	u.Path = path.Join(u.Path, prefix)
	return u.String(), nil
}

func (c *arrClient) doJSON(method, urlStr string, body any, out any) error {
	var req *http.Request
	var err error
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		req, err = http.NewRequest(method, urlStr, strings.NewReader(string(b)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequest(method, urlStr, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.inst.APIKey)
	if c.inst.Insecure {
		// caller should provide httpc with Insecure transport if needed; we keep simple
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("arr http %s: %d", method, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// categories returns the set of download client categories configured in Arr
func (c *arrClient) categories() (map[string]struct{}, error) {
	type field struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	}
	type dlc struct {
		Fields []field `json:"fields"`
	}
	var endpoint string
	switch c.inst.Type {
	case cfgpkg.ArrRadarr, cfgpkg.ArrSonarr:
		endpoint = "/api/v3/downloadclient"
	case cfgpkg.ArrLidarr:
		endpoint = "/api/v1/downloadclient"
	default:
		return nil, fmt.Errorf("unknown arr type")
	}
	u, err := c.base(endpoint)
	if err != nil {
		return nil, err
	}
	var items []dlc
	if err := c.doJSON(http.MethodGet, u, nil, &items); err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, it := range items {
		for _, f := range it.Fields {
			if strings.EqualFold(f.Name, "category") {
				switch v := f.Value.(type) {
				case string:
					if v != "" {
						out[v] = struct{}{}
					}
				}
			}
		}
	}
	return out, nil
}

// queue item structures (subset)
type radarrQueueItem struct {
	ID         int    `json:"id"`
	DownloadID string `json:"downloadId"`
	Movie      *struct {
		ID int `json:"id"`
	} `json:"movie"`
}

type sonarrQueueItem struct {
	ID         int    `json:"id"`
	DownloadID string `json:"downloadId"`
	Series     *struct {
		ID int `json:"id"`
	} `json:"series"`
	Episode *struct {
		ID int `json:"id"`
	} `json:"episode"`
}

type lidarrQueueItem struct {
	ID         int    `json:"id"`
	DownloadID string `json:"downloadId"`
	Artist     *struct {
		ID int `json:"id"`
	} `json:"artist"`
}

// findQueueByHash finds a queue item whose downloadId matches the torrent hash
func (c *arrClient) findQueueByHash(hash string) (id int, entity string, entityID int, err error) {
	// normalize hash to upper
	H := strings.ToUpper(hash)
	var endpoint string
	switch c.inst.Type {
	case cfgpkg.ArrRadarr:
		endpoint = "/api/v3/queue?page=1&pageSize=100&includeUnknownSeriesItems=true&includeMovie=true"
		u, e := c.base(endpoint)
		if e != nil {
			return 0, "", 0, e
		}
		var items []radarrQueueItem
		if e = c.doJSON(http.MethodGet, u, nil, &items); e != nil {
			return 0, "", 0, e
		}
		for _, it := range items {
			if strings.EqualFold(it.DownloadID, H) {
				if it.Movie != nil {
					return it.ID, "movie", it.Movie.ID, nil
				}
			}
		}
	case cfgpkg.ArrSonarr:
		endpoint = "/api/v3/queue?page=1&pageSize=100&includeUnknownSeriesItems=true"
		u, e := c.base(endpoint)
		if e != nil {
			return 0, "", 0, e
		}
		var items []sonarrQueueItem
		if e = c.doJSON(http.MethodGet, u, nil, &items); e != nil {
			return 0, "", 0, e
		}
		for _, it := range items {
			if strings.EqualFold(it.DownloadID, H) {
				if it.Series != nil {
					return it.ID, "series", it.Series.ID, nil
				}
			}
		}
	case cfgpkg.ArrLidarr:
		endpoint = "/api/v1/queue?page=1&pageSize=100"
		u, e := c.base(endpoint)
		if e != nil {
			return 0, "", 0, e
		}
		var items []lidarrQueueItem
		if e = c.doJSON(http.MethodGet, u, nil, &items); e != nil {
			return 0, "", 0, e
		}
		for _, it := range items {
			if strings.EqualFold(it.DownloadID, H) {
				if it.Artist != nil {
					return it.ID, "artist", it.Artist.ID, nil
				}
			}
		}
	}
	return 0, "", 0, fmt.Errorf("queue item not found for hash")
}

func (c *arrClient) blacklistQueueItem(id int) error {
	switch c.inst.Type {
	case cfgpkg.ArrRadarr:
		u, e := c.base(fmt.Sprintf("/api/v3/queue/%d?blacklist=true", id))
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodDelete, u, nil, nil)
	case cfgpkg.ArrSonarr:
		u, e := c.base(fmt.Sprintf("/api/v3/queue/%d?removeFromClient=false&blocklist=true", id))
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodDelete, u, nil, nil)
	case cfgpkg.ArrLidarr:
		u, e := c.base(fmt.Sprintf("/api/v1/queue/%d?blacklist=true", id))
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodDelete, u, nil, nil)
	}
	return nil
}

func (c *arrClient) triggerSearch(entity string, id int) error {
	payload := map[string]any{}
	switch c.inst.Type {
	case cfgpkg.ArrRadarr:
		payload["name"] = "MoviesSearch"
		payload["movieIds"] = []int{id}
		u, e := c.base("/api/v3/command")
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodPost, u, payload, nil)
	case cfgpkg.ArrSonarr:
		// Prefer series search for simplicity
		payload["name"] = "SeriesSearch"
		payload["seriesId"] = id
		u, e := c.base("/api/v3/command")
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodPost, u, payload, nil)
	case cfgpkg.ArrLidarr:
		payload["name"] = "ArtistSearch"
		payload["artistIds"] = []int{id}
		u, e := c.base("/api/v1/command")
		if e != nil {
			return e
		}
		return c.doJSON(http.MethodPost, u, payload, nil)
	}
	return nil
}

// resolveArrManagedRoutes fetches categories for each instance and returns category->clients map
func resolveArrManagedRoutes(instances []*cfgpkg.ArrInstance, httpc *http.Client) map[string][]*arrClient {
	out := map[string][]*arrClient{}
	for _, inst := range instances {
		if inst == nil || inst.BaseURL == "" || inst.APIKey == "" {
			continue
		}
		c := newArrClient(inst, httpc)
		cats, err := c.categories()
		if err != nil {
			continue
		}
		for cat := range cats {
			out[cat] = append(out[cat], c)
		}
	}
	return out
}

// backoff helper for Arr requests if needed
func sleepShort() { time.Sleep(500 * time.Millisecond) }
