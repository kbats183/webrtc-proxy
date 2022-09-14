package main

import (
	"fmt"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/yapingcat/gomedia/go-mpeg2"
	"net/http"
	"time"
)

func startMpegTsProxy(connection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample, _ *webrtc.TrackLocalStaticSample, source string, proxyConnection *proxyConnection) {
	resp, err := http.Get(source)
	if err != nil {
		proxyConnection.HandleError(fmt.Errorf("failed to connect to ts stream %s", source))
		proxyConnection.Close()
		return
	}

	demuxer := mpeg2.NewTSDemuxer()
	demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		if cid == mpeg2.TS_STREAM_H264 {
			err := videoTrack.WriteSample(media.Sample{
				Data:     frame,
				Duration: time.Second / 30,
			})
			if err != nil {
				proxyConnection.HandleError(fmt.Errorf("failed to send frame %s", err.Error()))
			}
		} else if cid == mpeg2.TS_STREAM_AAC {
		}
	}

	connection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		proxyConnection.ChangeState(state)
		if state == webrtc.PeerConnectionStateDisconnected {
			_ = resp.Body.Close()
			proxyConnection.Close()
		}
	})

	err = demuxer.Input(resp.Body)
	if err != nil {
		proxyConnection.HandleError(fmt.Errorf("failed to demux input %s", err.Error()))
		proxyConnection.Close()
	}
}
