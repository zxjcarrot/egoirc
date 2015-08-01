package egoirc

import (
	"bufio"
	"net"
)

// ircConn represents a connection to a irc server
type ircConn struct {
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

const (
	defaultPort = "6667"
)

// newConn tries to connect to the irc server addressed by @address, returns the new connection.
// @address
// @interval indicates the interval in second to send the ping command to the server
// default interval 50s is used if @interval is negative
func newConn(s *Setup) (conn ircConn, err error) {
	var newConn ircConn

	if newConn.c, err = net.Dial("tcp", s.address); err != nil {
		return
	}
	newConn.r = bufio.NewReader(newConn.c)
	newConn.w = bufio.NewWriter(newConn.c)
	conn = newConn
	return
}

// readline reads until "\r\n" is reached, returns the line with "\r\n" stripped out
func (c ircConn) readLine() (line string, err error) {
	var buf, buf2 []byte

again:

	buf2, err = c.r.ReadBytes('\r')

	if len(buf2) > 0 {
		buf = append(buf, buf2[0:len(buf2)-1]...) // get rid of '\r'
	}

	if err != nil {
		line = string(buf)
		return
	}

	dummy, err := c.r.Peek(1)
	if err != nil {
		return
	}

	if dummy[0] == '\n' {
		c.r.Read(dummy) // get rid of '\n'
	} else {
		goto again
	}

	line = string(buf)
	return
}

// writeLine writes a line of string into the connection
func (c ircConn) writeLine(line string) (err error) {
	_, err = c.w.WriteString(line)
	c.w.Flush()
	return
}

func (c ircConn) Close() {
	c.c.Close()
}
