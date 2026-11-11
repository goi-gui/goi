// Package media decodes audio and video with the ffmpeg command line tool.
//
// ffmpeg is the only decoder the framework needs: it covers every container and
// codec a desktop app meets, it is available on Linux, macOS and Windows, and
// running it as a child process keeps the framework free of cgo beyond the
// platform layer. A machine without ffmpeg still runs an app: the components
// report a missing backend instead of failing the frame.
//
// The two decoders here are deliberately dumb: they stream raw bytes at a fixed
// rate and let the caller decide what is late. That is what keeps a video from
// slowing the UI down — a frame the painter never asked for is dropped instead
// of queued.
package media

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoFFmpeg is returned when no ffmpeg binary can be found. An app that shows
// a player should surface it and carry on; nothing else needs ffmpeg.
var ErrNoFFmpeg = errors.New(`media: ffmpeg not found; install it, or point the GOI_FFMPEG environment variable at the binary`)

// Info describes one media file.
type Info struct {
	Width, Height int
	// FPS is the video frame rate; 0 for an audio-only file.
	FPS float64
	// Duration is the length of the file, or 0 when the container does not say.
	Duration time.Duration
	HasVideo bool
	HasAudio bool
	// AudioTracks is how many audio streams the file carries, which is what a
	// player offers as a choice. A file has 1 when it has sound, 0 when not.
	AudioTracks int
}

// IsLive reports whether the source is a live feed: a network stream with no
// length. A file with no duration is unusual enough that "network and no
// duration" is the right test for "cannot be seeked, has no end".
func (i Info) IsLive(src string) bool { return i.Duration <= 0 && IsURL(src) }

// IsURL reports whether src is opened by ffmpeg itself — a network stream or
// another protocol — rather than read from this machine. `file://` and the
// design's own `goi://` name local assets and are not streams.
func IsURL(src string) bool {
	m := schemeRe.FindStringSubmatch(src)
	if m == nil {
		return false
	}
	s := strings.ToLower(m[1])
	return s != "file" && s != "goi"
}

var (
	lookOnce sync.Once
	ffmpegAt string
)

// FFmpeg returns the ffmpeg binary to run: GOI_FFMPEG when it names one, else the
// first ffmpeg on PATH.
func FFmpeg() (string, error) {
	lookOnce.Do(func() {
		if p := os.Getenv("GOI_FFMPEG"); p != "" {
			if _, err := os.Stat(p); err == nil {
				ffmpegAt = p
				return
			}
		}
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegAt = p
		}
	})
	if ffmpegAt == "" {
		return "", ErrNoFFmpeg
	}
	return ffmpegAt, nil
}

// Available reports whether an ffmpeg binary was found.
func Available() bool {
	_, err := FFmpeg()
	return err == nil
}

// probeTimeout bounds the banner read, which ffmpeg prints and then exits.
const probeTimeout = 10 * time.Second

var (
	durationRe = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	videoRe    = regexp.MustCompile(`Stream #\d+:\d+(?:\[[^\]]*\])?.*?: Video: .*?(\d{2,5})x(\d{2,5})`)
	schemeRe   = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.\-]*)://`)
	fpsRe      = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*fps`)
	audioRe    = regexp.MustCompile(`Stream #\d+:\d+(?:\[[^\]]*\])?.*?: Audio: `)
)

// Probe reads the stream properties from ffmpeg's banner. It is one short-lived
// process per file, run once when a media node loads.
func Probe(src string) (Info, error) {
	bin, err := FFmpeg()
	if err != nil {
		return Info{}, err
	}
	args := []string{"-nostdin", "-hide_banner"}
	if IsURL(src) {
		// A network stream keeps its input open, so without an output ffmpeg
		// would read until the timeout. Half a second of decoded stream is
		// enough for the banner, and it makes a live source open in a blink.
		args = append(args, "-i", src, "-t", "0.5", "-f", "null", "-")
	} else {
		args = append(args, "-i", src)
	}
	cmd := exec.Command(bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	done := make(chan struct{})
	go func() {
		_ = cmd.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(probeTimeout):
		_ = cmd.Process.Kill()
		<-done
	}
	text := stderr.String()
	var info Info
	if m := durationRe.FindStringSubmatch(text); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		sec, _ := strconv.ParseFloat(m[3], 64)
		info.Duration = time.Duration((float64(h*3600+min*60) + sec) * float64(time.Second))
	}
	if m := videoRe.FindStringSubmatch(text); m != nil {
		info.Width, _ = strconv.Atoi(m[1])
		info.Height, _ = strconv.Atoi(m[2])
		info.HasVideo = info.Width > 0 && info.Height > 0
		if f := fpsRe.FindStringSubmatch(text); f != nil {
			info.FPS, _ = strconv.ParseFloat(f[1], 64)
		}
	}
	info.AudioTracks = len(audioRe.FindAllString(text, -1))
	info.HasAudio = info.AudioTracks > 0
	if !info.HasVideo && !info.HasAudio {
		// ffmpeg writes the reason on the first line of its banner.
		msg := firstLine(text)
		if msg == "" {
			msg = "no audio or video stream"
		}
		return Info{}, fmt.Errorf("media: %s: %s", filepath.Base(src), msg)
	}
	return info, nil
}

// firstLine returns the banner line that explains a failure ("No such file…").
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ffmpeg version") ||
			strings.HasPrefix(line, "built with") ||
			strings.HasPrefix(line, "configuration:") ||
			strings.HasPrefix(line, "lib") {
			continue
		}
		return line
	}
	return ""
}

// Video streams raw RGBA frames of exactly W by H pixels.
type Video struct {
	cmd *exec.Cmd
	out io.ReadCloser
	// W, H and FrameBytes are the decoded frame size.
	W, H       int
	FrameBytes int
	info       Info
	stderr     *strings.Builder
	closeOnce  sync.Once
}

// OpenVideo starts decoding src at w by h, from offset. A zero offset starts at
// the beginning. hw asks ffmpeg for hardware decoding; the caller falls back by
// opening again without it, because a machine, a driver or a codec may refuse.
func OpenVideo(src string, w, h int, offset time.Duration, hw bool) (*Video, error) {
	bin, err := FFmpeg()
	if err != nil {
		return nil, err
	}
	if w < 2 || h < 2 {
		return nil, fmt.Errorf("media: video size %dx%d is too small", w, h)
	}
	w &^= 1 // yuv420 wants even dimensions; rgba does not care, but the scaler does
	h &^= 1
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	if hw {
		// "auto" asks for whatever the platform offers (VideoToolbox, D3D11,
		// VAAPI, NVDEC) and falls back to software when the device cannot be
		// opened. Frames are downloaded to system memory for the rgba output.
		args = append(args, "-hwaccel", "auto")
	}
	if offset > 0 {
		args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
	}
	args = append(args,
		"-i", src,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d", w, h),
		"-f", "rawvideo", "-pix_fmt", "rgba",
		"pipe:1",
	)
	cmd := exec.Command(bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	return &Video{cmd: cmd, out: stdout, W: w, H: h, FrameBytes: w * h * 4, stderr: &stderr}, nil
}

// Info returns the stream properties a previous Probe found.
func (v *Video) Info() Info { return v.info }

// SetInfo records the probe result, so a caller can keep the duration and the
// frame rate next to the decoder.
func (v *Video) SetInfo(i Info) { v.info = i }

// Read fills p with one frame. It returns io.EOF at the end of the file and
// io.ErrUnexpectedEOF when the decoder stopped in the middle of a frame.
func (v *Video) Read(p []byte) (int, error) {
	if len(p) < v.FrameBytes {
		return 0, fmt.Errorf("media: frame buffer is %d bytes, want %d", len(p), v.FrameBytes)
	}
	n, err := io.ReadFull(v.out, p[:v.FrameBytes])
	if err == io.EOF {
		return 0, io.EOF
	}
	if err != nil {
		// A killed process is a seek or a stop, not a decode failure.
		if msg := strings.TrimSpace(v.stderr.String()); msg != "" {
			return n, fmt.Errorf("media: %s", firstLine(msg))
		}
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

// Close stops the decoder and reaps the process.
func (v *Video) Close() error {
	var err error
	v.closeOnce.Do(func() {
		if v.out != nil {
			_ = v.out.Close()
		}
		if v.cmd != nil && v.cmd.Process != nil {
			_ = v.cmd.Process.Kill()
		}
		if v.cmd != nil {
			err = v.cmd.Wait()
		}
	})
	return err
}

// PCM streams little-endian signed 16-bit samples.
type PCM struct {
	cmd       *exec.Cmd
	out       io.ReadCloser
	buf       *bufio.Reader
	Rate      int
	Channels  int
	stderr    *strings.Builder
	closeOnce sync.Once
}

// OpenPCM starts decoding the audio of src at rate and channels, from offset.
// track selects the audio stream, 0 being the first, which is how a film's
// language tracks are offered.
func OpenPCM(src string, rate, channels int, offset time.Duration, track int) (*PCM, error) {
	bin, err := FFmpeg()
	if err != nil {
		return nil, err
	}
	if rate <= 0 {
		rate = 48000
	}
	if channels <= 0 {
		channels = 2
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	if offset > 0 {
		args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
	}
	mapArg := "0:a:0"
	if track > 0 {
		mapArg = "0:a:" + strconv.Itoa(track)
	}
	args = append(args,
		"-i", src,
		"-vn",
		"-map", mapArg,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", strconv.Itoa(rate),
		"-ac", strconv.Itoa(channels),
		"pipe:1",
	)
	cmd := exec.Command(bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	return &PCM{cmd: cmd, out: stdout, buf: bufio.NewReaderSize(stdout, 64*1024),
		Rate: rate, Channels: channels, stderr: &stderr}, nil
}

// Read fills b with samples. It blocks until the buffer is full, so a caller
// always hands the audio device whole chunks; the end of the file comes back as
// a short read.
func (p *PCM) Read(b []byte) (int, error) { return io.ReadFull(p.buf, b) }

// Close stops the decoder and reaps the process.
func (p *PCM) Close() error {
	var err error
	p.closeOnce.Do(func() {
		if p.out != nil {
			_ = p.out.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		if p.cmd != nil {
			err = p.cmd.Wait()
		}
	})
	return err
}
