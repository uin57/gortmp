// Copyright 2013, zhangpeihao All rights reserved.

package rtmp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/zhangpeihao/goamf"
	"net"
	"time"
)

const (
	OUTBOUND_CONN_STATUS_CLOSE            = uint(0)
	OUTBOUND_CONN_STATUS_HANDSHAKE_OK     = uint(1)
	OUTBOUND_CONN_STATUS_CONNECT          = uint(2)
	OUTBOUND_CONN_STATUS_CONNECT_OK       = uint(3)
	OUTBOUND_CONN_STATUS_CREATE_STREAM    = uint(4)
	OUTBOUND_CONN_STATUS_CREATE_STREAM_OK = uint(5)
)

// A handler for outbound connection
type OutboundConnHandler interface {
	ConnHandler
	// When connection status changed
	OnStatus()
	// On stream created
	OnStreamCreated(stream OutboundStream)
}

type OutboundConn interface {
	// Connect an appliction on FMS after handshake.
	Connect(extendedParameters ...interface{}) (err error)
	// Create a stream
	CreateStream() (err error)
	// Close a connection
	Close()
	// URL to connect
	URL() string
	// Connection status
	Status() (uint, error)
	// Send a message
	Send(message *Message) error
	// Calls a command or method on Flash Media Server 
	// or on an application server running Flash Remoting.
	Call(customParameters ...interface{}) (err error)
	// Get network connect instance
	Conn() Conn
}

// High-level interface
//
// A RTMP connection(based on TCP) to RTMP server(FMS or crtmpserver).
// In one connection, we can create many chunk streams.
type outboundConn struct {
	url              string
	rtmpURL          RtmpURL
	status           uint
	err              error
	handler          OutboundConnHandler
	conn             Conn
	transactions     map[uint32]string
	streams          map[uint32]OutboundStream
	maxChannelNumber int
}

// Connect to FMS server, and finish handshake process
func Dial(url string, handler OutboundConnHandler, maxChannelNumber int) (OutboundConn, error) {
	rtmpURL, err := ParseURL(url)
	if err != nil {
		return nil, err
	}
	if rtmpURL.protocol != "rtmp" {
		return nil, errors.New(fmt.Sprintf("Unsupport protocol %s", rtmpURL.protocol))
	}
	c, err := net.Dial("tcp", fmt.Sprintf("%s:%d", rtmpURL.host, rtmpURL.port))
	if err != nil {
		return nil, err
	}

	ipConn, ok := c.(*net.TCPConn)
	if ok {
		ipConn.SetWriteBuffer(128 * 1024)
	}
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	timeout := time.Duration(10) * time.Second
	err = Handshake(c, br, bw, timeout)
	//err = HandshakeSample(c, br, bw, timeout)
	if err == nil {
		fmt.Println("Handshake OK")

		obConn := &outboundConn{
			url:              url,
			rtmpURL:          rtmpURL,
			handler:          handler,
			status:           OUTBOUND_CONN_STATUS_HANDSHAKE_OK,
			transactions:     make(map[uint32]string),
			streams:          make(map[uint32]OutboundStream),
			maxChannelNumber: maxChannelNumber,
		}
		obConn.conn = NewConn(c, br, bw, obConn, obConn.maxChannelNumber)
		return obConn, nil
	}

	return nil, err
}

// Connect to FMS server, and finish handshake process
func NewOutbounConn(c net.Conn, url string, handler OutboundConnHandler, maxChannelNumber int) (OutboundConn, error) {
	rtmpURL, err := ParseURL(url)
	if err != nil {
		return nil, err
	}
	if rtmpURL.protocol != "rtmp" {
		return nil, errors.New(fmt.Sprintf("Unsupport protocol %s", rtmpURL.protocol))
	}
	/*
		ipConn, ok := c.(*net.TCPConn)
		if ok {
			ipConn.SetWriteBuffer(128 * 1024)
		}
	*/
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	obConn := &outboundConn{
		url:              url,
		rtmpURL:          rtmpURL,
		handler:          handler,
		status:           OUTBOUND_CONN_STATUS_HANDSHAKE_OK,
		transactions:     make(map[uint32]string),
		streams:          make(map[uint32]OutboundStream),
		maxChannelNumber: maxChannelNumber,
	}
	obConn.conn = NewConn(c, br, bw, obConn, obConn.maxChannelNumber)
	return obConn, nil
}

// Connect an appliction on FMS after handshake.
func (obConn *outboundConn) Connect(extendedParameters ...interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
			if obConn.err == nil {
				obConn.err = err
			}
		}
	}()
	// Create connect command
	buf := new(bytes.Buffer)
	// Command name
	_, err = amf.WriteString(buf, "connect")
	CheckError(err, "Connect() Write name: connect")
	transactionID := obConn.conn.NewTransactionID()
	obConn.transactions[transactionID] = "connect"
	_, err = amf.WriteDouble(buf, float64(transactionID))
	CheckError(err, "Connect() Write transaction ID")
	_, err = amf.WriteObjectMarker(buf)
	CheckError(err, "Connect() Write object marker")

	_, err = amf.WriteObjectName(buf, "app")
	CheckError(err, "Connect() Write app name")
	_, err = amf.WriteString(buf, obConn.rtmpURL.App())
	CheckError(err, "Connect() Write app value")

	_, err = amf.WriteObjectName(buf, "flashver")
	CheckError(err, "Connect() Write flashver name")
	_, err = amf.WriteString(buf, FLASH_PLAYER_VERSION_STRING)
	CheckError(err, "Connect() Write flashver value")

	//	_, err = amf.WriteObjectName(buf, "swfUrl")
	//	CheckError(err, "Connect() Write swfUrl name")
	//	_, err = amf.WriteString(buf, SWF_URL_STRING)
	//	CheckError(err, "Connect() Write swfUrl value")

	_, err = amf.WriteObjectName(buf, "tcUrl")
	CheckError(err, "Connect() Write tcUrl name")
	_, err = amf.WriteString(buf, obConn.url)
	CheckError(err, "Connect() Write tcUrl value")

	_, err = amf.WriteObjectName(buf, "fpad")
	CheckError(err, "Connect() Write fpad name")
	_, err = amf.WriteBoolean(buf, false)
	CheckError(err, "Connect() Write fpad value")

	_, err = amf.WriteObjectName(buf, "capabilities")
	CheckError(err, "Connect() Write capabilities name")
	_, err = amf.WriteDouble(buf, DEFAULT_CAPABILITIES)
	CheckError(err, "Connect() Write capabilities value")

	_, err = amf.WriteObjectName(buf, "audioCodecs")
	CheckError(err, "Connect() Write audioCodecs name")
	_, err = amf.WriteDouble(buf, DEFAULT_AUDIO_CODECS)
	CheckError(err, "Connect() Write audioCodecs value")

	_, err = amf.WriteObjectName(buf, "vedioCodecs")
	CheckError(err, "Connect() Write vedioCodecs name")
	_, err = amf.WriteDouble(buf, DEFAULT_VIDEO_CODECS)
	CheckError(err, "Connect() Write vedioCodecs value")

	_, err = amf.WriteObjectName(buf, "vedioFunction")
	CheckError(err, "Connect() Write vedioFunction name")
	_, err = amf.WriteDouble(buf, float64(1))
	CheckError(err, "Connect() Write vedioFunction value")

	//	_, err = amf.WriteObjectName(buf, "pageUrl")
	//	CheckError(err, "Connect() Write pageUrl name")
	//	_, err = amf.WriteString(buf, PAGE_URL_STRING)
	//	CheckError(err, "Connect() Write pageUrl value")

	//_, err = amf.WriteObjectName(buf, "objectEncoding")
	//CheckError(err, "Connect() Write objectEncoding name")
	//_, err = amf.WriteDouble(buf, float64(amf.AMF0))
	//CheckError(err, "Connect() Write objectEncoding value")

	_, err = amf.WriteObjectEndMarker(buf)
	CheckError(err, "Connect() Write ObjectEndMarker")

	// extended parameters
	for _, param := range extendedParameters {
		_, err = amf.WriteValue(buf, param)
		CheckError(err, "Connect() Write extended parameters")
	}

	connectMessage := Message{
		ChunkStreamID: CS_ID_COMMAND,
		Type:          COMMAND_AMF0,
		Size:          uint32(buf.Len()),
		Buf:           buf,
	}
	connectMessage.Dump("connect")
	obConn.status = OUTBOUND_CONN_STATUS_CONNECT
	return obConn.conn.Send(&connectMessage)
}

// Close a connection
func (obConn *outboundConn) Close() {
	for _, stream := range obConn.streams {
		stream.Close()
	}
	time.Sleep(time.Second)
	obConn.status = OUTBOUND_CONN_STATUS_CLOSE
	obConn.conn.Close()
}

// URL to connect
func (obConn *outboundConn) URL() string {
	return obConn.url
}

// Connection status
func (obConn *outboundConn) Status() (uint, error) {
	return obConn.status, obConn.err
}

// Callback when recieved message. Audio & Video data
func (obConn *outboundConn) Received(message *Message) {
	stream, found := obConn.streams[message.StreamID]
	if found {
		if !stream.Received(message) {
			obConn.handler.Received(message)
		}
	} else {
		obConn.handler.Received(message)
	}
}

// Callback when recieved message.
func (obConn *outboundConn) ReceivedCommand(command *Command) {
	command.Dump()
	switch command.Name {
	case "_result":
		transaction, found := obConn.transactions[command.TransactionID]
		if found {
			switch transaction {
			case "connect":
				if command.Objects != nil && len(command.Objects) >= 2 {
					information, ok := command.Objects[1].(amf.Object)
					if ok {
						code, ok := information["code"]
						if ok && code == RESULT_CONNECT_OK {
							// Connect OK
							time.Sleep(time.Duration(200) * time.Millisecond)
							obConn.conn.SetWindowAcknowledgementSize()
							obConn.status = OUTBOUND_CONN_STATUS_CONNECT_OK
							obConn.handler.OnStatus()
							obConn.status = OUTBOUND_CONN_STATUS_CREATE_STREAM
							obConn.CreateStream()
						}
					}
				}
			case "createStream":
				if command.Objects != nil && len(command.Objects) >= 2 {
					streamID, ok := command.Objects[1].(float64)
					if ok {
						newChunkStream, err := obConn.conn.CreateMediaChunkStream()
						if err != nil {
							fmt.Println("outboundConn::ReceivedCommand() CreateMediaChunkStream err:", err)
							return
						}
						stream := &outboundStream{
							id:            uint32(streamID),
							conn:          obConn,
							chunkStreamID: newChunkStream.ID,
						}
						obConn.streams[stream.ID()] = stream
						obConn.status = OUTBOUND_CONN_STATUS_CREATE_STREAM_OK
						obConn.handler.OnStatus()
						obConn.handler.OnStreamCreated(stream)
					}
				}
			}
			delete(obConn.transactions, command.TransactionID)
		}
	case "_error":
		transaction, found := obConn.transactions[command.TransactionID]
		if found {
			fmt.Printf("Command(%d) %s error\n", command.TransactionID, transaction)
		} else {
			fmt.Printf("Command(%d) not been found\n", command.TransactionID)
		}
	case "onBWCheck":
	}
}

// Connection closed
func (obConn *outboundConn) Closed() {
	obConn.status = OUTBOUND_CONN_STATUS_CLOSE
	obConn.handler.OnStatus()
}

// Create a stream
func (obConn *outboundConn) CreateStream() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
			if obConn.err == nil {
				obConn.err = err
			}
		}
	}()
	// Create createStream command
	transactionID := obConn.conn.NewTransactionID()
	cmd := &Command{
		IsFlex:        false,
		Name:          "createStream",
		TransactionID: transactionID,
		Objects:       make([]interface{}, 1),
	}
	cmd.Objects[0] = nil
	buf := new(bytes.Buffer)
	err = cmd.Write(buf)
	CheckError(err, "createStream() Create command")
	obConn.transactions[transactionID] = "createStream"

	message := Message{
		ChunkStreamID: CS_ID_COMMAND,
		Type:          COMMAND_AMF0,
		Size:          uint32(buf.Len()),
		Buf:           buf,
	}
	message.Dump("createStream")
	return obConn.conn.Send(&message)
}

// Send a message
func (obConn *outboundConn) Send(message *Message) error {
	return obConn.conn.Send(message)
}

// Calls a command or method on Flash Media Server 
// or on an application server running Flash Remoting.
func (obConn *outboundConn) Call(customParameters ...interface{}) (err error) {
	return errors.New("Unimplemented")
}

// Get network connect instance
func (obConn *outboundConn) Conn() Conn {
	return obConn.conn
}
