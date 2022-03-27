package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/loggerutils"
	"github.com/docker/docker/pkg/tailfile"
	"github.com/pkg/errors"
)

const (
	// maxMsgLen is the maximum size of the logger.Message after serialization.
	// logger.defaultBufSize caps the size of Line field.
	maxMsgLen int = 1e6 // 1MB.

	encodeBinaryLen = 4
	initialBufSize  = 2048
	maxDecodeRetry  = 20000

	defaultMaxFileSize  int64 = 20 * 1024 * 1024
	defaultMaxFileCount       = 5
	defaultCompressLogs       = true
)

type decoder struct {
	rdr   io.Reader
	proto *logdriver.LogEntry
	// buf keeps bytes from rdr.
	buf []byte
	// offset is the position in buf.
	// If offset > 0, buf[offset:] has bytes which are read but haven't used.
	offset int
	// nextMsgLen is the length of the next log message.
	// If nextMsgLen = 0, a new value must be read from rdr.
	nextMsgLen int
}

func (d *decoder) Decode() (*logger.Message, error) {
	if d.proto == nil {
		d.proto = &logdriver.LogEntry{}
	} else {
		resetProto(d.proto)
	}
	if d.buf == nil {
		d.buf = make([]byte, initialBufSize)
	}

	if d.nextMsgLen == 0 {
		msgLen, err := d.decodeSizeHeader()
		if err != nil {
			return nil, err
		}

		if msgLen > maxMsgLen {
			return nil, fmt.Errorf("log message is too large (%d > %d)", msgLen, maxMsgLen)
		}

		if len(d.buf) < msgLen+encodeBinaryLen {
			d.buf = make([]byte, msgLen+encodeBinaryLen)
		} else if msgLen <= initialBufSize {
			d.buf = d.buf[:initialBufSize]
		} else {
			d.buf = d.buf[:msgLen+encodeBinaryLen]
		}

		d.nextMsgLen = msgLen
	}
	return d.decodeLogEntry()
}

func (d *decoder) Reset(rdr io.Reader) {
	if d.rdr == rdr {
		return
	}

	d.rdr = rdr
	if d.proto != nil {
		resetProto(d.proto)
	}
	if d.buf != nil {
		d.buf = d.buf[:initialBufSize]
	}
	d.offset = 0
	d.nextMsgLen = 0
}

func (d *decoder) Close() {
	d.buf = d.buf[:0]
	d.buf = nil
	if d.proto != nil {
		resetProto(d.proto)
	}
	d.rdr = nil
}

// resetProto resets all important fields of the logdriver.LogEntry
func resetProto(proto *logdriver.LogEntry) {
	proto.Source = ""
	proto.Line = proto.Line[:0]
	proto.TimeNano = 0
	proto.Partial = false
	if proto.PartialLogMetadata != nil {
		proto.PartialLogMetadata.Id = ""
		proto.PartialLogMetadata.Last = false
		proto.PartialLogMetadata.Ordinal = 0
	}
	proto.PartialLogMetadata = nil
}

// protoToMessage decode entry into bytes and metadata
func protoToMessage(proto *logdriver.LogEntry) *logger.Message {
	msg := &logger.Message{
		Source:    proto.Source,
		Timestamp: time.Unix(0, proto.TimeNano),
	}
	if proto.Partial {
		var md backend.PartialLogMetaData
		md.Last = proto.GetPartialLogMetadata().GetLast()
		md.ID = proto.GetPartialLogMetadata().GetId()
		md.Ordinal = int(proto.GetPartialLogMetadata().GetOrdinal())
		msg.PLogMetaData = &md
	}
	msg.Line = append(msg.Line[:0], proto.Line...)
	return msg
}

// readRecord reads the message into the buffer
func (d *decoder) readRecord(size int) error {
	var err error
	for i := 0; i < maxDecodeRetry; i++ {
		var n int
		n, err = io.ReadFull(d.rdr, d.buf[d.offset:size])
		d.offset += n
		if err != nil {
			if err != io.ErrUnexpectedEOF {
				return err
			}
			continue
		}
		break
	}
	if err != nil {
		return err
	}
	d.offset = 0
	return nil
}

// decodeSizeHeader returns the message size
func (d *decoder) decodeSizeHeader() (int, error) {
	err := d.readRecord(encodeBinaryLen)
	if err != nil {
		return 0, errors.Wrap(err, "could not read a size header")
	}

	msgLen := int(binary.BigEndian.Uint32(d.buf[:encodeBinaryLen]))
	return msgLen, nil
}

// decodeLogEntry convert log entry to message
func (d *decoder) decodeLogEntry() (*logger.Message, error) {
	msgLen := d.nextMsgLen
	err := d.readRecord(msgLen + encodeBinaryLen)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read a log entry (size=%d+%d)", msgLen, encodeBinaryLen)
	}
	d.nextMsgLen = 0

	if err := d.proto.Unmarshal(d.buf[:msgLen]); err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling log entry (size=%d)", msgLen)
	}

	msg := protoToMessage(d.proto)
	if msg.PLogMetaData == nil {
		msg.Line = append(msg.Line, '\n')
	}

	return msg, nil
}

func decodeFunc(rdr io.Reader) loggerutils.Decoder {
	return &decoder{rdr: rdr}
}

func getTailReader(ctx context.Context, r loggerutils.SizeReaderAt, req int) (io.Reader, int, error) {
	return tailfile.NewTailReader(ctx, r, req)
}
