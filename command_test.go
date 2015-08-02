package egoirc

import (
	"testing"
)

func TestMarshal(t *testing.T) {
	type testcase struct {
		cmd     Command
		want    string
		wanterr error
		got     string
		err     error
	}

	cases := []testcase{
		{Command{"", "ADMIN", []string{"t1", "t2"}}, "ADMIN t1 t2\r\n", nil, "", nil},
		{Command{"", "HELP", []string{}}, "HELP\r\n", nil, "", nil},
		{Command{"", "USER", []string{"paul", "8", "*", ":paul robert"}}, "USER paul 8 * :paul robert\r\n", nil, "", nil},
		{Command{"", "NONEXIST", []string{}}, "", Error{ERR_INVALID_CMD, err2msg[ERR_INVALID_MSG], nil}, "", nil},
		{Command{"prefix", "CNOTICE", []string{"t1", "t2", "t3"}}, ":prefix CNOTICE t1 t2 t3\r\n", nil, "", nil},
		{Command{"", "CNOTICE", []string{"t1", "t2"}}, "", Error{ERR_NEED_MORE_PARAMS, err2msg[ERR_NEED_MORE_PARAMS], nil}, "", nil},
		{Command{"", "ENCAP", []string{"t1", "t2", "333"}}, "", Error{ERR_NEED_PREFIX, err2msg[ERR_NEED_PREFIX], nil}, "", nil},
	}

	for i, c := range cases {
		c.got, c.err = c.cmd.marshal()
		if e, ok := c.err.(Error); ok && e.code != c.wanterr.(Error).code {
			t.Errorf("case %d: want error %v, got error %v", i, c.wanterr, c.err)
		} else if c.got != c.want {
			t.Errorf("case %d: want %v, got %v", i, c.want, c.got)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}

func Equal(c1, c2 Command) bool {
	if c1.Prefix != c2.Prefix || c1.Name != c2.Name || len(c1.Params) != len(c2.Params) {
		return false
	}

	for i := 0; i < min(len(c1.Params), len(c2.Params)); i++ {
		if c1.Params[i] != c2.Params[i] {
			return false
		}
	}

	return true
}

func TestUnmarshal(t *testing.T) {
	type testcase struct {
		line    string
		want    Command
		wanterr error
		got     Command
		err     error
	}

	cases := []testcase{
		{"ADMIN", Command{"", "ADMIN", []string{}}, nil, *new(Command), nil},
		{":irc.freenode.net 001 paul :blah", Command{"irc.freenode.net", "001", []string{"paul", ":blah"}}, nil, *new(Command), nil},
		{":irc.freenode.net aaa paul :blah", *new(Command), Error{ERR_INVALID_CMD, err2msg[ERR_INVALID_MSG], nil}, *new(Command), nil},
	}

	for i, c := range cases {
		c.err = c.got.unmarshal(c.line)
		if e, ok := c.err.(Error); ok && e.code != c.wanterr.(Error).code {
			t.Errorf("case %d: want error=%#v, got error %#v", i, c.wanterr, c.err)
		} else if !Equal(c.got, c.want) {
			t.Errorf("case %d: want %v, got %v", i, c.want, c.got)
		}
	}
}
