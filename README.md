

> This repository is a maintained fork of the original `distribyted` project, focused on tighter integrations with Radarr, Sonarr, and Lidarr. We gratefully acknowledge and honor the work of the original authors and contributors. Upstream project: [github.com/distribyted/distribyted](https://github.com/distribyted/distribyted).

## Distribyted

Distribyted is an alternative torrent client. 
It can expose torrent files as a standard FUSE, webDAV or HTTP endpoint and download them on demand, allowing random reads using a fixed amount of disk space. 

Distribyted tries to make easier integrations with other applications using torrent files, presenting them as a standard filesystem.

## Features

- Integration with Arr's (to delete unhealthy torrents and find health ones)
- qBittorrent compatible API
- `fsnotify` implementation for watch folders
- Improved/reworked UI 
- Rearchitected inner workings

## Get started

See the [docker-compose.yml](https://github.com/jkaberg/distribyted/blob/master/testing/docker-compose.yml) file. Whats important is setting up

- Mountpoints (that are shared across the services)
- Prowlarr: Optionally setup the `flaresolverr` service
- Radarr/Sonarr/Lidarr: Setup the Prowlarr, Jellyfin and qBittorrent integration, and map the correct folders
- Distribyted: Setup the Arr's integrations
- Jellyfin: Setup the media folders

If someonme wants to contribute an howto that would be nice!

## Contributing

Contributions are what make the open-source community such an amazing place to learn, inspire, and create. Any contributions you make are **greatly appreciated**.

## License

Distributed under the GPL3 license. See `LICENSE` for more information.