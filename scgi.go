package scgi // import "gopkg.in/scgi.v0"

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

func WriteNetstring(w io.Writer, data []byte) error {
	_, err := w.Write([]byte(strconv.Itoa(len(data))))
	if err != nil {
		return errors.Wrap(err, "netstring: write error")
	}

	_, err = w.Write([]byte{':'})
	if err != nil {
		return errors.Wrap(err, "netstring: write error")
	}

	_, err = w.Write(data)
	if err != nil {
		return errors.Wrap(err, "netstring: write error")
	}

	_, err = w.Write([]byte{','})
	if err != nil {
		return errors.Wrap(err, "netstring: write error")
	}

	return nil
}

func ReadNetstring(r *bufio.Reader) (string, error) {
	dataLen, err := r.ReadString(':')
	if err != nil {
		return "", errors.Wrap(err, "netstring: read error")
	}

	// Chop off the trailing ":"
	dataLen = dataLen[:len(dataLen)-1]
	count, err := strconv.Atoi(dataLen)
	if err != nil {
		return "", errors.Wrap(err, "netstring: read error")
	}

	data := make([]byte, count+1)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return "", errors.Wrap(err, "netstring: read error")
	}

	if data[len(data)-1] != ',' {
		return "", errors.New("netstring: read error: missing trailing comma")
	}
	data = data[:len(data)-1]

	return string(data), nil
}

type scgiClient struct{}

func (c *scgiClient) RoundTrip(req *http.Request) (*http.Response, error) {
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, errors.Wrap(err, "scgi: round trip: body read error")
	}

	// This allows for scgi://rtorrent to be relative and scgi:///tmp/rtorrent
	// to be absolute.
	path := req.URL.Path
	if req.URL.Host != "" {
		path = "./" + req.URL.Host + path
	}

	scgiConn, err := net.Dial("unix", path)

	// Write the required SCGI headers
	var headers = []string{
		"CONTENT_LENGTH",
		strconv.Itoa(len(data)),
		"SCGI",
		"1",
		"REQUEST_METHOD",
		req.Method,
		"SERVER_PROTOCOL",
		req.Proto,
	}

	headerBuf := &bytes.Buffer{}
	for _, val := range headers {
		headerBuf.WriteString(val)
		headerBuf.Write([]byte{0x00})
	}

	// Write additional headers
	for key, val := range req.Header {
		headerBuf.WriteString(key)
		headerBuf.Write([]byte{0x00})
		headerBuf.WriteString(strings.Join(val, ","))
		headerBuf.Write([]byte{0x00})
	}

	err = WriteNetstring(scgiConn, headerBuf.Bytes())
	if err != nil {
		return nil, errors.Wrap(err, "scgi: round trip")
	}

	_, err = scgiConn.Write(data)
	if err != nil {
		return nil, errors.Wrap(err, "scgi: round trip write error")
	}

	// There isn't a method for cgi reponse parsing, but they're close enough
	// that we can hack on what's needed and use a normal http parser. This does
	// assume that the Status header is sent first, but in my experience most
	// implementations do this anyway.
	scgiRead := bufio.NewReader(scgiConn)

	// Grab the first line and chop off the extra characters from the end.
	firstLine, err := scgiRead.ReadString('\n')
	if err != nil {
		// TODO: Wrap error
		return nil, err
	}
	if firstLine[len(firstLine)-1] == '\n' {
		firstLine = firstLine[:len(firstLine)-1]
	}
	if firstLine[len(firstLine)-1] == '\r' {
		firstLine = firstLine[:len(firstLine)-1]
	}

	// The first line should be a header containing "Status: 200 OK". We chop it
	// in half, ensure this is the Status header, and use the second part in the
	// http response.
	parts := strings.SplitN(firstLine, ": ", 2)
	if len(parts) != 2 {
		return nil, errors.New("scgi: round trip: invalid status response format")
	}
	if parts[0] != "Status" {
		return nil, errors.New("scgi: round trip: invalid status header")
	}

	scgiRead = bufio.NewReader(
		io.MultiReader(
			bytes.NewBufferString(req.Proto+" "+parts[1]+"\r\n"),
			scgiRead))

	resp, err := http.ReadResponse(scgiRead, req)
	if err != nil {
		return nil, errors.New("scgi: round trip")
	}

	return resp, nil
}
