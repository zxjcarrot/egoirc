/*
* event-driven irc client library
 */
package egoirc

import (
	"bytes"
	"strings"
)

type Command struct {
	prefix string
	name   string
	params []string
}

type rule struct {
	needPrefix bool
	minParams  int
}

// maps valid commands to its rule
// for detailed command spec, see https://en.wikipedia.org/wiki/List_of_Internet_Relay_Chat_commands
var validCmds = map[string]rule{
	"ADMIN":    rule{false, 0},
	"AWAY":     rule{false, 0},
	"CNOTICE":  rule{false, 3},
	"CPRIVMSG": rule{false, 3},
	"CONNECT":  rule{false, 2},
	"DIE":      rule{false, 0},
	"ENCAP":    rule{true, 3},
	"ERROR":    rule{false, 1},
	"HELP":     rule{false, 0},
	"INFO":     rule{false, 0},
	"ISON":     rule{false, 1},
	"JOIN":     rule{false, 1},
	"KICK":     rule{false, 2},
	"KILL":     rule{false, 2},
	"KNOCK":    rule{false, 1},
	"LINKS":    rule{false, 0},
	"LIST":     rule{false, 0},
	"LUSERS":   rule{false, 0},
	"MODE":     rule{false, 2},
	"MOTD":     rule{false, 0},
	"NAMES":    rule{false, 0},
	"NICK":     rule{false, 1},
	"NOTICE":   rule{false, 2},
	"PART":     rule{false, 1},
	"PASS":     rule{false, 1},
	"PING":     rule{false, 1},
	"PONG":     rule{false, 1},
	"PRIVMSG":  rule{false, 2},
	"QUIT":     rule{false, 0},
	"REHASH":   rule{false, 0},
	"RESTART":  rule{false, 0},
	"RULES":    rule{false, 0},
	"SERVER":   rule{false, 3},
	"SERVICE":  rule{false, 6},
	"SERVLIST": rule{false, 0},
	"SQUERY":   rule{false, 2},
	"SQUIT":    rule{false, 2},
	"SETNAME":  rule{false, 1},
	"SILENCE":  rule{false, 0},
	"STATS":    rule{false, 1},
	"SUMMON":   rule{false, 1},
	"TIME":     rule{false, 0},
	"TOPIC":    rule{false, 1},
	"TRACE":    rule{false, 0},
	"USER":     rule{false, 4},
	"USERHOST": rule{false, 1},
	"USERIP":   rule{false, 1},
	"USERS":    rule{false, 0},
	"VERSION":  rule{false, 0},
	"WALLOPS":  rule{false, 1},
	"WATCH":    rule{false, 0},
	"WHO":      rule{false, 0},
	"WHOIS":    rule{false, 1},
	"WHOWAS":   rule{false, 1},
}

func threeDigits(name string) bool {
	if len(name) != 3 {
		return false
	}
	for i := 0; i < 3; i = i + 1 {
		if !(name[i] >= '0' && name[i] <= '9') {
			return false
		}
	}
	return true
}

func newCommand(prefix, name string, params ...string) *Command {
	var cmd Command
	cmd.prefix = prefix
	cmd.name = name
	cmd.params = append(cmd.params, params...)
	return &cmd
}

// return if the given command is valid
func checkCmd(c *Command) (err error) {
	if threeDigits(c.name) {
		return
	}

	r, ok := validCmds[c.name]

	if !ok {
		err = Error{ERR_INVALID_CMD, err2msg[ERR_INVALID_CMD], c}
	} else if len(c.params) < r.minParams {
		err = Error{ERR_NEED_MORE_PARAMS, err2msg[ERR_NEED_MORE_PARAMS], c}
	} else if r.needPrefix && (len(c.params) < 1 || c.params[0][0] != ':') {
		err = Error{ERR_NEED_PREFIX, err2msg[ERR_NEED_PREFIX], c}
	}

	return
}

// marshal returns the text representation of a command ready for transmission
func (c *Command) marshal() (line string, err error) {
	if err = checkCmd(c); err != nil {
		return
	}

	var buf bytes.Buffer

	if c.prefix != "" {
		buf.WriteString(":")
		buf.WriteString(c.prefix)
		buf.WriteString(" ")
	}
	buf.WriteString(c.name)
	if len(c.params) > 0 {
		buf.WriteString(" ")
		buf.WriteString(strings.Join(c.params, " "))
	}

	buf.WriteString("\r\n")

	line = buf.String()
	return
}

// parse builds a command from a given input line without command validity checks
// assuming input line does not end with '\r\n'
func parse(line string) (Command, error) {
	var c Command

	if len(line) > 512 {
		return c, Error{ERR_TOO_LONG, err2msg[ERR_TOO_LONG], nil}
	}

	idx := 0
	parts := strings.Split(line, " ")

	if len(parts) < 1 {
		return c, Error{ERR_INVALID_MSG, err2msg[ERR_INVALID_MSG], nil}
	}

	if parts[0][0] == ':' {
		if len(parts) < 2 {
			return c, Error{ERR_INVALID_MSG, err2msg[ERR_INVALID_MSG], nil}
		}
		c.prefix = parts[0][1:] // discard ':'
		idx++
	}

	c.name = parts[idx]
	if idx+1 < len(parts) {
		c.params = parts[idx+1:]
	}

	return c, nil
}

// unmarshal parses the given line into a command, err != nil indicates there's error during parsing
func (c *Command) unmarshal(line string) (err error) {
	var tmp Command
	if tmp, err = parse(line); err != nil {
		return
	}
	if err = checkCmd(&tmp); err != nil {
		return
	}
	*c = tmp
	return
}
