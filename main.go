package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	mpdHost     string
	mpdPort     int
	maxRetries  int
	mpdPassword string
	quiet       bool
)

type mpdConn struct {
	conn net.Conn
	bw   *bufio.Writer
	br   *bufio.Reader
}

func dialMPD() (*mpdConn, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", mpdHost, mpdPort), 5*time.Second)
	if err != nil {
		return nil, err
	}
	mc := &mpdConn{conn: conn, bw: bufio.NewWriter(conn), br: bufio.NewReader(conn)}
	line, err := mc.readLine()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.HasPrefix(line, "OK ") {
		conn.Close()
		return nil, fmt.Errorf("unexpected greeting: %s", line)
	}
	if mpdPassword != "" {
		if err := mc.cmdOK("password %s", mpdPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth: %w", err)
		}
	}
	return mc, nil
}

func (mc *mpdConn) readLine() (string, error) {
	line, err := mc.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (mc *mpdConn) cmd(format string, args ...interface{}) error {
	_, err := fmt.Fprintf(mc.bw, format+"\n", args...)
	if err != nil {
		return err
	}
	return mc.bw.Flush()
}

// readResp reads MPD response pairs (key: value) until OK or ACK.
// https://musicpd.org/doc/protocol/#command-format
func (mc *mpdConn) readResp() (map[string]string, error) {
	resp := make(map[string]string)
	for {
		line, err := mc.readLine()
		if err != nil {
			return nil, err
		}
		if line == "OK" {
			return resp, nil
		}
		if strings.HasPrefix(line, "ACK ") {
			return nil, fmt.Errorf("%s", line)
		}
		if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
			resp[parts[0]] = parts[1]
		}
	}
}

func (mc *mpdConn) cmdOK(format string, args ...interface{}) error {
	if err := mc.cmd(format, args...); err != nil {
		return err
	}
	line, err := mc.readLine()
	if err != nil {
		return err
	}
	if line == "OK" {
		return nil
	}
	return fmt.Errorf("unexpected: %s", line)
}

// idlePlayer blocks in MPD idle mode, waiting for a player subsystem change.
// The caller must set a ReadDeadline on mc.conn for adaptive timeout; if it
// fires we send noidle to break out of idle and drain the response.
// https://musicpd.org/doc/protocol/#idle
func (mc *mpdConn) idlePlayer() (string, error) {
	if err := mc.cmd("idle player"); err != nil {
		return "", err
	}

	var event string
	for {
		line, err := mc.readLine()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				mc.conn.SetReadDeadline(time.Time{})
				if e := mc.cmd("noidle"); e != nil {
					return "", e
				}
				for {
					l, e := mc.readLine()
					if e != nil {
						return "", e
					}
					if l == "OK" || strings.HasPrefix(l, "ACK ") {
						break
					}
				}
				return "", nil
			}
			return "", err
		}
		if line == "OK" || strings.HasPrefix(line, "ACK ") {
			break
		}
		if strings.HasPrefix(line, "changed: ") {
			event = strings.TrimPrefix(line, "changed: ")
		}
	}
	mc.conn.SetReadDeadline(time.Time{})
	return event, nil
}

func (mc *mpdConn) close() {
	mc.conn.Close()
}

func logf(format string, args ...interface{}) {
	if quiet {
		return
	}
	fmt.Fprintf(os.Stderr, time.Now().Format("2006-01-02 15:04:05 ")+format, args...)
}

func escapeMPD(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", configPath(), "config file path")
	flag.StringVar(&mpdHost, "host", "127.0.0.1", "MPD host")
	flag.IntVar(&mpdPort, "port", 6600, "MPD port")
	flag.IntVar(&maxRetries, "retries", 1, "connection retry count before exiting")
	flag.StringVar(&mpdPassword, "password", "", "MPD password")
	flag.BoolVar(&quiet, "quiet", false, "suppress log output")
	flag.Parse()

	cfg := loadConfig(cfgPath)
	seen := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { seen[f.Name] = true })

	if !seen["host"] {
		if h := os.Getenv("MPD_HOST"); h != "" {
			if at := strings.LastIndexByte(h, '@'); at >= 0 {
				mpdHost = h[at+1:]
			} else {
				mpdHost = h
			}
		} else {
			mpdHost = cfg.Host
		}
	}
	if !seen["port"] {
		if p := os.Getenv("MPD_PORT"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				mpdPort = v
			}
		} else {
			mpdPort = cfg.Port
		}
	}
	if !seen["password"] {
		if h := os.Getenv("MPD_HOST"); h != "" {
			if at := strings.LastIndexByte(h, '@'); at >= 0 {
				mpdPassword = h[:at]
			}
		} else {
			mpdPassword = cfg.Password
		}
	}
	if !seen["retries"] {
		maxRetries = cfg.Retries
	}
	if !seen["quiet"] {
		quiet = cfg.Quiet
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for retries := 0; ; retries++ {
		mc, err := dialMPD()
		if err != nil {
			logf("%v\n", err)
			if retries >= maxRetries {
				logf("max retries reached, exiting\n")
				os.Exit(1)
			}
			time.Sleep(3 * time.Second)
			continue
		}

		retries = 0
		logf("connected\n")

	// Signal goroutine: close the connection to interrupt blocking I/O
	// in idlePlayer. This makes the event loop exit cleanly.
	iterCtx, iterCancel := context.WithCancel(ctx)
		go func(mc *mpdConn) {
			<-iterCtx.Done()
			mc.conn.Close()
		}(mc)

		var (
			file         string
			songID       string
			counted      bool
			segmentStart time.Time
			trackStart   time.Time // wall-clock when track started (like listenbrainz-mpd's listen_timestamp)
			accrued      time.Duration
			threshold    float64
		)

	eventLoop:
		for {
			// Adaptive ReadDeadline: wake up exactly when threshold is reached.
			// We track wall-clock playback time (not MPD's elapsed) so seeks
			// don't advance the counter.
			if file != "" && !counted {
				played := accrued
				if !segmentStart.IsZero() {
					played += time.Since(segmentStart)
				}
				remaining := threshold - played.Seconds()
				if remaining > 0 {
					mc.conn.SetReadDeadline(time.Now().Add(time.Duration(remaining * float64(time.Second))))
				} else {
					mc.conn.SetReadDeadline(time.Now())
				}
			} else {
				mc.conn.SetReadDeadline(time.Time{})
			}

			event, err := mc.idlePlayer()
			if err != nil {
				logf("idle error: %v\n", err)
				break eventLoop
			}
			if event != "" {
				logf("event: %s\n", event)
			}

			if err := mc.cmd("currentsong"); err != nil {
				break eventLoop
			}
			song, err := mc.readResp()
			if err != nil {
				break eventLoop
			}

			if err := mc.cmd("status"); err != nil {
				break eventLoop
			}
			status, err := mc.readResp()
			if err != nil {
				break eventLoop
			}

			state := status["state"]
			newFile := song["file"]
			newSongID := status["songid"]
			elapsedStr := status["elapsed"]
			durStr := song["duration"]
			repeatStr := status["repeat"]
			singleStr := status["single"]
			playlistLenStr := status["playlistlength"]

			var elapsed, dur float64
			if elapsedStr != "" {
				elapsed, _ = strconv.ParseFloat(elapsedStr, 64)
			}
			if durStr != "" && durStr != "0" {
				dur, _ = strconv.ParseFloat(durStr, 64)
			}
			// Same as listenbrainz-mpd: count after min(duration/2) or 4 min.
			if dur > 0 {
				threshold = math.Min(dur/2.0, 240.0)
			} else {
				threshold = 240.0
			}

			logf("state=%s file=%s elapsed=%.3f dur=%.3f threshold=%.3f counted=%v\n",
				state, newFile, elapsed, dur, threshold, counted)

			if state == "stop" {
				if file != "" {
					logf("stopped\n")
				}
				file = ""
				songID = ""
				counted = false
				accrued = 0
				segmentStart = time.Time{}
				trackStart = time.Time{} // stop resets everything
				continue
			}

			if state == "pause" {
				if !segmentStart.IsZero() {
					accrued += time.Since(segmentStart)
					segmentStart = time.Time{}
				}
				continue
			}

			// state == "play"
			isNew := newFile != file || newSongID != songID
			onRepeat := false
			if !isNew && counted {
				// Only restart the listen if the track looped in
				// repeat+single mode or single-track playlist.
				if dur > 0 && elapsed/dur <= 0.01 {
					playlistLen, _ := strconv.Atoi(playlistLenStr)
					if repeatStr == "1" && (singleStr != "0" || playlistLen == 1) {
						onRepeat = true
					}
				}
			}

			if isNew || onRepeat {
				if isNew {
					logf("song changed to: %s\n", newFile)
				}
				if onRepeat {
					logf("repeat detected\n")
				}
				file = newFile
				songID = newSongID
				counted = false
				accrued = 0
				segmentStart = time.Time{}
				trackStart = time.Now() // record start time once; used for sticker timestamps
			}
			if segmentStart.IsZero() {
				segmentStart = time.Now()
			}

			playedSeconds := accrued + time.Since(segmentStart)

			if file != "" && !counted && playedSeconds.Seconds() >= threshold {
				logf("threshold reached!\n")
				ef := escapeMPD(file)

				if err := mc.cmd(`sticker get song "%s" playCount`, ef); err != nil {
					break eventLoop
				}
				pcResp, pcErr := mc.readResp()
				pc := 0
				if pcErr == nil {
					if v, ok := pcResp["sticker"]; ok && strings.HasPrefix(v, "playCount=") {
						pc, _ = strconv.Atoi(strings.TrimPrefix(v, "playCount="))
					}
				}

				pc++
				logf("playCount -> %d\n", pc)
				if err := mc.cmdOK(`sticker set song "%s" playCount %d`, ef, pc); err != nil {
					break eventLoop
				}

				ts := trackStart.Unix() // use original start time, not current time
				logf("lastPlayed: %d\n", ts)
				if err := mc.cmdOK(`sticker set song "%s" lastPlayed %d`, ef, ts); err != nil {
					break eventLoop
				}

			// Only set firstPlayed if the sticker doesn't exist yet.
			if err := mc.cmd(`sticker get song "%s" firstPlayed`, ef); err != nil {
				break eventLoop
			}
			_, fpErr := mc.readResp()
			if fpErr != nil {
				logf("firstPlayed: %d\n", ts)
				if err := mc.cmdOK(`sticker set song "%s" firstPlayed %d`, ef, ts); err != nil {
					break eventLoop
				}
			}

				counted = true
			}
		}

		iterCancel()
		mc.close()
		logf("disconnected\n")

		if ctx.Err() != nil {
			logf("shutting down\n")
			return
		}
	}
}
