package main

import (
	"fmt"
	"github.com/yapingcat/gomedia/go-codec"
	_ "github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-rtmp"
	"log"
	"net"
	"sync"
)

type RtmpCenter struct {
	streams map[string]*RtmpProducer
	mtx     sync.Mutex
}

func (center *RtmpCenter) find(name string) *RtmpProducer {
	center.mtx.Lock()
	defer center.mtx.Unlock()
	if p, found := center.streams[name]; found {
		return p
	} else {
		return nil
	}
}

func (center *RtmpCenter) unRegister(name string) {
	center.mtx.Lock()
	defer center.mtx.Unlock()
	delete(center.streams, name)
}

func (center *RtmpCenter) register(name string, p *RtmpProducer) {
	center.mtx.Lock()
	defer center.mtx.Unlock()
	center.streams[name] = p
}

var rtmpCenter RtmpCenter

func init() {
	rtmpCenter = RtmpCenter{streams: make(map[string]*RtmpProducer)}
}

func startRtmpServer(closeCh chan<- struct{}) {
	log.Println("Starting RTMP Server")

	tcpAddr, _ := net.ResolveTCPAddr("tcp", ":1935")
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		close(closeCh)
		log.Fatalf("RTMP server start failed: %+v", err)
	}

	for {
		conn, _ := listener.Accept()
		sess := newRtmpSession(conn)
		sess.init()
		go sess.start()
	}
}

type RtmpSession struct {
	id         string
	mtx        sync.Mutex
	conn       net.Conn
	handle     *rtmp.RtmpServerHandle
	quit       chan struct{}
	frameCome  chan struct{}
	C          chan *MediaFrame
	source     *RtmpProducer
	frameLists []*MediaFrame
	isReady    bool
	die        sync.Once
}

func (sess *RtmpSession) getId() string {
	return sess.id
}

func newRtmpSession(conn net.Conn) *RtmpSession {
	id := randConnectionKey(10)
	return &RtmpSession{
		id:        id,
		conn:      conn,
		handle:    rtmp.NewRtmpServerHandle(),
		quit:      make(chan struct{}),
		frameCome: make(chan struct{}, 1),
		C:         make(chan *MediaFrame, 3000),
	}
}

func (sess *RtmpSession) init() {
	sess.handle.OnPublish(func(app, streamName string) rtmp.StatusCode {
		return rtmp.NETSTREAM_PUBLISH_START
	})

	sess.handle.SetOutput(func(b []byte) error {
		_, err := sess.conn.Write(b)
		return err
	})

	sess.handle.OnStateChange(func(newState rtmp.RtmpState) {
		if newState == rtmp.STATE_RTMP_PLAY_START {
			name := sess.handle.GetStreamName()
			log.Printf("RTMP play %s starting ...", name)
			source := rtmpCenter.find(name)
			sess.source = source
			if source != nil {
				source.addConsumer(sess)
				sess.isReady = true
				go sess.sendToClient()
			} else {
				log.Printf("RTMP source %s not found", name)
			}
		} else if newState == rtmp.STATE_RTMP_PUBLISH_START {
			sess.handle.OnFrame(func(cid codec.CodecID, pts, dts uint32, frame []byte) {
				f := &MediaFrame{
					cid:   cid,
					frame: frame,
					pts:   pts,
					dts:   dts,
				}
				sess.C <- f
			})
			name := sess.handle.GetStreamName()
			p := newRtmpProducer(name, sess)
			go p.dispatch()
			rtmpCenter.register(name, p)
		}
	})
}

func (sess *RtmpSession) sendToClient() {
	firstVideo := true
	for {
		select {
		case <-sess.frameCome:
			sess.mtx.Lock()
			frames := sess.frameLists
			sess.frameLists = nil
			sess.mtx.Unlock()
			for _, frame := range frames {
				if frame.cid != codec.CODECID_VIDEO_H264 && frame.cid != codec.CODECID_AUDIO_AAC {
					continue
				}
				if firstVideo { //wait for I frame
					if frame.cid == codec.CODECID_VIDEO_H264 {
						if !codec.IsH264IDRFrame(frame.frame) {
							continue
						}
						firstVideo = false
					} else {
						continue
					}
				}
				err := sess.handle.WriteFrame(frame.cid, frame.frame, frame.pts, frame.dts)
				if err != nil {
					sess.stop()
					return
				}
			}
		case <-sess.quit:
			return
		}
	}
}

func (sess *RtmpSession) stop() {
	sess.die.Do(func() {
		close(sess.quit)
		if sess.source != nil {
			sess.source.removeConsumer(sess.id)
			sess.source = nil
		}
		_ = sess.conn.Close()
	})
}

func (sess *RtmpSession) ready() bool {
	return sess.isReady
}

func (sess *RtmpSession) play(frame *MediaFrame) {
	sess.mtx.Lock()
	sess.frameLists = append(sess.frameLists, frame)
	sess.mtx.Unlock()
	select {
	case sess.frameCome <- struct{}{}:
	default:
	}
}

func (sess *RtmpSession) start() {
	defer sess.stop()
	for {
		buf := make([]byte, 65536)
		n, err := sess.conn.Read(buf)
		if err != nil {
			log.Printf("RTMP session read error %s", err.Error())
			return
		}
		err = sess.handle.Input(buf[:n])
		if err != nil {
			log.Printf("RTMP session input error %s", err.Error())
			fmt.Println(err)
			return
		}
	}
}

func newRtmpProducer(name string, sess *RtmpSession) *RtmpProducer {
	return &RtmpProducer{
		name:    name,
		session: sess,
		quit:    make(chan struct{}),
	}
}

type MediaFrame struct {
	cid   codec.CodecID
	frame []byte
	pts   uint32
	dts   uint32
}

func (f *MediaFrame) clone() *MediaFrame {
	tmp := &MediaFrame{
		cid: f.cid,
		pts: f.pts,
		dts: f.dts,
	}
	tmp.frame = make([]byte, len(f.frame))
	copy(tmp.frame, f.frame)
	return tmp
}

type RtmpProducer struct {
	name      string
	session   *RtmpSession
	consumers sync.Map // string -> MediaConsumer
	quit      chan struct{}
	die       sync.Once
}

func (producer *RtmpProducer) dispatch() {
	defer func() {
		fmt.Println("quit dispatch")
		producer.stop()
	}()
	for {
		select {
		case frame := <-producer.session.C:
			if frame == nil {
				continue
			}
			producer.consumers.Range(func(_, value any) bool {
				consumer := value.(MediaConsumer)
				if consumer.ready() {
					tmp := frame.clone()
					consumer.play(tmp)
				}
				return true
			})
		case <-producer.session.quit:
			return
		case <-producer.quit:
			return
		}
	}
}

func (producer *RtmpProducer) addConsumer(consumer MediaConsumer) {
	producer.consumers.Store(consumer.getId(), consumer)
}

func (producer *RtmpProducer) removeConsumer(id string) {
	producer.consumers.Delete(id)
}

func (producer *RtmpProducer) stop() {
	producer.die.Do(func() {
		close(producer.quit)
		rtmpCenter.unRegister(producer.name)
	})
}

type MediaConsumer interface {
	getId() string
	ready() bool
	play(tmp *MediaFrame)
}
