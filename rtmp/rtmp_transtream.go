package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type TransStream struct {
	stream.TransStreamImpl
	chunkSize  int
	header     []byte //音视频头chunk
	headerSize int
	muxer      *libflv.Muxer

	audioChunk librtmp.Chunk
	videoChunk librtmp.Chunk

	memoryPool     stream.MemoryPool
	transBuffer    stream.StreamBuffer
	lastTs         int64
	chunkSizeQueue *stream.Queue
}

var nextOffset int

func (t *TransStream) Input(packet utils.AVPacket) {
	utils.Assert(t.TransStreamImpl.Completed)

	var data []byte
	var chunk *librtmp.Chunk
	var videoPkt bool
	var length int
	//rtmp chunk消息体的数据大小
	var payloadSize int

	if utils.AVMediaTypeAudio == packet.MediaType() {
		data = packet.Data()
		length = len(data)
		chunk = &t.audioChunk
		payloadSize += 2 + length
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		videoPkt = true
		data = packet.AVCCPacketData()
		length = len(data)
		chunk = &t.videoChunk
		payloadSize += 5 + length
	}

	//即不开启GOP缓存又不合并发送. 直接使用AVPacket的预留头封装发送
	if !stream.AppConfig.GOPCache && stream.AppConfig.MergeWriteLatency < 1 {
		//首帧视频帧必须要
	} else {

	}

	//payloadSize += payloadSize / t.chunkSize
	//分配内存
	t.memoryPool.Mark()
	allocate := t.memoryPool.Allocate(12 + payloadSize + (payloadSize / t.chunkSize))

	//写chunk头
	chunk.Length = payloadSize
	chunk.Timestamp = uint32(packet.Dts())
	n := chunk.ToBytes(allocate)
	utils.Assert(n == 12)

	//写flv
	ct := packet.Pts() - packet.Dts()
	if videoPkt {
		n += t.muxer.WriteVideoData(allocate[12:], uint32(ct), packet.KeyFrame(), false)
	} else {
		n += t.muxer.WriteAudioData(allocate[12:], false)
	}

	first := true
	var min int
	for length > 0 {
		if first {
			min = utils.MinInt(length, t.chunkSize-5)
			first = false
		} else {
			min = utils.MinInt(length, t.chunkSize)
		}

		copy(allocate[n:], data[:min])
		n += min

		length -= min
		data = data[min:]

		//写一个ChunkType3用作分割
		if length > 0 {
			if videoPkt {
				allocate[n] = (0x3 << 6) | byte(librtmp.ChunkStreamIdVideo)
			} else {
				allocate[n] = (0x3 << 6) | byte(librtmp.ChunkStreamIdAudio)
			}
			n++
		}
	}

	rtmpData := t.memoryPool.Fetch()[:n]
	ret := true
	if stream.AppConfig.GOPCache {
		//ret = t.transBuffer.AddPacket(rtmpData, packet.KeyFrame() && videoPkt, packet.Dts())
		ret = t.transBuffer.AddPacket(packet, packet.KeyFrame() && videoPkt, packet.Dts())
	}

	if !ret || stream.AppConfig.GOPCache {
		t.memoryPool.FreeTail()
	}

	if ret {
		//发送给sink
		mergeWriteLatency := int64(350)

		if mergeWriteLatency == 0 {
			for _, sink := range t.Sinks {
				sink.Input(rtmpData)
			}

			return
		}

		t.chunkSizeQueue.Push(len(rtmpData))
		//if t.lastTs == 0 {
		//	t.transBuffer.Peek(0).(utils.AVPacket).Dts()
		//}

		endTs := t.lastTs + mergeWriteLatency
		if t.transBuffer.Peek(t.transBuffer.Size()-1).(utils.AVPacket).Dts() < endTs {
			return
		}

		head, tail := t.memoryPool.Data()
		sizeHead, sizeTail := t.chunkSizeQueue.Data()
		var offset int
		var size int
		var chunkSize int
		var lastTs int64
		var tailIndex int
		for i := 0; i < t.transBuffer.Size(); i++ {
			pkt := t.transBuffer.Peek(i).(utils.AVPacket)

			if i < len(sizeHead) {
				chunkSize = sizeHead[i].(int)
			} else {
				chunkSize = sizeTail[tailIndex].(int)
				tailIndex++
			}

			if pkt.Dts() <= t.lastTs && t.lastTs != 0 {
				offset += chunkSize
				continue
			}

			if pkt.Dts() > endTs {
				break
			}

			size += chunkSize
			lastTs = pkt.Dts()
		}
		t.lastTs = lastTs

		if nextOffset == 0 {
			nextOffset = size
		} else {
			utils.Assert(offset == nextOffset)
			nextOffset += size
		}

		//后面再优化只发送一次
		var data1 []byte
		var data2 []byte
		if offset > len(head) {
			offset -= len(head)
			head = tail
			tail = nil
		}
		if offset+size > len(head) {
			data1 = head[offset:]
			size -= len(head[offset:])
			data2 = tail[:size]
		} else {
			data1 = head[offset : offset+size]
		}

		for _, sink := range t.Sinks {
			if data1 != nil {
				sink.Input(data1)
			}

			if data2 != nil {
				sink.Input(data2)
			}
		}
	}
}

func (t *TransStream) AddSink(sink stream.ISink) {
	t.TransStreamImpl.AddSink(sink)

	utils.Assert(t.headerSize > 0)
	sink.Input(t.header[:t.headerSize])
	if !stream.AppConfig.GOPCache {
		return
	}

	//开启GOP缓存的情况下
	//开启合并写的情况下:
	// 如果合并写大小每满一次
	// if stream.AppConfig.GOPCache > 0 {
	// 	t.transBuffer.PeekAll(func(packet interface{}) {
	// 		sink.Input(packet.([]byte))
	// 	})
	// }
}

func (t *TransStream) onDiscardPacket(pkt interface{}) {
	t.memoryPool.FreeHead()
	size := t.chunkSizeQueue.Pop().(int)
	nextOffset -= size
}

func (t *TransStream) WriteHeader() error {
	utils.Assert(t.Tracks != nil)
	utils.Assert(!t.TransStreamImpl.Completed)

	var audioStream utils.AVStream
	var videoStream utils.AVStream
	var audioCodecId utils.AVCodecID
	var videoCodecId utils.AVCodecID

	for _, track := range t.Tracks {
		if utils.AVMediaTypeAudio == track.Type() {
			audioStream = track
			audioCodecId = audioStream.CodecId()
			t.audioChunk = librtmp.NewAudioChunk()
		} else if utils.AVMediaTypeVideo == track.Type() {
			videoStream = track
			videoCodecId = videoStream.CodecId()
			t.videoChunk = librtmp.NewVideoChunk()
		}
	}

	utils.Assert(audioStream != nil || videoStream != nil)

	//初始化
	t.TransStreamImpl.Completed = true
	t.header = make([]byte, 1024)
	t.muxer = libflv.NewMuxer(audioCodecId, videoCodecId, 0, 0, 0)
	t.memoryPool = stream.NewMemoryPoolWithRecopy(1024 * 4000)
	if stream.AppConfig.GOPCache {
		t.transBuffer = stream.NewStreamBuffer(200)
		t.transBuffer.SetDiscardHandler(t.onDiscardPacket)
	}

	var n int
	if audioStream != nil {
		n += t.muxer.WriteAudioData(t.header[12:], true)
		extra := audioStream.Extra()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.audioChunk.Length = n
		t.audioChunk.ToBytes(t.header)
		n += 12
	}

	if videoStream != nil {
		tmp := n
		n += t.muxer.WriteVideoData(t.header[n+12:], 0, false, true)
		extra := videoStream.Extra()
		copy(t.header[n+12:], extra)
		n += len(extra)

		t.videoChunk.Length = 5 + len(extra)
		t.videoChunk.ToBytes(t.header[tmp:])
		n += 12
	}

	t.headerSize = n
	return nil
}

func NewTransStream(chunkSize int) stream.ITransStream {
	transStream := &TransStream{chunkSize: chunkSize, TransStreamImpl: stream.TransStreamImpl{Sinks: make(map[stream.SinkId]stream.ISink, 64)}}
	transStream.chunkSizeQueue = stream.NewQueue(512)
	return transStream
}
