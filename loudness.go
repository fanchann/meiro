package main

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/elianiva/meiro/youtube"
)

// loudnessTimeout bounds one request for a track's loudness.
const loudnessTimeout = 15 * time.Second

// applyLoudness sets the current track's gain from what is known of it, and
// the player's volume to match.
func (a *app) applyLoudness() {
	value, ok := a.loudnessMap.Get(a.current.VideoID)
	a.loudness, a.loudnessOK = value.LoudnessDB, ok
	a.player.SetVolume(playbackVolume(a.volume, a.loudness, a.loudnessOK && a.settings.NormalizeLoudness, a.settings.LogVolume))
}

// loudnessWait bounds how long the audio waits for a track's loudness before it
// starts. The lookup usually lands while the stream is still being found, so
// the wait is rarely paid in full.
const loudnessWait = 2 * time.Second

// loudnessClient returns the client to ask for a track's loudness, or nil
// when normalization is off, nobody is signed in, or the value is known.
func (a *app) loudnessClient(videoID string) *youtube.Client {
	if a.account.session == nil || !a.settings.NormalizeLoudness {
		return nil
	}
	if _, ok := a.loudnessMap.Get(videoID); ok {
		return nil
	}
	return a.account.session.client
}

// fetchLoudness asks for a track's loudness without holding up playback, and
// applies it if the track is still the current one when it arrives. load is
// the task that owns the request, so that a newer one replaces it.
func (a *app) fetchLoudness(item youtube.MusicItem, load *task) {
	client := a.loudnessClient(item.VideoID)
	if client == nil {
		return
	}
	startTask(a, load, loudnessTimeout, func(ctx context.Context) (youtube.Loudness, error) {
		return client.Loudness(ctx, item.VideoID)
	}, func(value youtube.Loudness, err error) {
		if err != nil {
			log.Printf("playback: loudness unavailable video_id=%s: %v", item.VideoID, err)
			return
		}
		a.loudnessMap.Put(item.VideoID, value)
		if a.current.VideoID == item.VideoID {
			a.applyLoudness()
			a.syncSystemMedia()
		}
	})
}

// playbackVolume applies a track's loudness correction without changing the
// volume the user chose. loudness is YouTube's dB value for the track. With
// logarithmic set, the slider position follows a curve that spans 60 dB, so the
// low end moves in finer steps.
func playbackVolume(volume, loudness float64, normalize, logarithmic bool) float64 {
	volume /= 100
	if logarithmic {
		volume = (math.Pow(10, 3*volume) - 1) / 999
	}
	gain := 1.0
	if normalize {
		gain = math.Pow(10, -loudness/20)
	}
	return min(max(volume*gain, 0), 1)
}

// setNormalizeLoudness turns loudness normalization on or off, and keeps it
// for the next run.
func (a *app) setNormalizeLoudness(enabled bool) {
	if a.settings.NormalizeLoudness == enabled {
		return
	}
	a.settings.NormalizeLoudness = enabled
	a.saveSettings()
	a.applyLoudness()
	if enabled && a.current.VideoID != "" {
		a.fetchLoudness(a.current, &a.loudnessLoad)
	}
}
