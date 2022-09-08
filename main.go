package main

import (
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/yapingcat/gomedia/go-mpeg2"
	"log"
	"net/http"
	"time"
)

func main() {
	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/c", handleCreatePeerConnection)
	panic(http.ListenAndServe(":9080", nil))
}

func setupResponse(w *http.ResponseWriter, _ *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func handleCreatePeerConnection(w http.ResponseWriter, r *http.Request) {
	setupResponse(&w, r)
	source := r.URL.Query().Get("source")

	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		fmt.Println("Failed to parse json peerConnection config:", err)
		return
	}

	description := createProxyPeer(source, offer)

	response, err := json.Marshal(description)
	if err != nil {
		panic(err)
	}
	if _, err = w.Write(response); err != nil {
		panic(err)
	}
}

func createProxyPeer(source string, offer webrtc.SessionDescription) *webrtc.SessionDescription {
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		panic(err)
	}

	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if err != nil {
		panic(err)
	}
	if _, err = peerConnection.AddTrack(videoTrack); err != nil {
		panic(err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio", "pion")
	if err != nil {
		panic(err)
	}
	if _, err = peerConnection.AddTrack(audioTrack); err != nil {
		panic(err)
	}

	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	} else if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}
	<-gatherComplete

	go startMpegTsProxy(peerConnection, videoTrack, audioTrack, source)

	return peerConnection.LocalDescription()
}

func startMpegTsProxy(connection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample, audioTrack *webrtc.TrackLocalStaticSample, source string) {
	log.Println("Starting ts proxy to", source)
	resp, err := http.Get(source)
	if err != nil {
		log.Println("Failed to connect to ts stream", source)
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
				fmt.Println("Failed to send frame:", err)
			}
		} else if cid == mpeg2.TS_STREAM_AAC {
			//fmt.Println("hello")
			//if !foundAudio {
			//	foundAudio = true
			//}
			//n, err := aacFileFd.Write(frame)
			//if err != nil || n != len(frame) {
			//	fmt.Println(err)
			//}
		}
	}
	err = demuxer.Input(resp.Body)
	if err != nil {
		fmt.Println("Failed to read:", err)
		return
	}
}
