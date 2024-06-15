package rtc

import (
	"github.com/pion/webrtc/v3"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
)

type transStream struct {
	stream.BaseTransStream
}

func NewTransStream() stream.TransStream {
	t := &transStream{}
	return t
}

func TransStreamFactory(source stream.Source, protocol stream.Protocol, streams []utils.AVStream) (stream.TransStream, error) {
	return NewTransStream(), nil
}

func (t *transStream) Input(packet utils.AVPacket) error {
	if utils.AVMediaTypeAudio == packet.MediaType() {

	} else if utils.AVMediaTypeVideo == packet.MediaType() {

		for _, iSink := range t.Sinks {
			sink_ := iSink.(*sink)
			if sink_.state < webrtc.ICEConnectionStateConnected {
				continue
			}

			if packet.KeyFrame() {
				extra := t.BaseTransStream.Tracks[packet.Index()].CodecParameters().DecoderConfRecord().ToAnnexB()
				sink_.input(packet.Index(), extra, 0)
			}

			sink_.input(packet.Index(), packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()]), uint32(packet.Duration(1000)))
		}
	}

	return nil
}

func (t *transStream) AddSink(sink_ stream.Sink) error {
	//创建PeerConnection
	var videoTrack *webrtc.TrackLocalStaticSample
	rtcSink := sink_.(*sink)
	rtcSink.setTrackCount(len(t.Tracks))
	connection, err := webrtc.NewPeerConnection(webrtc.Configuration{})

	connection.OnICECandidate(func(candidate *webrtc.ICECandidate) {

	})

	for index, track := range t.Tracks {
		if utils.AVCodecIdH264 != track.CodecId() {
			continue
		}

		videoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
		if err != nil {
			panic(err)
		}

		if _, err := connection.AddTransceiverFromTrack(videoTrack, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			return err
		}

		if _, err = connection.AddTrack(videoTrack); err != nil {
			return err
		}

		rtcSink.addTrack(index, videoTrack)
	}

	if err = connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: rtcSink.offer}); err != nil {
		return err
	}

	complete := webrtc.GatheringCompletePromise(connection)
	answer, err := connection.CreateAnswer(nil)
	if err != nil {
		return err
	} else if err = connection.SetLocalDescription(answer); err != nil {
		return err
	}
	<-complete

	connection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		rtcSink.state = state
		if webrtc.ICEConnectionStateDisconnected > state {
			rtcSink.Close()
		}
	})

	rtcSink.peer = connection
	rtcSink.SendHeader([]byte(connection.LocalDescription().SDP))
	return t.BaseTransStream.AddSink(sink_)
}

func (t *transStream) WriteHeader() error {
	return nil
}