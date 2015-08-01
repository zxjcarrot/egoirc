package egoirc

import (
	"fmt"
	"testing"
)

func TestNewConn(t *testing.T) {
	s := Setup{}
	s.address = "irc.freenode.net:6667"
	conn, err := newConn(&s)
	if err != nil {
		t.Fatal(err)
	}
	for {
		line, err := conn.readLine()
		fmt.Printf("%s\n", line)
		if err != nil {
			t.Log(err)
			break
		}
	}
}
