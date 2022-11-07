package main

import (
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/yapingcat/gomedia/go-codec"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	//getWebrtcApi := getWebrtcApi()
	stopCh := make(chan struct{})
	go startRtmpServer(stopCh)
	go startHttpStream(stopCh, nil)
	<-stopCh
}

type httpServer struct {
	connections proxyConnections
	webrtc      *webrtc.API
}

func (hs *httpServer) handleCreatePeerConnection(w http.ResponseWriter, r *http.Request) {
	setupResponse(&w, r)
	source := r.URL.Query().Get("source")

	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		fmt.Println("Failed to parse json peerConnection config:", err)
		return
	}

	description := hs.createProxyPeer(source, offer)

	response, err := json.Marshal(description)
	if err != nil {
		panic(err)
	}
	if _, err = w.Write(response); err != nil {
		panic(err)
	}
}

func startHttpStream(closeCh chan<- struct{}, webrtcApi *webrtc.API) {
	log.Println("Starting HTTP server ...")

	server := httpServer{
		connections: proxyConnections{},
		webrtc:      webrtcApi,
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(".")))
	mux.HandleFunc("/c", server.handleCreatePeerConnection)
	mux.HandleFunc("/status", server.handleConnectionsStatus)
	mux.HandleFunc("/mixer/select", server.handleRTMPMixerSelect)
	err := http.ListenAndServe(":9080", mux)
	close(closeCh)
	log.Fatal(err)
}

func setupResponse(w *http.ResponseWriter, _ *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	(*w).Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func (hs *httpServer) handleConnectionsStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	connectionsStatus := hs.connections.GetStatus()
	_, _ = w.Write([]byte("Active streams:"))
	for _, status := range connectionsStatus {
		_, _ = w.Write([]byte("\n"))
		_, _ = w.Write([]byte(status))
	}
}

func (hs *httpServer) createProxyPeer(source string, offer webrtc.SessionDescription) *webrtc.SessionDescription {
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
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

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA}, "audio", "pion")
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

	if strings.Index(source, "http") == 0 {
		proxyConnection := hs.connections.NewConnection(peerConnection)
		go startMpegTsProxy(peerConnection, videoTrack, audioTrack, source, proxyConnection)
	} else if producer := rtmpCenter.find(source); producer != nil {
		proxyConnection := hs.connections.NewConnection(peerConnection)
		go startRtmpProxy(peerConnection, videoTrack, audioTrack, producer, proxyConnection)
	} else {
		log.Printf("Source %s not found", source)
	}

	return peerConnection.LocalDescription()
}

func (hs *httpServer) handleRTMPMixerSelect(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	rtmpCenter.mixer.SelectProducer(stream)
	_, _ = w.Write([]byte("ok"))
}

func startRtmpProxy(connection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample, _ *webrtc.TrackLocalStaticSample, producer RtmpProducer, proxyConnection *proxyConnection) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("panic occurred:", err)
		}
	}()

	consumer := WebRtcConsumer{
		clientId:   proxyConnection.key,
		videoTrack: videoTrack,
	}
	consumer.init()
	producer.AddConsumer(&consumer)

	log.Printf("Start fetching frame from RTMP for %s\n", proxyConnection.key)

	connection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		proxyConnection.ChangeState(state)
		if state == webrtc.PeerConnectionStateDisconnected {
			consumer.close()
			producer.RemoveConsumer(proxyConnection.key)
			proxyConnection.Close()
		}
	})

	firstFrame := true
	for {
		frame, running := <-consumer.frameChan
		if !running {
			return
		}
		if firstFrame { //wait for I frame
			if frame.cid == codec.CODECID_VIDEO_H264 {
				if !codec.IsH264IDRFrame(frame.frame) {
					continue
				}
				firstFrame = false
			} else {
				continue
			}
		}
		_ = videoTrack.WriteSample(media.Sample{
			Duration: time.Duration(frame.pts) * time.Millisecond,
			Data:     frame.frame,
		})
	}
}
