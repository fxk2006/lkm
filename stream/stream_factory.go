package stream

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
)

type TransStreamFactory func(source Source, protocol TransStreamProtocol, streams []utils.AVStream) (TransStream, error)

type RecordStreamFactory func(source string) (Sink, string, error)

var (
	transStreamFactories map[TransStreamProtocol]TransStreamFactory
	recordStreamFactory  RecordStreamFactory
)

func init() {
	transStreamFactories = make(map[TransStreamProtocol]TransStreamFactory, 8)
}

func RegisterTransStreamFactory(protocol TransStreamProtocol, streamFunc TransStreamFactory) {
	_, ok := transStreamFactories[protocol]
	if ok {
		panic(fmt.Sprintf("%s has been registered", protocol.ToString()))
	}

	transStreamFactories[protocol] = streamFunc
}

func FindTransStreamFactory(protocol TransStreamProtocol) (TransStreamFactory, error) {
	f, ok := transStreamFactories[protocol]
	if !ok {
		return nil, fmt.Errorf("unknown protocol %s", protocol.ToString())
	}

	return f, nil
}

func CreateTransStream(source Source, protocol TransStreamProtocol, streams []utils.AVStream) (TransStream, error) {
	factory, err := FindTransStreamFactory(protocol)
	if err != nil {
		return nil, err
	}

	return factory(source, protocol, streams)
}

func SetRecordStreamFactory(factory RecordStreamFactory) {
	recordStreamFactory = factory
}

func CreateRecordStream(sourceId string) (Sink, string, error) {
	return recordStreamFactory(sourceId)
}
