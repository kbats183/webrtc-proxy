package main

import (
	"github.com/pion/webrtc/v3"
	"github.com/yapingcat/gomedia/go-codec"
	"sync"
)

type WebRtcConsumer struct {
	clientId   string
	isReady    bool
	videoTrack *webrtc.TrackLocalStaticSample
	mtx        sync.Mutex
	framesList []*MediaFrame
	frameCome  chan struct{}
}

func (wc *WebRtcConsumer) init() {
	wc.frameCome = make(chan struct{}, 1)
}

func (wc *WebRtcConsumer) getId() string {
	return wc.clientId
}

func (wc *WebRtcConsumer) ready() bool {
	return wc.isReady
}

func (wc *WebRtcConsumer) play(frame *MediaFrame) {
	if wc.isReady {
		if frame.cid != codec.CODECID_VIDEO_H264 || !codec.IsH264IDRFrame(frame.frame) {
			return
		}
		wc.mtx.Lock()
		wc.framesList = append(wc.framesList, frame)
		wc.mtx.Unlock()
		select {
		case wc.frameCome <- struct{}{}:
		default:
		}
	}
}
