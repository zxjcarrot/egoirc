package egoirc

// EventHandler is user-registered fucntion for handling interested events,
// invoked by main goroutine when corresponding events occured.
// @e: the occured event
// @c: the command related to the event,
//  could be nil if the @e is one of  EV_CONNECTED, EV_DISCONNECTED or EV_DEFAULT
// @err: the error related to the event, nil when there is no error
// @data: provided by user at the callback registration phase
// handler should return boolean indicates if the event is successfully handled
// a false value will cause the client to exit
type EventHandler func(e Event, c *Command, err error, data interface{}) bool

// Event represents a event that is registered on a client.
// Event takes the following form:
//                irc command name: QUIT, PRIVMSG etc..., see https://en.wikipedia.org/wiki/List_of_Internet_Relay_Chat_commands for details.
//                irc reply number: 001, 311 etc..., see https://tools.ietf.org/html/rfc2812#section-5 for details.
//                connection status change events: see constants below.
//                error events: see err.go for detailed events.
type Event string

type eventRegistry struct {
	e       Event
	handler EventHandler
	data    interface{}
}

// general event type
const (
	et_error = iota // error event
	et_cmd          // command event
	et_user         // user-defined event
)

type eventType int
type eventData struct {
	et  eventType
	e   Event
	c   *Command
	err error
}

const (
	// events that do not have a callback will be delivery to this event's handler
	EV_DEFAULT = "DEFAULT"
	// connected with the server
	EV_CONNECTED = "CONNECTED"
	// disconnected from the server
	EV_DISCONNECTED = "DISCONNECTED"
	// network error
	EV_NET_ERROR = "NET_ERROR"
	// miscellaneous errors
	EV_ERROR = "ERROR"
)
