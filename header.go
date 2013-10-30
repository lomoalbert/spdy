// Copyright 2013, Amahi.  All rights reserved.
// Use of this source code is governed by the
// license that can be found in the LICENSE file.

// This file contains header-handling functions
// The header dictionary came from the SPDY standard.
// Some of the functions came from Jamie Hall's
// SPDY go library.

package spdy

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"sync"
)

type hrSource struct {
	r io.Reader
	m sync.RWMutex
	c *sync.Cond
}

func (src *hrSource) Read(p []byte) (n int, err error) {
	src.m.RLock()
	for src.r == nil {
		src.c.Wait()
	}
	n, err = src.r.Read(p)
	src.m.RUnlock()
	if err == io.EOF {
		src.change(nil)
		err = nil
	}
	return
}

func (src *hrSource) change(r io.Reader) {
	src.m.Lock()
	defer src.m.Unlock()
	src.r = r
	src.c.Broadcast()
}

// A headerReader reads zlib-compressed headers from discontiguous sources.
type headerReader struct {
	source       hrSource
	decompressor io.ReadCloser
}

// newHeaderReader creates a headerReader with the initial dictionary.
func newHeaderReader() (hr *headerReader) {
	hr = new(headerReader)
	hr.source.c = sync.NewCond(hr.source.m.RLocker())
	return
}

// ReadHeader reads a set of headers from a reader.
func (hr *headerReader) readHeader(r io.Reader) (h http.Header, err error) {
	hr.source.change(r)
	h, err = hr.read()
	return
}

// Decode reads a set of headers from a block of bytes.
func (hr *headerReader) decode(data []byte) (h http.Header, err error) {
	hr.source.change(bytes.NewBuffer(data))
	h, err = hr.read()
	return
}

func (hr *headerReader) read() (h http.Header, err error) {
	var count uint32
	if hr.decompressor == nil {
		hr.decompressor, err = zlib.NewReaderDict(&hr.source, headerDictionary)
		if err != nil {
			return
		}
	}
	err = binary.Read(hr.decompressor, binary.BigEndian, &count)
	if err != nil {
		return
	}
	h = make(http.Header, int(count))
	for i := 0; i < int(count); i++ {
		var name, value string
		name, err = readHeaderString(hr.decompressor)
		if err != nil {
			return
		}
		value, err = readHeaderString(hr.decompressor)
		if err != nil {
			return
		}
		valueList := strings.Split(string(value), "\x00")
		for _, v := range valueList {
			h.Add(name, v)
		}
	}
	return
}

func readHeaderString(r io.Reader) (s string, err error) {
	var length uint32
	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return
	}
	data := make([]byte, int(length))
	_, err = io.ReadFull(r, data)
	if err != nil {
		return
	}
	return string(data), nil
}

// write zlib-compressed headers on different streams
type headerWriter struct {
	compressor *zlib.Writer
	buffer     *bytes.Buffer
}

// creates a headerWriter ready to compress headers
func newHeaderWriter() (hw *headerWriter) {
	hw = &headerWriter{buffer: new(bytes.Buffer)}
	hw.compressor, _ = zlib.NewWriterLevelDict(hw.buffer, zlib.BestCompression, headerDictionary)
	return
}

// write a header block directly to a writer
func (hw *headerWriter) writeHeader(w io.Writer, h http.Header) (err error) {
	hw.write(h)
	_, err = io.Copy(w, hw.buffer)
	hw.buffer.Reset()
	return
}

// Encode returns a compressed header block.
func (hw *headerWriter) encode(h http.Header) (data []byte) {
	hw.write(h)
	data = make([]byte, hw.buffer.Len())
	hw.buffer.Read(data)
	return
}

func (hw *headerWriter) write(h http.Header) {
	binary.Write(hw.compressor, binary.BigEndian, uint32(len(h)))
	for k, vals := range h {
		k = strings.ToLower(k)
		binary.Write(hw.compressor, binary.BigEndian, uint32(len(k)))
		binary.Write(hw.compressor, binary.BigEndian, []byte(k))
		v := strings.Join(vals, "\x00")
		binary.Write(hw.compressor, binary.BigEndian, uint32(len(v)))
		binary.Write(hw.compressor, binary.BigEndian, []byte(v))
	}
	hw.compressor.Flush()
}

// compression header for SPDY/3
var headerDictionary = []byte{
	0x00, 0x00, 0x00, 0x07, 0x6f, 0x70, 0x74, 0x69,
	0x6f, 0x6e, 0x73, 0x00, 0x00, 0x00, 0x04, 0x68,
	0x65, 0x61, 0x64, 0x00, 0x00, 0x00, 0x04, 0x70,
	0x6f, 0x73, 0x74, 0x00, 0x00, 0x00, 0x03, 0x70,
	0x75, 0x74, 0x00, 0x00, 0x00, 0x06, 0x64, 0x65,
	0x6c, 0x65, 0x74, 0x65, 0x00, 0x00, 0x00, 0x05,
	0x74, 0x72, 0x61, 0x63, 0x65, 0x00, 0x00, 0x00,
	0x06, 0x61, 0x63, 0x63, 0x65, 0x70, 0x74, 0x00,
	0x00, 0x00, 0x0e, 0x61, 0x63, 0x63, 0x65, 0x70,
	0x74, 0x2d, 0x63, 0x68, 0x61, 0x72, 0x73, 0x65,
	0x74, 0x00, 0x00, 0x00, 0x0f, 0x61, 0x63, 0x63,
	0x65, 0x70, 0x74, 0x2d, 0x65, 0x6e, 0x63, 0x6f,
	0x64, 0x69, 0x6e, 0x67, 0x00, 0x00, 0x00, 0x0f,
	0x61, 0x63, 0x63, 0x65, 0x70, 0x74, 0x2d, 0x6c,
	0x61, 0x6e, 0x67, 0x75, 0x61, 0x67, 0x65, 0x00,
	0x00, 0x00, 0x0d, 0x61, 0x63, 0x63, 0x65, 0x70,
	0x74, 0x2d, 0x72, 0x61, 0x6e, 0x67, 0x65, 0x73,
	0x00, 0x00, 0x00, 0x03, 0x61, 0x67, 0x65, 0x00,
	0x00, 0x00, 0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x77,
	0x00, 0x00, 0x00, 0x0d, 0x61, 0x75, 0x74, 0x68,
	0x6f, 0x72, 0x69, 0x7a, 0x61, 0x74, 0x69, 0x6f,
	0x6e, 0x00, 0x00, 0x00, 0x0d, 0x63, 0x61, 0x63,
	0x68, 0x65, 0x2d, 0x63, 0x6f, 0x6e, 0x74, 0x72,
	0x6f, 0x6c, 0x00, 0x00, 0x00, 0x0a, 0x63, 0x6f,
	0x6e, 0x6e, 0x65, 0x63, 0x74, 0x69, 0x6f, 0x6e,
	0x00, 0x00, 0x00, 0x0c, 0x63, 0x6f, 0x6e, 0x74,
	0x65, 0x6e, 0x74, 0x2d, 0x62, 0x61, 0x73, 0x65,
	0x00, 0x00, 0x00, 0x10, 0x63, 0x6f, 0x6e, 0x74,
	0x65, 0x6e, 0x74, 0x2d, 0x65, 0x6e, 0x63, 0x6f,
	0x64, 0x69, 0x6e, 0x67, 0x00, 0x00, 0x00, 0x10,
	0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74, 0x2d,
	0x6c, 0x61, 0x6e, 0x67, 0x75, 0x61, 0x67, 0x65,
	0x00, 0x00, 0x00, 0x0e, 0x63, 0x6f, 0x6e, 0x74,
	0x65, 0x6e, 0x74, 0x2d, 0x6c, 0x65, 0x6e, 0x67,
	0x74, 0x68, 0x00, 0x00, 0x00, 0x10, 0x63, 0x6f,
	0x6e, 0x74, 0x65, 0x6e, 0x74, 0x2d, 0x6c, 0x6f,
	0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x00, 0x00,
	0x00, 0x0b, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e,
	0x74, 0x2d, 0x6d, 0x64, 0x35, 0x00, 0x00, 0x00,
	0x0d, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74,
	0x2d, 0x72, 0x61, 0x6e, 0x67, 0x65, 0x00, 0x00,
	0x00, 0x0c, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e,
	0x74, 0x2d, 0x74, 0x79, 0x70, 0x65, 0x00, 0x00,
	0x00, 0x04, 0x64, 0x61, 0x74, 0x65, 0x00, 0x00,
	0x00, 0x04, 0x65, 0x74, 0x61, 0x67, 0x00, 0x00,
	0x00, 0x06, 0x65, 0x78, 0x70, 0x65, 0x63, 0x74,
	0x00, 0x00, 0x00, 0x07, 0x65, 0x78, 0x70, 0x69,
	0x72, 0x65, 0x73, 0x00, 0x00, 0x00, 0x04, 0x66,
	0x72, 0x6f, 0x6d, 0x00, 0x00, 0x00, 0x04, 0x68,
	0x6f, 0x73, 0x74, 0x00, 0x00, 0x00, 0x08, 0x69,
	0x66, 0x2d, 0x6d, 0x61, 0x74, 0x63, 0x68, 0x00,
	0x00, 0x00, 0x11, 0x69, 0x66, 0x2d, 0x6d, 0x6f,
	0x64, 0x69, 0x66, 0x69, 0x65, 0x64, 0x2d, 0x73,
	0x69, 0x6e, 0x63, 0x65, 0x00, 0x00, 0x00, 0x0d,
	0x69, 0x66, 0x2d, 0x6e, 0x6f, 0x6e, 0x65, 0x2d,
	0x6d, 0x61, 0x74, 0x63, 0x68, 0x00, 0x00, 0x00,
	0x08, 0x69, 0x66, 0x2d, 0x72, 0x61, 0x6e, 0x67,
	0x65, 0x00, 0x00, 0x00, 0x13, 0x69, 0x66, 0x2d,
	0x75, 0x6e, 0x6d, 0x6f, 0x64, 0x69, 0x66, 0x69,
	0x65, 0x64, 0x2d, 0x73, 0x69, 0x6e, 0x63, 0x65,
	0x00, 0x00, 0x00, 0x0d, 0x6c, 0x61, 0x73, 0x74,
	0x2d, 0x6d, 0x6f, 0x64, 0x69, 0x66, 0x69, 0x65,
	0x64, 0x00, 0x00, 0x00, 0x08, 0x6c, 0x6f, 0x63,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x00, 0x00, 0x00,
	0x0c, 0x6d, 0x61, 0x78, 0x2d, 0x66, 0x6f, 0x72,
	0x77, 0x61, 0x72, 0x64, 0x73, 0x00, 0x00, 0x00,
	0x06, 0x70, 0x72, 0x61, 0x67, 0x6d, 0x61, 0x00,
	0x00, 0x00, 0x12, 0x70, 0x72, 0x6f, 0x78, 0x79,
	0x2d, 0x61, 0x75, 0x74, 0x68, 0x65, 0x6e, 0x74,
	0x69, 0x63, 0x61, 0x74, 0x65, 0x00, 0x00, 0x00,
	0x13, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x2d, 0x61,
	0x75, 0x74, 0x68, 0x6f, 0x72, 0x69, 0x7a, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x00, 0x00, 0x00, 0x05,
	0x72, 0x61, 0x6e, 0x67, 0x65, 0x00, 0x00, 0x00,
	0x07, 0x72, 0x65, 0x66, 0x65, 0x72, 0x65, 0x72,
	0x00, 0x00, 0x00, 0x0b, 0x72, 0x65, 0x74, 0x72,
	0x79, 0x2d, 0x61, 0x66, 0x74, 0x65, 0x72, 0x00,
	0x00, 0x00, 0x06, 0x73, 0x65, 0x72, 0x76, 0x65,
	0x72, 0x00, 0x00, 0x00, 0x02, 0x74, 0x65, 0x00,
	0x00, 0x00, 0x07, 0x74, 0x72, 0x61, 0x69, 0x6c,
	0x65, 0x72, 0x00, 0x00, 0x00, 0x11, 0x74, 0x72,
	0x61, 0x6e, 0x73, 0x66, 0x65, 0x72, 0x2d, 0x65,
	0x6e, 0x63, 0x6f, 0x64, 0x69, 0x6e, 0x67, 0x00,
	0x00, 0x00, 0x07, 0x75, 0x70, 0x67, 0x72, 0x61,
	0x64, 0x65, 0x00, 0x00, 0x00, 0x0a, 0x75, 0x73,
	0x65, 0x72, 0x2d, 0x61, 0x67, 0x65, 0x6e, 0x74,
	0x00, 0x00, 0x00, 0x04, 0x76, 0x61, 0x72, 0x79,
	0x00, 0x00, 0x00, 0x03, 0x76, 0x69, 0x61, 0x00,
	0x00, 0x00, 0x07, 0x77, 0x61, 0x72, 0x6e, 0x69,
	0x6e, 0x67, 0x00, 0x00, 0x00, 0x10, 0x77, 0x77,
	0x77, 0x2d, 0x61, 0x75, 0x74, 0x68, 0x65, 0x6e,
	0x74, 0x69, 0x63, 0x61, 0x74, 0x65, 0x00, 0x00,
	0x00, 0x06, 0x6d, 0x65, 0x74, 0x68, 0x6f, 0x64,
	0x00, 0x00, 0x00, 0x03, 0x67, 0x65, 0x74, 0x00,
	0x00, 0x00, 0x06, 0x73, 0x74, 0x61, 0x74, 0x75,
	0x73, 0x00, 0x00, 0x00, 0x06, 0x32, 0x30, 0x30,
	0x20, 0x4f, 0x4b, 0x00, 0x00, 0x00, 0x07, 0x76,
	0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x00, 0x00,
	0x00, 0x08, 0x48, 0x54, 0x54, 0x50, 0x2f, 0x31,
	0x2e, 0x31, 0x00, 0x00, 0x00, 0x03, 0x75, 0x72,
	0x6c, 0x00, 0x00, 0x00, 0x06, 0x70, 0x75, 0x62,
	0x6c, 0x69, 0x63, 0x00, 0x00, 0x00, 0x0a, 0x73,
	0x65, 0x74, 0x2d, 0x63, 0x6f, 0x6f, 0x6b, 0x69,
	0x65, 0x00, 0x00, 0x00, 0x0a, 0x6b, 0x65, 0x65,
	0x70, 0x2d, 0x61, 0x6c, 0x69, 0x76, 0x65, 0x00,
	0x00, 0x00, 0x06, 0x6f, 0x72, 0x69, 0x67, 0x69,
	0x6e, 0x31, 0x30, 0x30, 0x31, 0x30, 0x31, 0x32,
	0x30, 0x31, 0x32, 0x30, 0x32, 0x32, 0x30, 0x35,
	0x32, 0x30, 0x36, 0x33, 0x30, 0x30, 0x33, 0x30,
	0x32, 0x33, 0x30, 0x33, 0x33, 0x30, 0x34, 0x33,
	0x30, 0x35, 0x33, 0x30, 0x36, 0x33, 0x30, 0x37,
	0x34, 0x30, 0x32, 0x34, 0x30, 0x35, 0x34, 0x30,
	0x36, 0x34, 0x30, 0x37, 0x34, 0x30, 0x38, 0x34,
	0x30, 0x39, 0x34, 0x31, 0x30, 0x34, 0x31, 0x31,
	0x34, 0x31, 0x32, 0x34, 0x31, 0x33, 0x34, 0x31,
	0x34, 0x34, 0x31, 0x35, 0x34, 0x31, 0x36, 0x34,
	0x31, 0x37, 0x35, 0x30, 0x32, 0x35, 0x30, 0x34,
	0x35, 0x30, 0x35, 0x32, 0x30, 0x33, 0x20, 0x4e,
	0x6f, 0x6e, 0x2d, 0x41, 0x75, 0x74, 0x68, 0x6f,
	0x72, 0x69, 0x74, 0x61, 0x74, 0x69, 0x76, 0x65,
	0x20, 0x49, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x32, 0x30, 0x34, 0x20,
	0x4e, 0x6f, 0x20, 0x43, 0x6f, 0x6e, 0x74, 0x65,
	0x6e, 0x74, 0x33, 0x30, 0x31, 0x20, 0x4d, 0x6f,
	0x76, 0x65, 0x64, 0x20, 0x50, 0x65, 0x72, 0x6d,
	0x61, 0x6e, 0x65, 0x6e, 0x74, 0x6c, 0x79, 0x34,
	0x30, 0x30, 0x20, 0x42, 0x61, 0x64, 0x20, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x34, 0x30,
	0x31, 0x20, 0x55, 0x6e, 0x61, 0x75, 0x74, 0x68,
	0x6f, 0x72, 0x69, 0x7a, 0x65, 0x64, 0x34, 0x30,
	0x33, 0x20, 0x46, 0x6f, 0x72, 0x62, 0x69, 0x64,
	0x64, 0x65, 0x6e, 0x34, 0x30, 0x34, 0x20, 0x4e,
	0x6f, 0x74, 0x20, 0x46, 0x6f, 0x75, 0x6e, 0x64,
	0x35, 0x30, 0x30, 0x20, 0x49, 0x6e, 0x74, 0x65,
	0x72, 0x6e, 0x61, 0x6c, 0x20, 0x53, 0x65, 0x72,
	0x76, 0x65, 0x72, 0x20, 0x45, 0x72, 0x72, 0x6f,
	0x72, 0x35, 0x30, 0x31, 0x20, 0x4e, 0x6f, 0x74,
	0x20, 0x49, 0x6d, 0x70, 0x6c, 0x65, 0x6d, 0x65,
	0x6e, 0x74, 0x65, 0x64, 0x35, 0x30, 0x33, 0x20,
	0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x20,
	0x55, 0x6e, 0x61, 0x76, 0x61, 0x69, 0x6c, 0x61,
	0x62, 0x6c, 0x65, 0x4a, 0x61, 0x6e, 0x20, 0x46,
	0x65, 0x62, 0x20, 0x4d, 0x61, 0x72, 0x20, 0x41,
	0x70, 0x72, 0x20, 0x4d, 0x61, 0x79, 0x20, 0x4a,
	0x75, 0x6e, 0x20, 0x4a, 0x75, 0x6c, 0x20, 0x41,
	0x75, 0x67, 0x20, 0x53, 0x65, 0x70, 0x74, 0x20,
	0x4f, 0x63, 0x74, 0x20, 0x4e, 0x6f, 0x76, 0x20,
	0x44, 0x65, 0x63, 0x20, 0x30, 0x30, 0x3a, 0x30,
	0x30, 0x3a, 0x30, 0x30, 0x20, 0x4d, 0x6f, 0x6e,
	0x2c, 0x20, 0x54, 0x75, 0x65, 0x2c, 0x20, 0x57,
	0x65, 0x64, 0x2c, 0x20, 0x54, 0x68, 0x75, 0x2c,
	0x20, 0x46, 0x72, 0x69, 0x2c, 0x20, 0x53, 0x61,
	0x74, 0x2c, 0x20, 0x53, 0x75, 0x6e, 0x2c, 0x20,
	0x47, 0x4d, 0x54, 0x63, 0x68, 0x75, 0x6e, 0x6b,
	0x65, 0x64, 0x2c, 0x74, 0x65, 0x78, 0x74, 0x2f,
	0x68, 0x74, 0x6d, 0x6c, 0x2c, 0x69, 0x6d, 0x61,
	0x67, 0x65, 0x2f, 0x70, 0x6e, 0x67, 0x2c, 0x69,
	0x6d, 0x61, 0x67, 0x65, 0x2f, 0x6a, 0x70, 0x67,
	0x2c, 0x69, 0x6d, 0x61, 0x67, 0x65, 0x2f, 0x67,
	0x69, 0x66, 0x2c, 0x61, 0x70, 0x70, 0x6c, 0x69,
	0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2f, 0x78,
	0x6d, 0x6c, 0x2c, 0x61, 0x70, 0x70, 0x6c, 0x69,
	0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2f, 0x78,
	0x68, 0x74, 0x6d, 0x6c, 0x2b, 0x78, 0x6d, 0x6c,
	0x2c, 0x74, 0x65, 0x78, 0x74, 0x2f, 0x70, 0x6c,
	0x61, 0x69, 0x6e, 0x2c, 0x74, 0x65, 0x78, 0x74,
	0x2f, 0x6a, 0x61, 0x76, 0x61, 0x73, 0x63, 0x72,
	0x69, 0x70, 0x74, 0x2c, 0x70, 0x75, 0x62, 0x6c,
	0x69, 0x63, 0x70, 0x72, 0x69, 0x76, 0x61, 0x74,
	0x65, 0x6d, 0x61, 0x78, 0x2d, 0x61, 0x67, 0x65,
	0x3d, 0x67, 0x7a, 0x69, 0x70, 0x2c, 0x64, 0x65,
	0x66, 0x6c, 0x61, 0x74, 0x65, 0x2c, 0x73, 0x64,
	0x63, 0x68, 0x63, 0x68, 0x61, 0x72, 0x73, 0x65,
	0x74, 0x3d, 0x75, 0x74, 0x66, 0x2d, 0x38, 0x63,
	0x68, 0x61, 0x72, 0x73, 0x65, 0x74, 0x3d, 0x69,
	0x73, 0x6f, 0x2d, 0x38, 0x38, 0x35, 0x39, 0x2d,
	0x31, 0x2c, 0x75, 0x74, 0x66, 0x2d, 0x2c, 0x2a,
	0x2c, 0x65, 0x6e, 0x71, 0x3d, 0x30, 0x2e,
}
