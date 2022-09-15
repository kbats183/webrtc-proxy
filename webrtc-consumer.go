package main

import (
	"github.com/pion/webrtc/v3"
	"github.com/yapingcat/gomedia/go-codec"
	"sync"
	"sync/atomic"
)

type WebRtcConsumer struct {
	clientId   string
	isReady    atomic.Bool
	videoTrack *webrtc.TrackLocalStaticSample
	mtx        sync.Mutex
	frameChan  chan *MediaFrame
}

func (wc *WebRtcConsumer) init() {
	wc.frameChan = make(chan *MediaFrame, 100)
	wc.isReady.Store(true)
}

func (wc *WebRtcConsumer) getId() string {
	return wc.clientId
}

func (wc *WebRtcConsumer) ready() bool {
	return wc.isReady.Load()
}

func (wc *WebRtcConsumer) play(frame *MediaFrame) {
	if wc.isReady.Load() {
		if frame.cid != codec.CODECID_VIDEO_H264 {
			return
		}
		wc.frameChan <- frame
	}
}

func (wc *WebRtcConsumer) close() {
	wc.isReady.Store(false)
	close(wc.frameChan)
}
