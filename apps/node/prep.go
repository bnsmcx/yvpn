package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Preparing a video means making one file that every device in the family can
// play directly: H.264 video no taller than 1080p, AAC audio, in an MP4 with
// its index at the front so playback starts before the download ends. Direct
// play is what lets a 2-vCPU droplet host a watch party: Jellyfin never has to
// transcode live, for anyone.
//
// ffmpeg and ffprobe come from the Jellyfin image, which already ships a build
// with libx264, so there is nothing extra to install.
const (
	ffmpegPath  = "/usr/lib/jellyfin-ffmpeg/ffmpeg"
	ffprobePath = "/usr/lib/jellyfin-ffmpeg/ffprobe"
)

type probeStream struct {
	Index       int    `json:"index"`
	CodecType   string `json:"codec_type"`
	CodecName   string `json:"codec_name"`
	Profile     string `json:"profile"`
	PixFmt      string `json:"pix_fmt"`
	Height      int    `json:"height"`
	Disposition struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type probeResult struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func parseProbe(out string) (probeResult, error) {
	var p probeResult
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return p, fmt.Errorf("could not read ffprobe output: %v", err)
	}
	return p, nil
}

// Duration in seconds, or 0 when the container does not say.
func (p probeResult) Duration() float64 {
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	return d
}

// prepPlan is what ffmpeg will be asked to do, and why.
type prepPlan struct {
	Args       []string
	CopyVideo  bool
	CopyAudio  bool
	Downscaled bool
}

// planPrep chooses the cheapest conversion that still plays everywhere. A video
// stream that is already 8-bit H.264 at 1080p or less is copied untouched, which
// takes seconds rather than most of the film's running time; so is AAC audio.
func planPrep(p probeResult, in, out string) (prepPlan, error) {
	var v, a *probeStream
	for i := range p.Streams {
		s := &p.Streams[i]
		switch {
		// Cover art inside an MKV or MP3 is a "video" stream of one frame.
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && v == nil:
			v = s
		case s.CodecType == "audio" && a == nil:
			a = s
		}
	}
	if v == nil {
		return prepPlan{}, errors.New("this file has no video in it")
	}

	plan := prepPlan{
		CopyVideo: v.CodecName == "h264" && v.PixFmt == "yuv420p" && v.Height > 0 && v.Height <= 1080,
		CopyAudio: a != nil && a.CodecName == "aac",
	}
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y", "-i", in,
		"-map", fmt.Sprintf("0:%d", v.Index)}
	if a != nil {
		args = append(args, "-map", fmt.Sprintf("0:%d", a.Index))
	}

	if plan.CopyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		// Even dimensions are an H.264 requirement; anything taller than 1080p
		// is brought down to it, which also keeps the encode quick.
		scale := "scale=trunc(iw/2)*2:trunc(ih/2)*2"
		if v.Height > 1080 {
			scale = "scale=-2:1080"
			plan.Downscaled = true
		}
		args = append(args, "-vf", scale, "-c:v", "libx264", "-preset", "veryfast",
			"-crf", "21", "-pix_fmt", "yuv420p", "-profile:v", "high")
	}
	if a != nil {
		if plan.CopyAudio {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
		}
	}
	// Subtitle and data streams rarely survive the trip into MP4 and are not
	// what a watch party needs; leaving them out keeps the result playable.
	args = append(args, "-sn", "-dn", "-movflags", "+faststart",
		"-progress", "pipe:1", "-nostats", out)
	plan.Args = args
	return plan, nil
}

// progressFrom reads one line of ffmpeg's -progress output and reports how far
// through the file it is, as a fraction; ok is false for lines that say nothing
// about that. The fraction stops short of 1 until ffmpeg says it is done.
func progressFrom(line string, duration float64) (frac float64, ok bool) {
	k, v, found := strings.Cut(strings.TrimSpace(line), "=")
	if !found {
		return 0, false
	}
	switch k {
	case "progress":
		if v == "end" {
			return 1, true
		}
	case "out_time_us", "out_time_ms": // both are microseconds, despite the name
		us, err := strconv.ParseFloat(v, 64)
		if err != nil || duration <= 0 || us < 0 {
			return 0, false
		}
		f := us / (duration * 1e6)
		if f > 0.99 {
			f = 0.99
		}
		return f, true
	}
	return 0, false
}
