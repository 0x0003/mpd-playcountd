MPD sticker daemon that tracks `playCount`, `lastPlayed`, and `firstPlayed` per song. Records a play after the song has played for half its duration (up to 4 minutes).  
The recording behavior mirrors [listenbrainz-mpd](https://codeberg.org/elomatreb/listenbrainz-mpd) where possible.

- **playCount** - incremented each time the threshold is reached
- **lastPlayed** - Unix timestamp of the most recent play
- **firstPlayed** - Unix timestamp of the first recorded play

## Why?

Mostly for personal use in scripting. Check out [mpd-lastplayed](https://github.com/0x0003/scripts/blob/master/mpd-lastplayed) and [mpd-neverplayed](https://github.com/0x0003/scripts/blob/master/mpd-neverplayed) for playlist creation, and [mps](https://github.com/0x0003/scripts/blob/master/mps) for now-playing status.

Also, some clients (e.g. [rmpc](https://github.com/mierak/rmpc/)) can read and display `playCount` and `lastPlayed` stickers.  
`firstPlayed` isn't "formally" used anywhere as far as I'm aware.

## Usage

```sh
mpd-playcountd # run with defaults
mpd-playcountd --help # print help
mpd-playcountd -host 10.0.0.5 -port 6666 -password "hunter2"
mpd-playcountd -config /path/to/config.toml
```

## Configuration

Precedence: **flag > config file > env var > default**.

| Flag        | Env var    | Default     | Description                                           |
|-------------|------------|-------------|-------------------------------------------------------|
| `-host`     | `MPD_HOST` | `127.0.0.1` | MPD server address (`password@host` format supported) |
| `-port`     | `MPD_PORT` | `6600`      | MPD port                                              |
| `-password` | -          | empty       | MPD password                                          |
| `-retries`  | -          | `1`         | Connection retry count before exiting                 |
| `-quiet`    | -          | `false`     | Suppress log output                                   |
| `-config`   | -          | XDG path    | Path to TOML config file                              |

### Config file

By default the program looks for a config file at `$XDG_CONFIG_HOME/mpd-playcountd/config.toml` - typically `~/.config/mpd-playcountd/config.toml` on Linux, and `%USERPROFILE%\.config\mpd-playcountd\config.toml` on Windows.

```toml
host = "127.0.0.1"
port = 6600
password = ""
retries = 1
quiet = false
```

## Build

- bare go

```sh
go build -o mpd-playcountd .
```

- cross-compile for Windows:

```sh
GOOS=windows GOARCH=amd64 go build -o mpd-playcountd.exe .
```

---

- nix

```sh
nix build # the binary will be in ./result/bin/mpd-playcountd
```

