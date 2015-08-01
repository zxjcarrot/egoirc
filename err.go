package egoirc

import (
	"fmt"
)

const (
	ERR_INVALID_CMD      = iota // invalid command
	ERR_TOO_LONG                // message exceeded 512 characters
	ERR_INVALID_MSG             // invalid message
	ERR_NEED_MORE_PARAMS        // need more parameters
	ERR_NEED_PREFIX             // prefix is required
	ERR_NEED_NICKNAME           // nickname is required
	ERR_NICKNAME_TAKEN          // nickname is already taken
	ERR_CANT_REGISTER           // can't register to the server
)

var err2msg = [...]string{
	ERR_INVALID_CMD:      "invalid command",
	ERR_TOO_LONG:         "message exceeded 512 characters",
	ERR_INVALID_MSG:      "invalid message",
	ERR_NEED_MORE_PARAMS: "need more parameters",
	ERR_NEED_PREFIX:      "prefix is required",
	ERR_NEED_NICKNAME:    "nickname is required",
	ERR_NICKNAME_TAKEN:   "nickname is already taken",
	ERR_CANT_REGISTER:    "can't register to the server",
}

var err2event = [...]string{
	ERR_INVALID_CMD:      "ERR_INVALID_CMD",
	ERR_TOO_LONG:         "ERR_TOO_LONG",
	ERR_INVALID_MSG:      "ERR_INVALID_MSG",
	ERR_NEED_MORE_PARAMS: "ERR_NEED_MORE_PARAMS",
	ERR_NEED_PREFIX:      "ERR_NEED_PREFIX",
	ERR_NEED_NICKNAME:    "ERR_NEED_NICKNAME",
	ERR_NICKNAME_TAKEN:   "ERR_NICKNAME_TAKEN",
	ERR_CANT_REGISTER:    "ERR_CANT_REGISTER",
}

type Error struct {
	// the numeric value of the error
	code int
	// the text value of the error
	msg string
	// the command related to this error
	c *Command
}

func (e Error) Error() string {
	return fmt.Sprintf("code %d : %s, %v", e.code, e.msg, e.c)
}
