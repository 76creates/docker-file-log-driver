package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/containerd/fifo"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/loggerutils"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

const (
	baseLogDir string = "/data/output"
)

type driver struct {
	mu   sync.Mutex
	logs map[string]*dockerInput
}

type dockerInput struct {
	stream io.ReadCloser
	info   logger.Info
}

func newDriver() *driver {
	return &driver{
		logs: make(map[string]*dockerInput),
	}
}

func (d *driver) StartLogging(file string, logCtx logger.Info) error {
	var capval int64 = -1
	var maxFiles = 1
	var compress = false

	d.mu.Lock()
	if _, exists := d.logs[file]; exists {
		d.mu.Unlock()
		return fmt.Errorf("logger for %q already exists", file)
	}
	d.mu.Unlock()

	buf := bytes.NewBuffer(nil)
	marshalFunc := func(msg *logger.Message) ([]byte, error) {
		_, err := buf.Write(msg.Line)
		if err != nil {
			return []byte{}, err
		}
		err = buf.WriteByte('\n')
		if err != nil {
			return []byte{}, err
		}

		b := buf.Bytes()
		buf.Reset()
		return b, nil
	}

	logDir := baseLogDir + "/" + logCtx.ContainerID + "/"
	os.MkdirAll(logDir, 0660)

	stdErrFile := logDir + "stderr"
	stdErr, err := loggerutils.NewLogFile(stdErrFile, capval, maxFiles, compress, marshalFunc, decodeFunc, 0640, getTailReader)
	if err != nil {
		return err
	}
	stdOutFile := logDir + "stdout"
	stdOut, err := loggerutils.NewLogFile(stdOutFile, capval, maxFiles, compress, marshalFunc, decodeFunc, 0640, getTailReader)
	if err != nil {
		return err
	}

	f, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

	d.mu.Lock()
	lf := &dockerInput{f, logCtx}
	d.logs[file] = lf
	d.mu.Unlock()

	go consumeLog(lf, stdOut, stdErr)
	return nil
}

func (d *driver) StopLogging(file string) error {
	d.mu.Lock()
	_, ok := d.logs[file]
	if ok {
		delete(d.logs, file)
	}
	d.mu.Unlock()
	return nil
}

func consumeLog(lf *dockerInput, stdOutWriter, stdErrWriter *loggerutils.LogFile) {
	dec := protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, maxMsgLen)
	defer dec.Close()
	var buf logdriver.LogEntry
	for {
		if err := dec.ReadMsg(&buf); err != nil {
			if err == io.EOF {
				lf.stream.Close()
				return
			}
			dec = protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
			continue
		}
		var msg logger.Message
		msg.Line = buf.Line
		msg.Source = buf.Source
		if buf.PartialLogMetadata != nil {
			msg.PLogMetaData.ID = buf.PartialLogMetadata.Id
			msg.PLogMetaData.Last = buf.PartialLogMetadata.Last
			msg.PLogMetaData.Ordinal = int(buf.PartialLogMetadata.Ordinal)
		}
		msg.Timestamp = time.Unix(0, buf.TimeNano)

		if msg.Source == "stdout" {
			err := stdOutWriter.WriteLogEntry(&msg)
			if err != nil {
				panic(err)
			}
		} else if msg.Source == "stderr" {
			err := stdErrWriter.WriteLogEntry(&msg)
			if err != nil {
				panic(err)
			}
		}

		buf.Reset()
	}
}

func (d *driver) ReadLogs(info logger.Info, config logger.ReadConfig) (io.ReadCloser, error) {
	return nil, nil
}
