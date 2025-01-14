package hls

import (
	"bytes"
	"github.com/lkmio/lkm/collections"
	"math"
	"strconv"
)

const (
	ExtM3u      = "EXTM3U"
	ExtXVersion = "EXT-X-VERSION" //在文件中唯一

	ExtINF              = "EXTINF"              //<duration>(浮点类型, 版本小于3用整型),[<title>]
	ExXByteRange        = "EXT-X-BYTERANGE"     //版本4及以上,分片位置
	ExtXDiscontinuity   = "EXT-X-DISCONTINUITY" //后面的切片不连续, 文件格式、时间戳发生变化
	ExtXKey             = "EXT-X-KEY"           //加密使用
	ExtXMap             = "EXT-X-MAP"           //音视频的元数据
	ExtXProgramDateTime = "EXT-X-PROGRAM-DATE-TIME"
	ExtXDateRange       = "EXT-X-DATERANGE"

	ExtXTargetDuration        = "EXT-X-TARGETDURATION" //切片最大时长, 整型单位秒
	ExtXMediaSequence         = "EXT-X-MEDIA-SEQUENCE" //第一个切片序号
	ExtXDiscontinuitySequence = "EXT-X-DISCONTINUITY-SEQUENCE"
	ExtXEndList               = "EXT-X-ENDLIST"
	ExtXPlaylistType          = "EXT-X-PLAYLIST-TYPE"
	ExtXIFramesOnly           = "EXT-X-I-FRAMES-ONLY"

	ExtXMedia           = "EXT-X-MEDIA"
	ExtXStreamINF       = "EXT-X-STREAM-INF"
	ExtXIFrameStreamINF = "EXT-X-I-FRAME-STREAM-INF"
	ExtXSessionData     = "EXT-X-SESSION-DATA"
	ExtXSessionKey      = "EXT-X-SESSION-KEY"

	ExtXIndependentSegments = "EXT-X-INDEPENDENT-SEGMENTS"
	ExtXStart               = "EXT-X-START"
)

//HttpContent-Type头必须是"application/vnd.apple.mpegurl"或"audio/mpegurl"
//无BOM

type M3U8Writer interface {
	// AddSegment 添加切片
	//@Params  duration 切片时长
	//@Params  url m3u8列表中切片的url
	//@Params  sequence m3u8列表中的切片序号
	//@Params  path 切片位于磁盘中的绝对路径
	AddSegment(duration float32, url string, sequence int, path string)

	ToString() string

	// Size 返回切片文件个数
	Size() int

	// Head 返回第一个切片文件
	Head() Segment
}

func NewM3U8Writer(len int) M3U8Writer {
	return &m3u8Writer{
		stringBuffer: bytes.NewBuffer(make([]byte, 0, 1024*10)),
		playlist:     collections.NewQueue(len),
	}
}

type Segment struct {
	duration float32
	url      string
	sequence int
	path     string
}

type m3u8Writer struct {
	stringBuffer *bytes.Buffer
	playlist     *collections.Queue
}

func (m *m3u8Writer) AddSegment(duration float32 /*title string,*/, url string, sequence int, path string) {
	if m.playlist.IsFull() {
		m.playlist.Pop()
	}

	m.playlist.Push(Segment{duration: duration, url: url, sequence: sequence, path: path})
}

func (m *m3u8Writer) targetDuration() int {
	var targetDuration int
	head, tail := m.playlist.Data()

	compute := func(playlist []interface{}) {
		for _, segment := range playlist {
			//影响播放器缓存.
			round := int(math.Ceil(float64(segment.(Segment).duration)))
			if round > targetDuration {
				targetDuration = round
			}
		}
	}

	if head != nil {
		compute(head)
	}

	if tail != nil {
		compute(tail)
	}

	return targetDuration
}

func (m *m3u8Writer) ToString() string {
	//暂时只实现简单的播放列表
	head, tail := m.playlist.Data()
	if head == nil {
		return ""
	}

	m.stringBuffer.Reset()
	m.stringBuffer.WriteString("#EXTM3U\r\n")
	//暂时只实现第三个版本
	m.stringBuffer.WriteString("#EXT-X-VERSION:3\r\n")
	m.stringBuffer.WriteString("#EXT-X-TARGETDURATION:")
	m.stringBuffer.WriteString(strconv.Itoa(m.targetDuration()))
	m.stringBuffer.WriteString("\r\n")
	m.stringBuffer.WriteString("#EXT-X-MEDIA-SEQUENCE:")
	m.stringBuffer.WriteString(strconv.Itoa(head[0].(Segment).sequence))
	m.stringBuffer.WriteString("\r\n")

	appendSegments := func(playlist []interface{}) {
		for _, segment := range playlist {
			m.stringBuffer.WriteString("#EXTINF:")
			m.stringBuffer.WriteString(strconv.FormatFloat(float64(segment.(Segment).duration), 'f', -1, 32))
			m.stringBuffer.WriteString(",\r\n")
			m.stringBuffer.WriteString(segment.(Segment).url + "%s")
			m.stringBuffer.WriteString("\r\n")
		}
	}

	if head != nil {
		appendSegments(head)
	}

	if tail != nil {
		appendSegments(tail)
	}

	return m.stringBuffer.String()
}

func (m *m3u8Writer) Size() int {
	var size int
	head, tail := m.playlist.Data()

	if head != nil {
		size += len(head)
	}

	if tail != nil {
		size += len(tail)
	}

	return size
}

func (m *m3u8Writer) Head() Segment {
	head, _ := m.playlist.Data()
	if head != nil {
		return head[0].(Segment)
	}

	return Segment{}
}
