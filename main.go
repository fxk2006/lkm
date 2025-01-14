package main

import (
	"encoding/json"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/jt1078"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/record"
	"github.com/lkmio/lkm/rtc"
	"github.com/lkmio/lkm/rtsp"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"github.com/lkmio/lkm/rtmp"
	"github.com/lkmio/lkm/stream"
)

func readRunArgs() (map[string]string, map[string]string) {
	args := os.Args

	// 运行参数项优先级高于config.json参数项
	// --disable-rtmp 		--enable-rtmp=11935
	// --disable-rtsp 		--enable-rtsp
	// --disable-hls  		--enable-hls
	// --disable-webrtc 	--enable-webrtc=18000
	// --disable-gb28181 	--enable-gb28181
	// --disable-jt1078		--enable-jt1078=11078
	// --disable-hooks		--enable-hooks
	// --disable-record 	--enable-record

	disableOptions := map[string]string{}
	enableOptions := map[string]string{}
	for _, arg := range args {
		// 参数忽略大小写
		arg = strings.ToLower(arg)

		var option string
		var enable bool
		if strings.HasPrefix(arg, "--disable-") {
			option = arg[len("--disable-"):]
		} else if strings.HasPrefix(arg, "--enable-") {
			option = arg[len("--enable-"):]
			enable = true
		} else {
			continue
		}

		pair := strings.Split(option, "=")
		var value string
		if len(pair) > 1 {
			value = pair[1]
		}

		if enable {
			enableOptions[pair[0]] = value
		} else {
			disableOptions[pair[0]] = value
		}
	}

	// 删除重叠参数, 禁用和开启同时声明时, 以开启为准.
	for k := range enableOptions {
		if _, ok := disableOptions[k]; ok {
			delete(disableOptions, k)
		}
	}

	return disableOptions, enableOptions
}

func mergeArgs(options map[string]stream.EnableConfig, disableOptions, enableOptions map[string]string) {
	for k := range disableOptions {
		option, ok := options[k]
		utils.Assert(ok)

		option.SetEnable(false)
	}

	for k, v := range enableOptions {
		var port int

		if len(v) > 0 {
			atoi, err := strconv.Atoi(v)
			if err == nil && atoi > 0 {
				port = atoi
			}
		}

		option, ok := options[k]
		utils.Assert(ok)

		option.SetEnable(true)

		if port > 0 {
			if config, ok := option.(stream.PortConfig); ok {
				config.SetPort(port)
			}
		}
	}
}

func init() {
	stream.RegisterTransStreamFactory(stream.TransStreamRtmp, rtmp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.TransStreamHls, hls.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.TransStreamFlv, flv.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.TransStreamRtsp, rtsp.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.TransStreamRtc, rtc.TransStreamFactory)
	stream.RegisterTransStreamFactory(stream.TransStreamGBStreamForward, gb28181.TransStreamFactory)
	stream.SetRecordStreamFactory(record.NewFLVFileSink)

	config, err := stream.LoadConfigFile("./config.json")
	if err != nil {
		panic(err)
	}

	stream.SetDefaultConfig(config)

	options := map[string]stream.EnableConfig{
		"rtmp":    &config.Rtmp,
		"rtsp":    &config.Rtsp,
		"hls":     &config.Hls,
		"webrtc":  &config.WebRtc,
		"gb28181": &config.GB28181,
		"jt1078":  &config.JT1078,
		"hooks":   &config.Hooks,
		"record":  &config.Record,
	}

	// 读取运行参数
	disableOptions, enableOptions := readRunArgs()
	mergeArgs(options, disableOptions, enableOptions)

	stream.AppConfig = *config

	if stream.AppConfig.Hooks.Enable {
		stream.InitHookUrls()
	}

	if stream.AppConfig.WebRtc.Enable {
		// 设置公网IP和端口
		rtc.InitConfig()
	}

	// 初始化日志
	log.InitLogger(config.Log.FileLogging, zapcore.Level(stream.AppConfig.Log.Level), stream.AppConfig.Log.Name, stream.AppConfig.Log.MaxSize, stream.AppConfig.Log.MaxBackup, stream.AppConfig.Log.MaxAge, stream.AppConfig.Log.Compress)

	if stream.AppConfig.GB28181.Enable && stream.AppConfig.GB28181.IsMultiPort() {
		gb28181.TransportManger = transport.NewTransportManager(uint16(stream.AppConfig.GB28181.Port[0]), uint16(stream.AppConfig.GB28181.Port[1]))
	}

	if stream.AppConfig.Rtsp.Enable && stream.AppConfig.Rtsp.IsMultiPort() {
		rtsp.TransportManger = transport.NewTransportManager(uint16(stream.AppConfig.Rtsp.Port[1]), uint16(stream.AppConfig.Rtsp.Port[2]))
	}

	// 打印配置信息
	indent, _ := json.MarshalIndent(stream.AppConfig, "", "\t")
	log.Sugar.Infof("server config:\r\n%s", indent)
}

func main() {
	if stream.AppConfig.Rtmp.Enable {
		rtmpAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.Rtmp.Port))
		if err != nil {
			panic(err)
		}

		server := rtmp.NewServer()
		err = server.Start(rtmpAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtmp服务成功 addr:", rtmpAddr.String())
	}

	if stream.AppConfig.Rtsp.Enable {
		rtspAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.Rtsp.Port[0]))
		if err != nil {
			panic(rtspAddr)
		}

		server := rtsp.NewServer(stream.AppConfig.Rtsp.Password)
		err = server.Start(rtspAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动rtsp服务成功 addr:", rtspAddr.String())
	}

	log.Sugar.Info("启动http服务 addr:", stream.ListenAddr(stream.AppConfig.Http.Port))
	go startApiServer(net.JoinHostPort(stream.AppConfig.ListenIP, strconv.Itoa(stream.AppConfig.Http.Port)))

	// 单端口模式下, 启动时就创建收流端口
	// 多端口模式下, 创建GBSource时才创建收流端口
	if stream.AppConfig.GB28181.Enable && !stream.AppConfig.GB28181.IsMultiPort() {
		if stream.AppConfig.GB28181.IsEnableUDP() {
			server, err := gb28181.NewUDPServer(gb28181.NewSSRCFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedUDPServer = server
			log.Sugar.Info("启动GB28181 udp收流端口成功:" + stream.ListenAddr(stream.AppConfig.GB28181.Port[0]))
		}

		if stream.AppConfig.GB28181.IsEnableTCP() {
			server, err := gb28181.NewTCPServer(gb28181.NewSSRCFilter(128))
			if err != nil {
				panic(err)
			}

			gb28181.SharedTCPServer = server
			log.Sugar.Info("启动GB28181 tcp收流端口成功:" + stream.ListenAddr(stream.AppConfig.GB28181.Port[0]))
		}
	}

	if stream.AppConfig.JT1078.Enable {
		jtAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.JT1078.Port))
		if err != nil {
			panic(err)
		}

		server := jt1078.NewServer()
		err = server.Start(jtAddr)
		if err != nil {
			panic(err)
		}

		log.Sugar.Info("启动jt1078服务成功 addr:", jtAddr.String())
	}

	if stream.AppConfig.Hooks.IsEnableOnStarted() {
		go func() {
			_, _ = stream.Hook(stream.HookEventStarted, "", nil)
		}()
	}

	// 开启pprof调试
	err := http.ListenAndServe(":19999", nil)
	if err != nil {
		println(err)
	}

	select {}
}
